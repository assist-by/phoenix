package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	osSignal "os/signal"
	"syscall"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/market"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
	"github.com/assist-by/phoenix/internal/scheduler"
)

// CollectorTask는 데이터 수집 작업을 정의합니다
type CollectorTask struct {
	collector *market.Collector
	discord   *discord.Client
}

// Execute는 데이터 수집 작업을 실행합니다
func (t *CollectorTask) Execute(ctx context.Context) error {
	// 작업 시작 알림
	if err := t.discord.SendInfo("📊 데이터 수집 시작"); err != nil {
		log.Printf("작업 시작 알림 전송 실패: %v", err)
	}

	// 데이터 수집 실행
	if err := t.collector.Collect(ctx); err != nil {
		if err := t.discord.SendError(err); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		return err
	}

	return nil
}

func main() {
	// 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 로그 설정
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("트레이딩 봇 시작...")

	// 설정 로드
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	// API 키 선택
	apiKey := cfg.Binance.APIKey
	secretKey := cfg.Binance.SecretKey

	// Discord 클라이언트 생성
	discordClient := discord.NewClient(
		cfg.Discord.SignalWebhook,
		cfg.Discord.TradeWebhook,
		cfg.Discord.ErrorWebhook,
		cfg.Discord.InfoWebhook,
		discord.WithTimeout(10*time.Second),
	)

	// 시작 알림 전송
	if err := discordClient.SendInfo("🚀 트레이딩 봇이 시작되었습니다."); err != nil {
		log.Printf("시작 알림 전송 실패: %v", err)
	}

	// 테스트넷 사용 시 테스트넷 API 키로 변경
	if cfg.Binance.UseTestnet {
		apiKey = cfg.Binance.TestAPIKey
		secretKey = cfg.Binance.TestSecretKey

		discordClient.SendInfo("⚠️ 테스트넷 모드로 실행 중입니다. 실제 자산은 사용되지 않습니다.")
	} else {
		discordClient.SendInfo("⚠️ 메인넷 모드로 실행 중입니다. 실제 자산이 사용됩니다!")
	}

	// 바이낸스 클라이언트 생성
	binanceClient := market.NewClient(
		apiKey,
		secretKey,
		market.WithTimeout(10*time.Second),
		market.WithTestnet(cfg.Binance.UseTestnet),
	)
	// 바이낸스 서버와 시간 동기화
	if err := binanceClient.SyncTime(ctx); err != nil {
		log.Printf("바이낸스 서버 시간 동기화 실패: %v", err)
		if err := discordClient.SendError(fmt.Errorf("바이낸스 서버 시간 동기화 실패: %w", err)); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		os.Exit(1)
	}

	// 시그널 감지기 생성
	detector := signal.NewDetector(signal.DetectorConfig{
		EMALength:      200,
		StopLossPct:    0.02,
		TakeProfitPct:  0.04,
		MinHistogram:   0.00005,
		MaxWaitCandles: 3, // 대기 상태 최대 캔들 수 설정
	})

	if cfg.App.BuyMode {
		// Buy Mode 실행
		log.Println("Buy Mode 활성화: 1회 매수 후 종료합니다")

		// 매수 작업 생성
		buyTask := &BuyTask{
			client:   binanceClient,
			discord:  discordClient,
			detector: detector,
			config:   cfg,
		}

		// 매수 실행
		if err := buyTask.Execute(ctx); err != nil {
			log.Printf("매수 실행 중 에러 발생: %v", err)
			if err := discordClient.SendError(err); err != nil {
				log.Printf("에러 알림 전송 실패: %v", err)
			}
			os.Exit(1)
		}

		// 매수 성공 알림 및 종료
		if err := discordClient.SendInfo("✅ 1회 매수 실행 완료. 프로그램을 종료합니다."); err != nil {
			log.Printf("종료 알림 전송 실패: %v", err)
		}

		log.Println("프로그램을 종료합니다.")
		os.Exit(0)
	}

	// 데이터 수집기 생성
	collector := market.NewCollector(
		binanceClient,
		discordClient,
		detector,
		cfg,
		market.WithRetryConfig(market.RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
			Factor:     2.0,
		}),
	)

	// 수집 작업 생성
	task := &CollectorTask{
		collector: collector,
		discord:   discordClient,
	}

	// 스케줄러 생성 (fetchInterval)
	scheduler := scheduler.NewScheduler(cfg.App.FetchInterval, task)

	// 시그널 처리
	sigChan := make(chan os.Signal, 1)
	osSignal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 스케줄러 시작
	go func() {
		if err := scheduler.Start(ctx); err != nil {
			log.Printf("스케줄러 실행 중 에러 발생: %v", err)
			if err := discordClient.SendError(err); err != nil {
				log.Printf("에러 알림 전송 실패: %v", err)
			}
		}
	}()

	// 시그널 대기
	sig := <-sigChan
	log.Printf("시스템 종료 신호 수신: %v", sig)

	// 스케줄러 중지
	scheduler.Stop()

	// 종료 알림 전송
	if err := discordClient.SendInfo("👋 트레이딩 봇이 정상적으로 종료되었습니다."); err != nil {
		log.Printf("종료 알림 전송 실패: %v", err)
	}

	log.Println("프로그램을 종료합니다.")
}

////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////

// BuyTask는 1회 매수 작업을 정의합니다
type BuyTask struct {
	client   *market.Client
	discord  *discord.Client
	detector *signal.Detector
	config   *config.Config
}

// BuyTask의 Execute 함수 시작 부분에 추가
func (t *BuyTask) Execute(ctx context.Context) error {
	// 심볼 설정 (BTCUSDT 고정)
	symbol := "BTCUSDT"

	// 작업 시작 알림
	if err := t.discord.SendInfo(fmt.Sprintf("🚀 %s 1회 매수 시작", symbol)); err != nil {
		log.Printf("작업 시작 알림 전송 실패: %v", err)
	}

	// 1. 기존 포지션 확인
	positions, err := t.client.GetPositions(ctx)
	if err != nil {
		return fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 기존 포지션이 있는지 확인
	for _, pos := range positions {
		if pos.Symbol == symbol && pos.Quantity != 0 {
			return fmt.Errorf("이미 %s에 대한 포지션이 있습니다. 수량: %.8f, 방향: %s",
				pos.Symbol, pos.Quantity, pos.PositionSide)
		}
	}

	// 2. 열린 주문 확인
	openOrders, err := t.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("주문 조회 실패: %w", err)
	}

	// 기존 TP/SL 주문이 있는지 확인
	if len(openOrders) > 0 {
		// 기존 주문 취소
		log.Printf("기존 주문 %d개를 취소합니다.", len(openOrders))
		for _, order := range openOrders {
			if err := t.client.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				// 취소 실패해도 계속 진행
			} else {
				log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
			}
		}
	}

	// 매수 실행 로직
	// 1. 잔고 조회
	balances, err := t.client.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("잔고 조회 실패: %w", err)
	}

	// 2. USDT 잔고 확인
	usdtBalance, exists := balances["USDT"]
	if !exists || usdtBalance.Available <= 0 {
		return fmt.Errorf("USDT 잔고가 부족합니다")
	}

	// 3. 현재 가격 조회 (최근 캔들 사용)
	candles, err := t.client.GetKlines(ctx, symbol, "1m", 1)
	if err != nil {
		return fmt.Errorf("가격 정보 조회 실패: %w", err)
	}

	if len(candles) == 0 {
		return fmt.Errorf("캔들 데이터를 가져오지 못했습니다")
	}

	currentPrice := candles[0].Close

	// 4. 심볼 정보 조회
	symbolInfo, err := t.client.GetSymbolInfo(ctx, symbol)
	if err != nil {
		return fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// 5. HEDGE 모드 설정
	if err := t.client.SetPositionMode(ctx, true); err != nil {
		return fmt.Errorf("HEDGE 모드 설정 실패: %w", err)
	}

	// 6. 레버리지 설정 (5배 고정)
	leverage := 5
	if err := t.client.SetLeverage(ctx, symbol, leverage); err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	// 7. 매수 수량 계산 (잔고의 90% 사용)
	// CollectPosition 함수와 동일한 로직 사용
	collector := market.NewCollector(t.client, t.discord, t.detector, t.config)

	// 레버리지 브라켓 정보 조회
	brackets, err := t.client.GetLeverageBrackets(ctx, symbol)
	if err != nil {
		return fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 해당 심볼의 브라켓 정보 찾기
	var symbolBracket *market.SymbolBrackets
	for _, b := range brackets {
		if b.Symbol == symbol {
			symbolBracket = &b
			break
		}
	}

	if symbolBracket == nil || len(symbolBracket.Brackets) == 0 {
		return fmt.Errorf("레버리지 브라켓 정보가 없습니다")
	}

	// 설정된 레버리지에 맞는 브라켓 찾기
	bracket := findBracket(symbolBracket.Brackets, leverage)
	if bracket == nil {
		return fmt.Errorf("적절한 레버리지 브라켓을 찾을 수 없습니다")
	}

	// 포지션 크기 계산
	positionResult := collector.CalculatePosition(
		usdtBalance.Available,
		leverage,
		currentPrice,
		symbolInfo.StepSize,
		bracket.MaintMarginRatio,
	)

	// 최소 주문 가치 체크
	if positionResult.PositionValue < symbolInfo.MinNotional {
		return fmt.Errorf("포지션 크기가 최소 주문 가치(%.2f USDT)보다 작습니다", symbolInfo.MinNotional)
	}

	// 8. 주문 수량 정밀도 조정
	adjustedQuantity := market.AdjustQuantity(
		positionResult.Quantity,
		symbolInfo.StepSize,
		symbolInfo.QuantityPrecision,
	)

	// 9. 매수 주문 생성 (LONG 포지션)
	entryOrder := market.OrderRequest{
		Symbol:       symbol,
		Side:         market.Buy,
		PositionSide: market.Long,
		Type:         market.Market,
		Quantity:     adjustedQuantity,
	}

	// 10. 매수 주문 실행
	orderResponse, err := t.client.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return fmt.Errorf("주문 실행 실패: %w", err)
	}

	// 11. 성공 메시지 출력 및 로깅
	log.Printf("매수 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		symbol, adjustedQuantity, orderResponse.OrderID)

	// 12. 포지션 확인 및 TP/SL 설정
	// 포지션이 실제로 생성되었는지 확인
	maxRetries := 5
	retryInterval := 1 * time.Second
	var position *market.PositionInfo

	for i := 0; i < maxRetries; i++ {
		positions, err := t.client.GetPositions(ctx)
		if err != nil {
			log.Printf("포지션 조회 실패 (시도 %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryInterval)
			continue
		}

		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionSide == "LONG" && pos.Quantity > 0 {
				position = &pos
				log.Printf("포지션 확인: %s LONG, 수량: %.8f, 진입가: %.2f",
					pos.Symbol, pos.Quantity, pos.EntryPrice)
				break
			}
		}

		if position != nil {
			break
		}

		log.Printf("아직 포지션이 생성되지 않음 (시도 %d/%d), 대기 중...", i+1, maxRetries)
		time.Sleep(retryInterval)
		retryInterval *= 2 // 지수 백오프
	}

	if position == nil {
		return fmt.Errorf("최대 재시도 횟수 초과: 포지션을 찾을 수 없음")
	}

	// 13. TP/SL 설정 (1% 고정)
	actualEntryPrice := position.EntryPrice
	actualQuantity := position.Quantity

	// 원래 계산
	rawStopLoss := actualEntryPrice * 0.999   // 진입가 -1%
	rawTakeProfit := actualEntryPrice * 1.001 // 진입가 +1%

	// 가격 정밀도에 맞게 조정
	// symbolInfo.TickSize와 symbolInfo.PricePrecision 사용
	stopLoss := AdjustPrice(rawStopLoss, symbolInfo.TickSize, symbolInfo.PricePrecision)
	takeProfit := AdjustPrice(rawTakeProfit, symbolInfo.TickSize, symbolInfo.PricePrecision)

	// TP/SL 설정 알림
	if err := t.discord.SendInfo(fmt.Sprintf(
		"TP/SL 설정 중: %s\n진입가: %.2f\n수량: %.8f\n손절가: %.2f (-1%%)\n목표가: %.2f (+1%%)",
		symbol, actualEntryPrice, actualQuantity, stopLoss, takeProfit)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	// 14. TP/SL 주문 생성
	slOrder := market.OrderRequest{
		Symbol:       symbol,
		Side:         market.Sell,
		PositionSide: market.Long,
		Type:         market.StopMarket,
		Quantity:     actualQuantity,
		StopPrice:    stopLoss,
	}

	// 손절 주문 실행 전에 로깅 추가
	log.Printf("손절(SL) 주문 생성: 심볼=%s, 가격=%.2f, 수량=%.8f",
		slOrder.Symbol, slOrder.StopPrice, slOrder.Quantity)

	// 손절 주문 실행
	slResponse, err := t.client.PlaceOrder(ctx, slOrder)
	if err != nil {
		log.Printf("손절(SL) 주문 실패: %v", err)
		return fmt.Errorf("손절(SL) 주문 실패: %w", err)
	}
	log.Printf("손절(SL) 주문 성공: ID=%d", slResponse.OrderID)

	// 익절 주문 생성
	tpOrder := market.OrderRequest{
		Symbol:       symbol,
		Side:         market.Sell,
		PositionSide: market.Long,
		Type:         market.TakeProfitMarket,
		Quantity:     actualQuantity,
		StopPrice:    takeProfit,
	}

	// 익절 주문 생성 전에 로깅 추가
	log.Printf("익절(TP) 주문 생성: 심볼=%s, 가격=%.2f, 수량=%.8f",
		tpOrder.Symbol, tpOrder.StopPrice, tpOrder.Quantity)

	// 익절 주문 실행
	tpResponse, err := t.client.PlaceOrder(ctx, tpOrder)
	if err != nil {
		log.Printf("익절(TP) 주문 실패: %v", err)
		return fmt.Errorf("익절(TP) 주문 실패: %w", err)
	}
	log.Printf("익절(TP) 주문 성공: ID=%d", tpResponse.OrderID)

	// 15. TP/SL 설정 완료 알림
	if err := t.discord.SendInfo(fmt.Sprintf("✅ TP/SL 설정 완료: %s", symbol)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	// TradeInfo 생성 및 전송
	tradeInfo := notification.TradeInfo{
		Symbol:        symbol,
		PositionType:  "LONG",
		PositionValue: positionResult.PositionValue,
		Quantity:      adjustedQuantity,
		EntryPrice:    currentPrice,
		StopLoss:      stopLoss,
		TakeProfit:    takeProfit,
		Balance:       usdtBalance.Available - positionResult.PositionValue,
		Leverage:      leverage,
	}

	if err := t.discord.SendTradeInfo(tradeInfo); err != nil {
		log.Printf("거래 정보 알림 전송 실패: %v", err)
	}

	// 16. 최종 열린 주문 확인
	openOrders, err = t.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		log.Printf("열린 주문 조회 실패: %v", err)
		// 오류가 발생해도 계속 진행
	} else {
		log.Printf("현재 열린 주문 상태 (총 %d개):", len(openOrders))

		var tpCount, slCount int

		for _, order := range openOrders {
			if order.Symbol == symbol && order.PositionSide == "LONG" {
				orderType := ""
				if order.Type == "TAKE_PROFIT_MARKET" {
					orderType = "TP"
					tpCount++
				} else if order.Type == "STOP_MARKET" {
					orderType = "SL"
					slCount++
				}

				if orderType != "" {
					log.Printf("- %s 주문: ID=%d, 가격=%.2f, 수량=%.8f",
						orderType, order.OrderID, order.StopPrice, order.OrigQuantity)
				}
			}
		}

		if tpCount > 0 && slCount > 0 {
			log.Printf("✅ TP/SL 주문이 모두 성공적으로 확인되었습니다!")
			if err := t.discord.SendInfo("✅ 최종 확인: TP/SL 주문이 모두 성공적으로 설정되었습니다!"); err != nil {
				log.Printf("최종 확인 알림 전송 실패: %v", err)
			}
		} else {
			errorMsg := fmt.Sprintf("⚠️ 주의: TP 주문 %d개, SL 주문 %d개 확인됨 (예상: 각 1개)", tpCount, slCount)
			log.Printf(errorMsg)
			if err := t.discord.SendInfo(errorMsg); err != nil {
				log.Printf("주의 알림 전송 실패: %v", err)
			}
		}
	}

	return nil
}

// findBracket은 주어진 레버리지에 해당하는 브라켓을 찾습니다
func findBracket(brackets []market.LeverageBracket, leverage int) *market.LeverageBracket {
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

// 추가할 AdjustPrice 함수
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
