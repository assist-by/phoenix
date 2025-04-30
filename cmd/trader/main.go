package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	osSignal "os/signal"
	"syscall"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	eBinance "github.com/assist-by/phoenix/internal/exchange/binance"
	"github.com/assist-by/phoenix/internal/market"
	"github.com/assist-by/phoenix/internal/notification/discord"
	pBinance "github.com/assist-by/phoenix/internal/position/binance"
	"github.com/assist-by/phoenix/internal/scheduler"
	"github.com/assist-by/phoenix/internal/strategy"
	"github.com/assist-by/phoenix/internal/strategy/macdsarema"
)

// CollectorTask는 데이터 수집 작업을 정의합니다
type CollectorTask struct {
	collector *market.Collector
	discord   *discord.Client
}

// Execute는 데이터 수집 작업을 실행합니다
func (t *CollectorTask) Execute(ctx context.Context) error {
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
	// 명령줄 플래그 정의
	testLongFlag := flag.Bool("testlong", false, "롱 포지션 테스트 후 종료")
	testShortFlag := flag.Bool("testshort", false, "숏 포지션 테스트 후 종료")

	// 플래그 파싱
	flag.Parse()

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
	binanceClient := eBinance.NewClient(
		apiKey,
		secretKey,
		eBinance.WithTimeout(10*time.Second),
		eBinance.WithTestnet(cfg.Binance.UseTestnet),
	)
	// 바이낸스 서버와 시간 동기화
	if err := binanceClient.SyncTime(ctx); err != nil {
		log.Printf("바이낸스 서버 시간 동기화 실패: %v", err)
		if err := discordClient.SendError(fmt.Errorf("바이낸스 서버 시간 동기화 실패: %w", err)); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		os.Exit(1)
	}

	// 전략 레지스트리 생성
	strategyRegistry := strategy.NewRegistry()

	// MACD+SAR+EMA 전략 등록
	macdsarema.RegisterStrategy(strategyRegistry)

	// 전략 설정
	strategyConfig := map[string]interface{}{
		"emaLength":      200,
		"stopLossPct":    0.02,
		"takeProfitPct":  0.04,
		"minHistogram":   0.00005,
		"maxWaitCandles": 3,
	}

	// 전략 인스턴스 생성
	tradingStrategy, err := strategyRegistry.Create("MACD+SAR+EMA", strategyConfig)
	if err != nil {
		log.Fatalf("전략 생성 실패: %v", err)
	}

	// 전략 초기화
	tradingStrategy.Initialize(context.Background())

	// 포지션 매니저 생성
	positionManager := pBinance.NewManager(
		binanceClient,
		discordClient,
		tradingStrategy,
	)

	// 데이터 수집기 생성 (detector 대신 tradingStrategy 사용)
	collector := market.NewCollector(
		binanceClient,
		discordClient,
		tradingStrategy,
		positionManager,
		cfg,
		market.WithRetryConfig(market.RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
			Factor:     2.0,
		}),
	)

	// // 시그널 감지기 생성
	// detector := signal.NewDetector(signal.DetectorConfig{
	// 	EMALength:      200,
	// 	StopLossPct:    0.02,
	// 	TakeProfitPct:  0.04,
	// 	MinHistogram:   0.00005,
	// 	MaxWaitCandles: 3, // 대기 상태 최대 캔들 수 설정
	// })

	// 테스트 모드 실행 (플래그 기반)
	if *testLongFlag || *testShortFlag {
		testType := "Long"
		signalType := domain.Long

		if *testShortFlag {
			testType = "Short"
			signalType = domain.Short
		}

		// 테스트할 심볼
		symbol := "BTCUSDT"

		// 현재 가격 정보 가져오기
		candles, err := binanceClient.GetKlines(ctx, symbol, "1m", 1)
		if err != nil {
			log.Fatalf("가격 정보 조회 실패: %v", err)
		}
		currentPrice := candles[0].Close

		// 테스트 시그널 생성
		var testSignal *strategy.Signal

		if signalType == domain.Long {
			testSignal = &strategy.Signal{
				Type:       domain.Long,
				Symbol:     symbol,
				Price:      currentPrice,
				Timestamp:  time.Now(),
				StopLoss:   currentPrice * 0.99, // 가격의 99% (1% 손절)
				TakeProfit: currentPrice * 1.01, // 가격의 101% (1% 익절)
				Conditions: map[string]interface{}{
					"EMALong":     true,
					"EMAShort":    false,
					"MACDLong":    true,
					"MACDShort":   false,
					"SARLong":     true,
					"SARShort":    false,
					"EMAValue":    currentPrice * 0.95, // 예시 값
					"MACDValue":   0.0015,              // 예시 값
					"SignalValue": 0.0010,              // 예시 값
					"SARValue":    currentPrice * 0.98, // 예시 값
				},
			}
		} else {
			testSignal = &strategy.Signal{
				Type:       domain.Short,
				Symbol:     symbol,
				Price:      currentPrice,
				Timestamp:  time.Now(),
				StopLoss:   currentPrice * 1.01, // 가격의 101% (1% 손절)
				TakeProfit: currentPrice * 0.99, // 가격의 99% (1% 익절)
				Conditions: map[string]interface{}{
					"EMALong":     false,
					"EMAShort":    true,
					"MACDLong":    false,
					"MACDShort":   true,
					"SARLong":     false,
					"SARShort":    true,
					"EMAValue":    currentPrice * 1.05, // 예시 값
					"MACDValue":   -0.0015,             // 예시 값
					"SignalValue": -0.0010,             // 예시 값
					"SARValue":    currentPrice * 1.02, // 예시 값
				},
			}
		}

		// 시그널 알림 전송
		if err := discordClient.SendSignal(testSignal); err != nil {
			log.Printf("시그널 알림 전송 실패: %v", err)
		}

		// executeSignalTrade 직접 호출
		if err := collector.ExecuteSignalTrade(ctx, testSignal); err != nil {
			log.Printf("테스트 매매 실행 중 에러 발생: %v", err)
			if err := discordClient.SendError(err); err != nil {
				log.Printf("에러 알림 전송 실패: %v", err)
			}
			os.Exit(1)
		}

		// 테스트 성공 알림 및 종료
		if err := discordClient.SendInfo(fmt.Sprintf("✅ 테스트 %s 실행 완료. 프로그램을 종료합니다.", testType)); err != nil {
			log.Printf("종료 알림 전송 실패: %v", err)
		}

		log.Println("프로그램을 종료합니다.")
		os.Exit(0)
	}

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
