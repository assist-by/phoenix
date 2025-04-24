package market

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/exchange"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
	"github.com/assist-by/phoenix/internal/strategy"
)

// RetryConfig는 재시도 설정을 정의합니다
type RetryConfig struct {
	MaxRetries int           // 최대 재시도 횟수
	BaseDelay  time.Duration // 기본 대기 시간
	MaxDelay   time.Duration // 최대 대기 시간
	Factor     float64       // 대기 시간 증가 계수
}

// Collector는 시장 데이터 수집기를 구현합니다
type Collector struct {
	exchange exchange.Exchange
	discord  *discord.Client
	strategy strategy.Strategy
	config   *config.Config

	retry RetryConfig
	mu    sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(exchange exchange.Exchange, discord *discord.Client, strategy strategy.Strategy, config *config.Config, opts ...CollectorOption) *Collector {
	c := &Collector{
		exchange: exchange,
		discord:  discord,
		strategy: strategy,
		config:   config,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// CollectorOption은 수집기의 옵션을 정의합니다
type CollectorOption func(*Collector)

// WithCandleLimit은 캔들 데이터 조회 개수를 설정합니다
func WithCandleLimit(limit int) CollectorOption {
	return func(c *Collector) {
		c.config.App.CandleLimit = limit
	}
}

// WithRetryConfig는 재시도 설정을 지정합니다
func WithRetryConfig(config RetryConfig) CollectorOption {
	return func(c *Collector) {
		c.retry = config
	}
}

// collect는 한 번의 데이터 수집 사이클을 수행합니다
func (c *Collector) Collect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 심볼 목록 결정
	var symbols []string
	var err error

	if c.config.App.UseTopSymbols {

		symbols, err = c.exchange.GetTopVolumeSymbols(ctx, c.config.App.TopSymbolsCount)
		if err != nil {
			return fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
		}
	} else {
		// 설정된 심볼 사용
		if len(c.config.App.Symbols) > 0 {
			symbols = c.config.App.Symbols
		} else {
			// 기본값으로 BTCUSDT 사용
			symbols = []string{"BTCUSDT"}
		}
	}

	// 각 심볼의 잔고 조회

	balances, err := c.exchange.GetBalance(ctx)
	if err != nil {
		return err
	}

	// 잔고 정보 로깅 및 알림
	balanceInfo := "현재 보유 잔고:\n"
	for asset, balance := range balances {
		if balance.Available > 0 || balance.Locked > 0 {
			balanceInfo += fmt.Sprintf("%s: 총: %.8f, 사용가능: %.8f, 잠금: %.8f\n",
				asset, balance.CrossWalletBalance, balance.Available, balance.Locked)
		}
	}
	if c.discord != nil {
		if err := c.discord.SendInfo(balanceInfo); err != nil {
			log.Printf("잔고 정보 알림 전송 실패: %v", err)
		}
	}

	// 각 심볼의 캔들 데이터 수집
	for _, symbol := range symbols {
		err := c.withRetry(ctx, fmt.Sprintf("%s 캔들 데이터 조회", symbol), func() error {
			candles, err := c.exchange.GetKlines(ctx, symbol, c.getIntervalString(), c.config.App.CandleLimit)
			if err != nil {
				return err
			}

			log.Printf("%s 심볼의 캔들 데이터 %d개 수집 완료", symbol, len(candles))

			// 캔들 데이터를 indicator.PriceData로 변환
			prices := make([]indicator.PriceData, len(candles))
			for i, candle := range candles {
				prices[i] = indicator.PriceData{
					Time:   candle.OpenTime,
					Open:   candle.Open,
					High:   candle.High,
					Low:    candle.Low,
					Close:  candle.Close,
					Volume: candle.Volume,
				}
			}

			// 시그널 감지
			s, err := c.strategy.Analyze(ctx, symbol, candles)
			if err != nil {
				log.Printf("시그널 감지 실패 (%s): %v", symbol, err)
				return nil
			}

			// 시그널 정보 로깅
			log.Printf("%s 시그널 감지 결과: %+v", symbol, s)

			if s != nil {
				if err := c.discord.SendSignal(s); err != nil {
					log.Printf("시그널 알림 전송 실패 (%s): %v", symbol, err)
				}

				if s.Type != domain.NoSignal {

					// 진입 가능 여부 확인
					result, err := c.checkEntryAvailable(ctx, s)
					if err != nil {
						if err := c.discord.SendError(err); err != nil {
							log.Printf("에러 알림 전송 실패: %v", err)
						}

					}

					if result {
						// 매매 실행
						if err := c.ExecuteSignalTrade(ctx, s); err != nil {
							c.discord.SendError(fmt.Errorf("매매 실행 실패: %v", err))
						} else {
							log.Printf("%s %s 포지션 진입 및 TP/SL 설정 완료",
								s.Symbol, s.Type.String())
						}
					}
				}
			}

			return nil
		})
		if err != nil {
			log.Printf("%s 심볼 데이터 수집 실패: %v", symbol, err)
			continue
		}
	}

	return nil
}

// CalculatePosition은 코인의 특성과 최소 주문 단위를 고려하여 실제 포지션 크기와 수량을 계산합니다
// 단계별 계산:
// 1. 이론적 최대 포지션 = 가용잔고 × 레버리지
// 2. 이론적 최대 수량 = 이론적 최대 포지션 ÷ 코인 가격
// 3. 실제 수량 = 이론적 최대 수량을 최소 주문 단위로 내림
// 4. 실제 포지션 가치 = 실제 수량 × 코인 가격
// 5. 수수료 및 마진 고려해 최종 조정
func (c *Collector) CalculatePosition(
	balance float64, // 가용 잔고
	totalBalance float64, // 총 잔고 (usdtBalance.CrossWalletBalance)
	leverage int, // 레버리지
	coinPrice float64, // 코인 현재 가격
	stepSize float64, // 코인 최소 주문 단위
	maintMargin float64, // 유지증거금률
) (PositionSizeResult, error) {
	// 1. 사용 가능한 잔고에서 항상 90%만 사용
	maxAllocationPercent := 0.9
	allocatedBalance := totalBalance * maxAllocationPercent

	// 가용 잔고가 필요한 할당 금액보다 작은 경우 에러 반환
	if balance < allocatedBalance {
		return PositionSizeResult{}, fmt.Errorf("가용 잔고가 부족합니다: 필요 %.2f USDT, 현재 %.2f USDT",
			allocatedBalance, balance)
	}

	// 2. 레버리지 적용 및 수수료 고려
	totalFeeRate := 0.002 // 0.2% (진입 + 청산 수수료 + 여유분)
	effectiveMargin := maintMargin + totalFeeRate

	// 안전하게 사용 가능한 최대 포지션 가치 계산
	maxSafePositionValue := (allocatedBalance * float64(leverage)) / (1 + effectiveMargin)

	// 3. 최대 안전 수량 계산
	maxSafeQuantity := maxSafePositionValue / coinPrice

	// 4. 최소 주문 단위로 수량 조정
	// stepSize가 0.001이면 소수점 3자리
	precision := 0
	temp := stepSize
	for temp < 1.0 {
		temp *= 10
		precision++
	}

	// 소수점 자릿수에 맞춰 내림 계산
	scale := math.Pow(10, float64(precision))
	steps := math.Floor(maxSafeQuantity / stepSize)
	adjustedQuantity := steps * stepSize

	// 소수점 자릿수 정밀도 보장
	adjustedQuantity = math.Floor(adjustedQuantity*scale) / scale

	// 5. 최종 포지션 가치 계산
	finalPositionValue := adjustedQuantity * coinPrice

	// 포지션 크기에 대한 추가 안전장치 (최소값과 최대값 제한)
	finalPositionValue = math.Min(finalPositionValue, maxSafePositionValue)

	// 소수점 2자리까지 내림 (USDT 기준)
	return PositionSizeResult{
		PositionValue: math.Floor(finalPositionValue*100) / 100,
		Quantity:      adjustedQuantity,
	}, nil
}

// findDomainBracket은 주어진 레버리지에 해당하는 브라켓을 찾습니다
func findDomainBracket(brackets []domain.LeverageBracket, leverage int) *domain.LeverageBracket {
	// 레버리지가 높은 순으로 정렬되어 있으므로,
	// 설정된 레버리지보다 크거나 같은 첫 번째 브라켓을 찾습니다.
	for i := len(brackets) - 1; i >= 0; i-- {
		if brackets[i].InitialLeverage >= leverage {
			return &brackets[i]
		}
	}

	// 찾지 못한 경우 가장 낮은 레버리지 브라켓 반환
	if len(brackets) > 0 {
		return &brackets[0]
	}
	return nil
}
func (c *Collector) checkEntryAvailable(ctx context.Context, coinSignal *strategy.Signal) (bool, error) {
	// 1. 현재 포지션 조회
	positions, err := c.exchange.GetPositions(ctx)
	if err != nil {
		return false, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 기존 포지션 확인
	var existingPosition *domain.Position
	for _, pos := range positions {
		if pos.Symbol == coinSignal.Symbol && pos.Quantity != 0 {
			existingPosition = &pos
			break
		}
	}

	// 포지션이 없으면 바로 진행 가능
	if existingPosition == nil {
		log.Printf("활성 포지션 없음: %s", coinSignal.Symbol)

		// 2. 열린 주문 확인 및 취소
		return c.cancelOpenOrders(ctx, coinSignal.Symbol)
	}

	// 현재 포지션 방향 확인
	currentPositionIsLong := existingPosition.PositionSide == "LONG" ||
		(existingPosition.PositionSide == "BOTH" && existingPosition.Quantity > 0)

	// 새 시그널 방향 확인
	newSignalIsLong := coinSignal.Type == domain.Long

	// 같은 방향의 시그널이면 진입 불가
	if currentPositionIsLong == newSignalIsLong {
		return false, fmt.Errorf("이미 같은 방향의 %s 포지션이 존재합니다: 수량: %.8f, 방향: %s",
			existingPosition.Symbol, math.Abs(existingPosition.Quantity), existingPosition.PositionSide)
	}

	// 반대 방향 시그널이면 기존 포지션 청산 후 진행
	log.Printf("반대 방향 시그널 감지: 기존 %s 포지션 청산 후 %s 진입 예정",
		existingPosition.PositionSide,
		map[bool]string{true: "LONG", false: "SHORT"}[newSignalIsLong])

	// 1) 기존 주문 취소
	cancelled, err := c.cancelOpenOrders(ctx, coinSignal.Symbol)
	if !cancelled || err != nil {
		return false, fmt.Errorf("기존 주문 취소 실패: %w", err)
	}

	// 2) 시장가로 포지션 청산
	err = c.closePositionAtMarket(ctx, existingPosition)
	if err != nil {
		return false, fmt.Errorf("포지션 청산 실패: %w", err)
	}

	// 3) 포지션이 제대로 청산되었는지 확인
	cleared, err := c.confirmPositionClosed(ctx, coinSignal.Symbol)
	if !cleared || err != nil {
		return false, fmt.Errorf("포지션 청산 확인 실패: %w", err)
	}

	// 디스코드로 알림
	if c.discord != nil {
		c.discord.SendInfo(fmt.Sprintf("기존 %s 포지션을 청산하고 %s 시그널 진행 준비 완료",
			existingPosition.PositionSide,
			map[bool]string{true: "LONG", false: "SHORT"}[newSignalIsLong]))
	}

	return true, nil
}

// cancelOpenOrders는 특정 심볼에 대한 모든 열린 주문을 취소합니다
func (c *Collector) cancelOpenOrders(ctx context.Context, symbol string) (bool, error) {
	openOrders, err := c.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return false, fmt.Errorf("주문 조회 실패: %w", err)
	}

	// 기존 TP/SL 주문 취소
	if len(openOrders) > 0 {
		log.Printf("%s의 기존 주문 %d개를 취소합니다.", symbol, len(openOrders))
		for _, order := range openOrders {
			if err := c.exchange.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				return false, fmt.Errorf("주문 취소 실패 (ID: %d): %w", order.OrderID, err)
			}
			log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
		}
	}

	return true, nil
}

// confirmPositionClosed는 포지션이 제대로 청산되었는지 확인합니다
func (c *Collector) confirmPositionClosed(ctx context.Context, symbol string) (bool, error) {
	// 포지션이 청산되었는지 확인 (최대 5회 시도)
	maxRetries := 5
	retryInterval := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		positions, err := c.exchange.GetPositions(ctx)
		if err != nil {
			log.Printf("포지션 조회 실패 (시도 %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryInterval)
			continue
		}

		// 해당 심볼의 포지션이 있는지 확인
		positionExists := false
		for _, pos := range positions {
			if pos.Symbol == symbol && math.Abs(pos.Quantity) > 0 {
				positionExists = true
				break
			}
		}

		if !positionExists {
			log.Printf("%s 포지션 청산 확인 완료", symbol)
			return true, nil
		}

		log.Printf("%s 포지션 청산 대기 중... (시도 %d/%d)", symbol, i+1, maxRetries)
		time.Sleep(retryInterval)
		retryInterval *= 2 // 지수 백오프
	}

	return false, fmt.Errorf("최대 재시도 횟수 초과: 포지션이 청산되지 않음")
}

// closePositionAtMarket는 시장가로 포지션을 청산합니다
func (c *Collector) closePositionAtMarket(ctx context.Context, position *domain.Position) error {
	// 포지션 방향에 따라 반대 주문 생성
	side := Buy
	positionSide := Long

	if position.PositionSide == "LONG" || (position.PositionSide == "BOTH" && position.Quantity > 0) {
		side = Sell
		positionSide = Long
	} else {
		side = Buy
		positionSide = Short
	}

	// 포지션 수량 (절대값 사용)
	quantity := math.Abs(position.Quantity)

	// 시장가 청산 주문
	closeOrder := domain.OrderRequest{
		Symbol:       position.Symbol,
		Side:         domain.OrderSide(side),
		PositionSide: domain.PositionSide(positionSide),
		Type:         domain.Market,
		Quantity:     quantity,
	}

	// 주문 실행
	orderResponse, err := c.exchange.PlaceOrder(ctx, closeOrder)
	if err != nil {
		return fmt.Errorf("포지션 청산 주문 실패: %w", err)
	}

	log.Printf("포지션 청산 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		position.Symbol, quantity, orderResponse.OrderID)

	return nil
}

// TODO: 단순 상향돌파만 체크하는게 아니라 MACD가 0 이상인지 이하인지 그거도 추세 판단하는데 사용되는걸 적용해야한다.
// ExecuteSignalTrade는 감지된 시그널에 따라 매매를 실행합니다
func (c *Collector) ExecuteSignalTrade(ctx context.Context, s *strategy.Signal) error {
	if s.Type == domain.NoSignal {
		return nil // 시그널이 없으면 아무것도 하지 않음
	}

	//---------------------------------
	// 1. 잔고 조회
	//---------------------------------

	balances, err := c.exchange.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("잔고 조회 실패: %w", err)
	}

	//---------------------------------
	// 2. USDT 잔고 확인
	//---------------------------------
	usdtBalance, exists := balances["USDT"]
	if !exists || usdtBalance.Available <= 0 {
		return fmt.Errorf("USDT 잔고가 부족합니다")
	}

	//---------------------------------
	// 3. 현재 가격 조회 (최근 캔들 사용)
	//---------------------------------

	candles, err := c.exchange.GetKlines(ctx, s.Symbol, "1m", 1)
	if err != nil {
		return fmt.Errorf("가격 정보 조회 실패: %w", err)
	}
	if len(candles) == 0 {
		return fmt.Errorf("캔들 데이터를 가져오지 못했습니다")
	}
	currentPrice := candles[0].Close

	//---------------------------------
	// 4. 심볼 정보 조회
	//---------------------------------

	symbolInfo, err := c.exchange.GetSymbolInfo(ctx, s.Symbol)

	if err != nil {
		return fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	//---------------------------------
	// 5. HEDGE 모드 설정
	//---------------------------------

	err = c.exchange.SetPositionMode(ctx, true)

	if err != nil {
		return fmt.Errorf("HEDGE 모드 설정 실패: %w", err)
	}

	//---------------------------------
	// 6. 레버리지 설정
	//---------------------------------
	leverage := c.config.Trading.Leverage
	err = c.exchange.SetLeverage(ctx, s.Symbol, leverage)
	if err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	//---------------------------------
	// 7. 매수 수량 계산 (잔고의 90% 사용)
	//---------------------------------
	// 레버리지 브라켓 정보 조회
	brackets, err := c.exchange.GetLeverageBrackets(ctx, s.Symbol)
	if err != nil {
		return fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	if len(brackets) == 0 {
		return fmt.Errorf("레버리지 브라켓 정보가 없습니다")
	}

	bracket := findDomainBracket(brackets, leverage)
	if bracket == nil {
		return fmt.Errorf("적절한 레버리지 브라켓을 찾을 수 없습니다")
	}

	// 포지션 크기 계산
	positionResult, err := c.CalculatePosition(
		usdtBalance.Available,
		usdtBalance.CrossWalletBalance,
		leverage,
		currentPrice,
		symbolInfo.StepSize,
		bracket.MaintMarginRatio,
	)
	if err != nil {
		return fmt.Errorf("포지션 계산 실패: %w", err)
	}

	// 최소 주문 가치 체크
	if positionResult.PositionValue < symbolInfo.MinNotional {
		return fmt.Errorf("포지션 크기가 최소 주문 가치(%.2f USDT)보다 작습니다", symbolInfo.MinNotional)
	}

	//---------------------------------
	// 8. 주문 수량 정밀도 조정
	//---------------------------------
	adjustedQuantity := AdjustQuantity(
		positionResult.Quantity,
		symbolInfo.StepSize,
		symbolInfo.QuantityPrecision,
	)

	//---------------------------------
	// 9. 진입 주문 생성
	//---------------------------------
	orderSide := Buy
	positionSide := Long
	if s.Type == domain.Short {
		orderSide = Sell
		positionSide = Short
	}

	entryOrder := domain.OrderRequest{
		Symbol:       s.Symbol,
		Side:         domain.OrderSide(orderSide),
		PositionSide: domain.PositionSide(positionSide),
		Type:         domain.Market,
		Quantity:     adjustedQuantity,
	}

	//---------------------------------
	// 10. 진입 주문 실행
	//---------------------------------
	orderResponse, err := c.exchange.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return fmt.Errorf("주문 실행 실패: %w", err)
	}

	//---------------------------------
	// 11. 성공 메시지 출력 및 로깅
	//---------------------------------
	log.Printf("매수 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		s.Symbol, adjustedQuantity, orderResponse.OrderID)

	//---------------------------------
	// 12. 포지션 확인 및 TP/SL 설정
	//---------------------------------
	maxRetries := 5
	retryInterval := 1 * time.Second
	var position *domain.Position

	// 목표 포지션 사이드 문자열로 변환
	targetPositionSide := domain.LongPosition
	if s.Type == domain.Short {
		targetPositionSide = domain.ShortPosition
	}

	for i := 0; i < maxRetries; i++ {

		positions, err := c.exchange.GetPositions(ctx)
		if err != nil {
			log.Printf("포지션 조회 실패 (시도 %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryInterval)
			continue
		}

		for _, pos := range positions {
			// 포지션 사이드 문자열 비교
			if pos.Symbol == s.Symbol && pos.PositionSide == targetPositionSide {
				// Long은 수량이 양수, Short은 음수이기 때문에 조건 분기
				positionValid := false
				if targetPositionSide == domain.LongPosition && pos.Quantity > 0 {
					positionValid = true
				} else if targetPositionSide == domain.ShortPosition && pos.Quantity < 0 {
					positionValid = true
				}

				if positionValid {
					position = &pos
					// log.Printf("포지션 확인: %s %s, 수량: %.8f, 진입가: %.2f",
					// 	pos.Symbol, pos.PositionSide, math.Abs(pos.Quantity), pos.EntryPrice)
					break
				}
			}
		}

		if position != nil {
			break
		}
		time.Sleep(retryInterval)
		retryInterval *= 2 // 지수 백오프
	}

	if position == nil {
		return fmt.Errorf("최대 재시도 횟수 초과: 포지션을 찾을 수 없음")
	}

	//---------------------------------
	// 13. TP/SL 값 설정
	//---------------------------------
	// 종료 주문을 위한 반대 방향 계산

	actualEntryPrice := position.EntryPrice
	actualQuantity := math.Abs(position.Quantity)

	var stopLoss, takeProfit float64
	if s.Type == domain.Long {
		slDistance := s.Price - s.StopLoss
		tpDistance := s.TakeProfit - s.Price
		stopLoss = actualEntryPrice - slDistance
		takeProfit = actualEntryPrice + tpDistance
	} else {
		slDistance := s.StopLoss - s.Price
		tpDistance := s.Price - s.TakeProfit
		stopLoss = actualEntryPrice + slDistance
		takeProfit = actualEntryPrice - tpDistance
	}

	// 가격 정밀도에 맞게 조정
	// symbolInfo.TickSize와 symbolInfo.PricePrecision 사용
	adjustStopLoss := AdjustPrice(stopLoss, symbolInfo.TickSize, symbolInfo.PricePrecision)
	adjustTakeProfit := AdjustPrice(takeProfit, symbolInfo.TickSize, symbolInfo.PricePrecision)

	// 실제 계산된 비율로 메시지 생성
	slPctChange := ((adjustStopLoss - actualEntryPrice) / actualEntryPrice) * 100
	tpPctChange := ((adjustTakeProfit - actualEntryPrice) / actualEntryPrice) * 100

	// Short 포지션인 경우 부호 반전 (Short에서는 손절은 가격 상승, 익절은 가격 하락)
	if s.Type == domain.Short {
		slPctChange = -slPctChange
		tpPctChange = -tpPctChange
	}

	// TP/SL 설정 알림
	if err := c.discord.SendInfo(fmt.Sprintf(
		"TP/SL 설정 중: %s\n진입가: %.2f\n수량: %.8f\n손절가: %.2f (%.2f%%)\n목표가: %.2f (%.2f%%)",
		s.Symbol, actualEntryPrice, actualQuantity, adjustStopLoss, slPctChange, adjustTakeProfit, tpPctChange)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	//---------------------------------
	// 14. TP/SL 주문 생성
	//---------------------------------
	oppositeSide := Sell
	if s.Type == domain.Short {
		oppositeSide = Buy
	}
	// 손절 주문 생성
	slOrder := domain.OrderRequest{
		Symbol:       s.Symbol,
		Side:         domain.OrderSide(oppositeSide),
		PositionSide: domain.PositionSide(positionSide),
		Type:         domain.StopMarket,
		Quantity:     actualQuantity,
		StopPrice:    adjustStopLoss,
	}
	// 손절 주문 실행

	slResponse, err := c.exchange.PlaceOrder(ctx, slOrder)
	if err != nil {
		log.Printf("손절(SL) 주문 실패: %v", err)
		return fmt.Errorf("손절(SL) 주문 실패: %w", err)
	}

	// 익절 주문 생성
	tpOrder := domain.OrderRequest{
		Symbol:       s.Symbol,
		Side:         domain.OrderSide(oppositeSide),
		PositionSide: domain.PositionSide(positionSide),
		Type:         domain.TakeProfitMarket,
		Quantity:     actualQuantity,
		StopPrice:    adjustTakeProfit,
	}
	// 익절 주문 실행

	tpResponse, err := c.exchange.PlaceOrder(ctx, tpOrder)
	if err != nil {
		log.Printf("익절(TP) 주문 실패: %v", err)
		return fmt.Errorf("익절(TP) 주문 실패: %w", err)
	}

	//---------------------------------
	// 15. TP/SL 설정 완료 알림
	//---------------------------------
	if err := c.discord.SendInfo(fmt.Sprintf("✅ TP/SL 설정 완료: %s\n익절(TP) 주문 성공: ID=%d 심볼=%s, 가격=%.2f, 수량=%.8f\n손절(SL) 주문 생성: ID=%d 심볼=%s, 가격=%.2f, 수량=%.8f", s.Symbol, tpResponse.OrderID, tpOrder.Symbol, tpOrder.StopPrice, tpOrder.Quantity, slResponse.OrderID, slOrder.Symbol, slOrder.StopPrice, slOrder.Quantity)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	//---------------------------------
	// 16. 거래 정보 생성 및 전송
	//---------------------------------
	tradeInfo := notification.TradeInfo{
		Symbol:        s.Symbol,
		PositionType:  string(targetPositionSide),
		PositionValue: positionResult.PositionValue,
		Quantity:      actualQuantity,
		EntryPrice:    actualEntryPrice,
		StopLoss:      adjustStopLoss,
		TakeProfit:    adjustTakeProfit,
		Balance:       usdtBalance.Available - positionResult.PositionValue,
		Leverage:      leverage,
	}

	if err := c.discord.SendTradeInfo(tradeInfo); err != nil {
		log.Printf("거래 정보 알림 전송 실패: %v", err)
	}

	return nil
}

// AdjustQuantity는 바이낸스 최소 단위(stepSize)에 맞게 수량을 조정합니다
func AdjustQuantity(quantity float64, stepSize float64, precision int) float64 {
	if stepSize == 0 {
		return quantity // stepSize가 0이면 조정 불필요
	}

	// stepSize로 나누어 떨어지도록 조정
	steps := math.Floor(quantity / stepSize)
	adjustedQuantity := steps * stepSize

	// 정밀도에 맞게 반올림
	scale := math.Pow(10, float64(precision))
	return math.Floor(adjustedQuantity*scale) / scale
}

// getIntervalString은 수집 간격을 바이낸스 API 형식의 문자열로 변환합니다
func (c *Collector) getIntervalString() domain.TimeInterval {
	switch c.config.App.FetchInterval {
	case 1 * time.Minute:
		return domain.Interval1m
	case 3 * time.Minute:
		return domain.Interval3m
	case 5 * time.Minute:
		return domain.Interval5m
	case 15 * time.Minute:
		return domain.Interval15m
	case 30 * time.Minute:
		return domain.Interval30m
	case 1 * time.Hour:
		return domain.Interval1h
	case 2 * time.Hour:
		return domain.Interval2h
	case 4 * time.Hour:
		return domain.Interval4h
	case 6 * time.Hour:
		return domain.Interval6h
	case 8 * time.Hour:
		return domain.Interval8h
	case 12 * time.Hour:
		return domain.Interval12h
	case 24 * time.Hour:
		return domain.Interval1d
	default:
		return domain.Interval15m // 기본값
	}
}

// withRetry는 재시도 로직을 구현한 래퍼 함수입니다
func (c *Collector) withRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	delay := c.retry.BaseDelay

	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := fn(); err != nil {
				lastErr = err

				// 재시도 가능한 오류인지 확인
				if !IsRetryableError(err) {
					// 재시도가 필요 없는 오류는 바로 반환
					log.Printf("%s 실패 (재시도 불필요): %v", operation, err)
					return err
				}

				if attempt == c.retry.MaxRetries {
					// 마지막 시도에서 실패하면 Discord로 에러 알림 전송
					errMsg := fmt.Errorf("%s 실패 (최대 재시도 횟수 초과): %v", operation, err)
					if c.discord != nil {
						if notifyErr := c.discord.SendError(errMsg); notifyErr != nil {
							log.Printf("Discord 에러 알림 전송 실패: %v", notifyErr)
						}
					}
					return fmt.Errorf("최대 재시도 횟수 초과: %w", lastErr)
				}

				log.Printf("%s 실패 (attempt %d/%d): %v",
					operation, attempt+1, c.retry.MaxRetries, err)

				// 다음 재시도 전 대기
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
					// 대기 시간을 증가시키되, 최대 대기 시간을 넘지 않도록 함
					delay = time.Duration(float64(delay) * c.retry.Factor)
					if delay > c.retry.MaxDelay {
						delay = c.retry.MaxDelay
					}
				}
				continue
			}
			return nil
		}
	}
	return lastErr
}

// AdjustPrice는 가격 정밀도 설정 함수
func AdjustPrice(price float64, tickSize float64, precision int) float64 {
	if tickSize == 0 {
		return price // tickSize가 0이면 조정 불필요
	}

	// tickSize로 나누어 떨어지도록 조정
	ticks := math.Floor(price / tickSize)
	adjustedPrice := ticks * tickSize

	// 정밀도에 맞게 반올림
	scale := math.Pow(10, float64(precision))
	return math.Floor(adjustedPrice*scale) / scale
}
