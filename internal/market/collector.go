package market

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
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
	client        *Client
	discord       *discord.Client
	detector      *signal.Detector
	fetchInterval time.Duration
	candleLimit   int
	retry         RetryConfig
	mu            sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(client *Client, discord *discord.Client, detector *signal.Detector, fetchInterval time.Duration, candleLimit int, opts ...CollectorOption) *Collector {
	c := &Collector{
		client:        client,
		discord:       discord,
		detector:      detector,
		fetchInterval: fetchInterval,
		candleLimit:   candleLimit,
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
		c.candleLimit = limit
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

	// 이하 collect() 함수의 내용을 그대로 사용
	var symbols []string
	err := c.withRetry(ctx, "상위 거래량 심볼 조회", func() error {
		var err error
		symbols, err = c.client.GetTopVolumeSymbols(ctx, 3)
		return err
	})
	if err != nil {
		return fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
	}

	// 각 심볼의 잔고 조회
	var balances map[string]Balance
	err = c.withRetry(ctx, "잔고 조회", func() error {
		var err error
		balances, err = c.client.GetBalance(ctx)
		return err
	})
	if err != nil {
		return err
	}

	// 잔고 정보 로깅 및 알림
	balanceInfo := "현재 보유 잔고:\n"
	for asset, balance := range balances {
		if balance.Available > 0 || balance.Locked > 0 {
			balanceInfo += fmt.Sprintf("%s: 사용가능: %.8f, 잠금: %.8f\n",
				asset, balance.Available, balance.Locked)
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
			candles, err := c.client.GetKlines(ctx, symbol, c.getIntervalString(), c.candleLimit)
			if err != nil {
				return err
			}

			log.Printf("%s 심볼의 캔들 데이터 %d개 수집 완료", symbol, len(candles))

			// 캔들 데이터를 indicator.PriceData로 변환
			prices := make([]indicator.PriceData, len(candles))
			for i, candle := range candles {
				prices[i] = indicator.PriceData{
					Time:   time.Unix(candle.OpenTime/1000, 0),
					Open:   candle.Open,
					High:   candle.High,
					Low:    candle.Low,
					Close:  candle.Close,
					Volume: candle.Volume,
				}
			}

			// 시그널 감지
			s, err := c.detector.Detect(symbol, prices)
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

				if s.Type != signal.NoSignal {

					// 진입 가능 여부 확인
					result, err := c.checkEntryAvailable(ctx, s)
					if err != nil {
						log.Printf("진입 가능 여부 확인 실패: %v", err)
						if err := c.discord.SendError(err); err != nil {
							log.Printf("에러 알림 전송 실패: %v", err)
						}

					}

					if result.Available {
						// 매매 실행
						if err := c.executeSignalTrade(ctx, s); err != nil {
							c.discord.SendError(fmt.Errorf("매매 실행 실패: %v", err))
						} else {
							log.Printf("%s %s 포지션 진입 및 TP/SL 설정 완료",
								s.Symbol, s.Type.String())
						}
						balances, err := c.client.GetBalance(ctx)
						if err != nil {
							return fmt.Errorf("잔고 조회 실패: %w", err)
						}
						usdtBalance := balances["USDT"].Available

						// TradeInfo 생성
						tradeInfo := notification.TradeInfo{
							Symbol:        s.Symbol,
							PositionType:  s.Type.String(),
							PositionValue: result.PositionValue,
							Quantity:      result.Quantity,
							EntryPrice:    s.Price,
							StopLoss:      s.StopLoss,
							TakeProfit:    s.TakeProfit,
							Balance:       usdtBalance,
							Leverage:      5,
						}

						if err := c.discord.SendTradeInfo(tradeInfo); err != nil {
							log.Printf("거래 정보 알림 전송 실패: %v", err)
							if err := c.discord.SendError(err); err != nil {
								log.Printf("에러 알림 전송 실패: %v", err)
							}
						}

					} else {
						log.Printf("진입 불가: %s", result.Reason)
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

// calculatePosition은 코인의 특성과 최소 주문 단위를 고려하여 실제 포지션 크기와 수량을 계산합니다
// 단계별 계산:
// 1. 이론적 최대 포지션 = 가용잔고 × 레버리지
// 2. 이론적 최대 수량 = 이론적 최대 포지션 ÷ 코인 가격
// 3. 실제 수량 = 이론적 최대 수량을 최소 주문 단위로 내림
// 4. 실제 포지션 가치 = 실제 수량 × 코인 가격
// 5. 수수료 및 마진 고려해 최종 조정
func (c *Collector) calculatePosition(
	balance float64, // 가용 잔고
	leverage int, // 레버리지
	coinPrice float64, // 코인 현재 가격
	stepSize float64, // 코인 최소 주문 단위
	maintMargin float64, // 유지증거금률
) PositionSizeResult {
	// 1. 사용 가능한 잔고에서 안전 비율만 사용 (90%)
	safeBalance := balance * 0.9

	// 2. 레버리지 적용 및 수수료 고려
	totalFeeRate := 0.002 // 0.2% (진입 + 청산 수수료 + 여유분)
	effectiveMargin := maintMargin + totalFeeRate

	// 안전하게 사용 가능한 최대 포지션 가치 계산
	maxSafePositionValue := (safeBalance * float64(leverage)) / (1 + effectiveMargin)

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
	}
}

// findBracket은 주어진 레버리지에 해당하는 브라켓을 찾습니다
func findBracket(brackets []LeverageBracket, leverage int) *LeverageBracket {
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

func (c *Collector) checkEntryAvailable(ctx context.Context, coinSignal *signal.Signal) (EntryCheckResult, error) {
	result := EntryCheckResult{
		Available: false,
	}

	// 1. 현재 포지션 조회
	positions, err := c.client.GetPositions(ctx)
	if err != nil {
		if len(positions) == 0 {
			log.Printf("활성 포지션 없음: %s", coinSignal.Symbol)
		} else {
			return result, err
		}

	}

	// 포지션 체크
	for _, pos := range positions {
		if pos.Symbol == coinSignal.Symbol {
			if coinSignal.Type == signal.Long && pos.PositionSide == "LONG" {
				result.Reason = "이미 롱 포지션이 있습니다"
				return result, nil
			}
			if coinSignal.Type == signal.Short && pos.PositionSide == "SHORT" {
				result.Reason = "이미 숏 포지션이 있습니다"
				return result, nil
			}
		}
	}

	// 2. 잔고 조회
	balances, err := c.client.GetBalance(ctx)
	if err != nil {
		return result, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	// USDT 잔고 확인
	usdtBalance, exists := balances["USDT"]
	if !exists {
		result.Reason = "USDT 잔고가 부족합니다"
		return result, nil
	}

	// 3. 심볼 정보 조회
	symbolInfo, err := c.client.GetSymbolInfo(ctx, coinSignal.Symbol)
	if err != nil {
		return result, fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// 4. 레버리지 브라켓 정보 조회
	brackets, err := c.client.GetLeverageBrackets(ctx, coinSignal.Symbol)
	if err != nil {
		return result, fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 해당 심볼의 브라켓 정보 찾기
	var symbolBracket *SymbolBrackets
	for _, b := range brackets {
		if b.Symbol == coinSignal.Symbol {
			symbolBracket = &b
			break
		}
	}

	if symbolBracket == nil || len(symbolBracket.Brackets) == 0 {
		result.Reason = "레버리지 브라켓 정보가 없습니다"
		return result, nil
	}

	// 설정된 레버리지에 맞는 브라켓 찾기
	leverage := 5 // 레버리지 설정값
	bracket := findBracket(symbolBracket.Brackets, leverage)
	if bracket == nil {
		result.Reason = "적절한 레버리지 브라켓을 찾을 수 없습니다"
		return result, nil
	}

	// 브라켓 정보 로깅
	log.Printf("선택된 브라켓: 레버리지 %dx, 유지증거금률 %.4f%%, 최대 포지션 %.2f USDT",
		bracket.InitialLeverage,
		bracket.MaintMarginRatio*100,
		bracket.Notional)

	if err := c.discord.SendInfo(fmt.Sprintf("```\n%s\n레버리지: %dx\n유지증거금률: %.4f%%\n최대 포지션: %.2f USDT\n```",
		coinSignal.Symbol,
		bracket.InitialLeverage,
		bracket.MaintMarginRatio*100,
		bracket.Notional)); err != nil {
		c.discord.SendError(err)
	}

	// 5. 포지션 크기 계산
	positionResult := c.calculatePosition(
		usdtBalance.Available,
		leverage,
		coinSignal.Price,
		symbolInfo.StepSize,
		bracket.MaintMarginRatio,
	)

	// 최소 주문 가치 체크
	if positionResult.PositionValue < symbolInfo.MinNotional {
		result.Reason = fmt.Sprintf("포지션 크기가 최소 주문 가치(%.2f USDT)보다 작습니다", symbolInfo.MinNotional)
		return result, nil
	}

	// 모든 검사 통과
	result.Available = true
	result.PositionValue = positionResult.PositionValue
	result.Quantity = positionResult.Quantity

	return result, nil
}

// TODO: 단순 상향돌파만 체크하는게 아니라 MACD가 0 이상인지 이하인지 그거도 추세 판단하는데 사용되는걸 적용해야한다.
// executeSignalTrade는 감지된 시그널에 따라 매매를 실행합니다
func (c *Collector) executeSignalTrade(ctx context.Context, s *signal.Signal) error {
	if s.Type == signal.NoSignal {
		return nil // 시그널이 없으면 아무것도 하지 않음
	}

	// 1. 진입 가능 여부 확인 _ > quantity
	result, err := c.checkEntryAvailable(ctx, s)
	if err != nil {
		return fmt.Errorf("진입 가능 여부 확인 실패: %w", err)
	}

	// 2. 진입 불가능한 경우 종료
	if !result.Available {
		return fmt.Errorf("진입 불가: %s", result.Reason)
	}

	// 3. HEDGE 모드 설정
	if err := c.client.SetPositionMode(ctx, true); err != nil {
		return fmt.Errorf("HEDGE 모드 설정 실패: %w", err)
	}

	// 4. 레버리지 설정 (기본값 5로 가정)
	leverage := 5 // 설정에서 가져오는 것으로 변경 가능
	if err := c.client.SetLeverage(ctx, s.Symbol, leverage); err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	// 5. 심볼 정보 조회
	symbolInfo, err := c.client.GetSymbolInfo(ctx, s.Symbol)
	if err != nil {
		return fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// 6. 주문 수량 정밀도 조정 (StepSize 기준)
	originalQuantity := result.Quantity
	adjustedQuantity := adjustQuantity(originalQuantity, symbolInfo.StepSize, symbolInfo.QuantityPrecision)

	c.discord.SendInfo(fmt.Sprintf("주문 수량 계산: %s, 원래 수량=%f, 조정된 수량=%f, stepSize=%.8f, 정밀도=%d",
		s.Symbol, originalQuantity, adjustedQuantity, symbolInfo.StepSize, symbolInfo.QuantityPrecision))

	// 7. 진입 주문 생성
	orderSide := Buy
	positionSide := Long
	if s.Type == signal.Short {
		orderSide = Sell
		positionSide = Short
	}

	// 8. 진입 주문 실행
	entryOrder := OrderRequest{
		Symbol:       s.Symbol,
		Side:         orderSide,
		PositionSide: positionSide,
		Type:         Market,
		Quantity:     adjustedQuantity,
	}

	_, err = c.client.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return fmt.Errorf("주문 실행 실패: %w", err)
	}

	// 잔고 조회하여 TradeInfo에 추가
	balances, err := c.client.GetBalance(ctx)
	if err != nil {
		log.Printf("잔고 조회 실패: %v", err)
		// 잔고 조회 실패해도 진행
	}

	// USDT 잔고 가져오기
	usdtBalance := 0.0
	if balance, exists := balances["USDT"]; exists {
		usdtBalance = balance.Available
	}

	// TradeInfo 생성 및 전송
	tradeInfo := notification.TradeInfo{
		Symbol:        s.Symbol,
		PositionType:  s.Type.String(),
		PositionValue: result.PositionValue,
		Quantity:      adjustedQuantity, // 조정된 수량 사용
		EntryPrice:    s.Price,
		StopLoss:      s.StopLoss,
		TakeProfit:    s.TakeProfit,
		Balance:       usdtBalance,
		Leverage:      leverage,
	}

	if err := c.discord.SendTradeInfo(tradeInfo); err != nil {
		log.Printf("거래 정보 알림 전송 실패: %v", err)
	}

	// 9. TP/SL 주문 실행 - 여기서는 동일한 조정된 수량 사용
	if err := c.placeTPSLOrders(ctx, s, adjustedQuantity); err != nil {
		// TP/SL 설정 실패 알림
		errMsg := fmt.Errorf("⚠️ TP/SL 주문 설정 실패: %w", err)
		log.Printf("%v", errMsg)
		if err := c.discord.SendError(errMsg); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		// TP/SL 실패해도 메인 주문은 이미 실행되었으므로 계속 진행
	}

	return nil
}

// placeTPSLOrders는 TP(Take Profit)와 SL(Stop Loss) 주문을 설정합니다
func (c *Collector) placeTPSLOrders(ctx context.Context, s *signal.Signal, adjustedQuantity float64) error {

	// 주문의 반대 사이드 계산
	oppositeSide := Sell
	if s.Type == signal.Short {
		oppositeSide = Buy
	}

	// 포지션 사이드
	positionSide := Long
	if s.Type == signal.Short {
		positionSide = Short
	}

	// 손절(SL) 주문 생성
	slOrder := OrderRequest{
		Symbol:       s.Symbol,
		Side:         oppositeSide,
		PositionSide: positionSide,
		Type:         StopMarket,       // 스탑 마켓 주문
		Quantity:     adjustedQuantity, // 이미 조정된 수량
		StopPrice:    s.StopLoss,       // 손절가
	}

	// 손절 주문 실행
	_, err := c.client.PlaceOrder(ctx, slOrder)
	if err != nil {
		return fmt.Errorf("손절(SL) 주문 실패: %w", err)
	}

	// 익절(TP) 주문 생성
	tpOrder := OrderRequest{
		Symbol:       s.Symbol,
		Side:         oppositeSide,
		PositionSide: positionSide,
		Type:         TakeProfitMarket, // 익절 마켓 주문
		Quantity:     adjustedQuantity, // 이미 조정된 수량
		StopPrice:    s.TakeProfit,     // 익절가
	}

	// 익절 주문 실행
	_, err = c.client.PlaceOrder(ctx, tpOrder)
	if err != nil {
		return fmt.Errorf("익절(TP) 주문 실패: %w", err)
	}

	// TP/SL 설정 성공 알림
	tpslMsg := fmt.Sprintf("✅ %s TP/SL 설정 성공\n심볼: %s\n손절가: %.2f (%.2f%%)\n목표가: %.2f (%.2f%%)",
		s.Type.String(),
		s.Symbol,
		s.StopLoss,
		calculatePctDiff(s.Price, s.StopLoss, s.Type == signal.Short),
		s.TakeProfit,
		calculatePctDiff(s.Price, s.TakeProfit, s.Type != signal.Short))

	if err := c.discord.SendInfo(tpslMsg); err != nil {
		c.discord.SendError(fmt.Errorf("알림 전송 실패: %v", err))
	}

	return nil
}

// adjustQuantity는 바이낸스 최소 단위(stepSize)에 맞게 수량을 조정합니다
func adjustQuantity(quantity float64, stepSize float64, precision int) float64 {
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

// calculatePctDiff는 두 가격 간의 백분율 차이를 계산합니다
func calculatePctDiff(base, target float64, isGain bool) float64 {
	diff := (target - base) / base * 100
	if isGain {
		return diff // 양수 값 (이익)
	}
	return -diff // 음수 값 (손실)
}

// getIntervalString은 수집 간격을 바이낸스 API 형식의 문자열로 변환합니다
func (c *Collector) getIntervalString() string {
	switch c.fetchInterval {
	case 1 * time.Minute:
		return "1m"
	case 3 * time.Minute:
		return "3m"
	case 5 * time.Minute:
		return "5m"
	case 15 * time.Minute:
		return "15m"
	case 30 * time.Minute:
		return "30m"
	case 1 * time.Hour:
		return "1h"
	case 2 * time.Hour:
		return "2h"
	case 4 * time.Hour:
		return "4h"
	case 6 * time.Hour:
		return "6h"
	case 8 * time.Hour:
		return "8h"
	case 12 * time.Hour:
		return "12h"
	case 24 * time.Hour:
		return "1d"
	default:
		return "15m" // 기본값
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

// formatUSDT는 USDT 금액을 포맷팅합니다
func formatUSDT(amount float64) string {
	return fmt.Sprintf("%.2f USDT", amount)
}
