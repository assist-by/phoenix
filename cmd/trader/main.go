package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	osSignal "os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/assist-by/phoenix/internal/backtest"
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

	backtestFlag := flag.Bool("backtest", false, "백테스트 모드로 실행")

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

	// 백테스트 모드 처리
	if *backtestFlag {
		runBacktest(ctx, cfg, discordClient, binanceClient, strategyRegistry)
		return
	}

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
		var testSignal domain.SignalInterface

		if signalType == domain.Long {
			testSignal = macdsarema.NewMACDSAREMASignal(
				domain.Long,
				symbol,
				currentPrice,
				time.Now(),
				currentPrice*0.99, // 가격의 99% (1% 손절)
				currentPrice*1.01, // 가격의 101% (1% 익절)
			)
			// 추가 필드 설정
			macdSignal := testSignal.(*macdsarema.MACDSAREMASignal)
			macdSignal.EMAValue = currentPrice * 0.95
			macdSignal.MACDValue = 0.0015
			macdSignal.SignalValue = 0.0010
			macdSignal.SARValue = currentPrice * 0.98
			macdSignal.EMAAbove = true
			macdSignal.SARBelow = true
			macdSignal.MACDCross = 1
		} else {
			testSignal = macdsarema.NewMACDSAREMASignal(
				domain.Short,
				symbol,
				currentPrice,
				time.Now(),
				currentPrice*1.01, // 가격의 101% (1% 손절)
				currentPrice*0.99, // 가격의 99% (1% 익절)
			)
			// 추가 필드 설정
			macdSignal := testSignal.(*macdsarema.MACDSAREMASignal)
			macdSignal.EMAValue = currentPrice * 1.05
			macdSignal.MACDValue = -0.0015
			macdSignal.SignalValue = -0.0010
			macdSignal.SARValue = currentPrice * 1.02
			macdSignal.EMAAbove = false
			macdSignal.SARBelow = false
			macdSignal.MACDCross = -1
		}

		// 시그널 알림 전송
		if err := discordClient.SendSignal(testSignal); err != nil {
			log.Printf("시그널 알림 전송 실패: %v", err)
		}

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

func runBacktest(ctx context.Context, config *config.Config, discordClient *discord.Client, binanceClient *eBinance.Client, strategyRegistry *strategy.Registry) {
	// 백테스트 설정 로깅
	log.Printf("백테스트 시작: 전략=%s, 심볼=%s, 기간=%d일, 간격=%s",
		config.Backtest.Strategy,
		config.Backtest.Symbol,
		config.Backtest.Days,
		config.Backtest.Interval,
	)

	// 지정된 전략이 등록되어 있는지 확인
	availableStrategies := strategyRegistry.ListStrategies()
	strategyFound := false
	for _, s := range availableStrategies {
		if s == config.Backtest.Strategy {
			strategyFound = true
			break
		}
	}

	if !strategyFound {
		errMsg := fmt.Sprintf("지정된 전략 '%s'을(를) 찾을 수 없습니다. 사용 가능한 전략: %v",
			config.Backtest.Strategy, availableStrategies)
		log.Println(errMsg)
		if discordClient != nil {
			discordClient.SendError(fmt.Errorf(errMsg))
		}
		return
	}

	// 전략 생성
	strategyConfig := map[string]interface{}{
		"emaLength":      200, // 기본값, 나중에 환경변수로 확장 가능
		"stopLossPct":    0.02,
		"takeProfitPct":  0.04,
		"minHistogram":   0.00005,
		"maxWaitCandles": 3,
	}

	tradingStrategy, err := strategyRegistry.Create(config.Backtest.Strategy, strategyConfig)
	if err != nil {
		errMsg := fmt.Sprintf("전략 생성 실패: %v", err)
		log.Println(errMsg)
		if discordClient != nil {
			discordClient.SendError(fmt.Errorf(errMsg))
		}
		return
	}

	// 전략 초기화
	tradingStrategy.Initialize(ctx)

	// 심볼 정보 조회
	symbol := config.Backtest.Symbol
	symbolInfo, err := binanceClient.GetSymbolInfo(ctx, symbol)
	if err != nil {
		errMsg := fmt.Sprintf("심볼 정보 조회 실패: %v", err)
		log.Println(errMsg)
		if discordClient != nil {
			discordClient.SendError(fmt.Errorf(errMsg))
		}
		return
	}

	// 캔들 데이터 로드
	days := config.Backtest.Days
	interval := domain.TimeInterval(config.Backtest.Interval)

	// 필요한 캔들 개수 계산 (일별 캔들 수 * 일수 + 여유분)
	candlesPerDay := 24 * 60 / domain.TimeIntervalToDuration(interval).Minutes()
	requiredCandles := int(candlesPerDay*float64(days)) + 200 // 지표 계산을 위한 여유분

	// 데이터 로드
	log.Printf("바이낸스에서 %d개의 캔들 데이터를 로드합니다...", requiredCandles)
	candles, err := binanceClient.GetKlines(ctx, symbol, interval, requiredCandles)
	if err != nil {
		errMsg := fmt.Sprintf("캔들 데이터 로드 실패: %v", err)
		log.Println(errMsg)
		if discordClient != nil {
			discordClient.SendError(fmt.Errorf(errMsg))
		}
		return
	}
	log.Printf("%d개의 캔들 데이터를 성공적으로 로드했습니다.", len(candles))

	// 캔들 데이터 시간순 정렬
	sort.Slice(candles, func(i, j int) bool {
		return candles[i].OpenTime.Before(candles[j].OpenTime)
	})

	log.Printf("정렬된 캔들 데이터 기간: %s ~ %s",
		candles[0].OpenTime.Format("2006-01-02 15:04:05"),
		candles[len(candles)-1].CloseTime.Format("2006-01-02 15:04:05"))

	// 백테스트 엔진 초기화
	log.Printf("백테스트 엔진을 초기화하는 중...")
	engine := backtest.NewEngine(config, tradingStrategy, symbolInfo, candles, symbol, interval)

	// 백테스트 실행
	log.Printf("백테스트 실행 중...")
	result, err := engine.Run()
	if err != nil {
		errMsg := fmt.Sprintf("백테스트 실행 실패: %v", err)
		log.Println(errMsg)
		if discordClient != nil {
			discordClient.SendError(fmt.Errorf(errMsg))
		}
		return
	}

	// 결과 출력
	printBacktestResult(result)

	// Discord 알림 (옵션)
	if discordClient != nil {
		sendBacktestResultToDiscord(discordClient, result, symbol, interval)
	}
}

// printBacktestResult는 백테스트 결과를 콘솔에 출력합니다
func printBacktestResult(result *backtest.Result) {
	fmt.Println("\n--------------------------------")
	fmt.Println("        백테스트 결과")
	fmt.Println("--------------------------------")
	fmt.Printf("심볼: %s, 간격: %s\n", result.Symbol, result.Interval)
	fmt.Printf("기간: %s ~ %s\n",
		result.StartTime.Format("2006-01-02 15:04:05"),
		result.EndTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("총 거래 횟수: %d\n", result.TotalTrades)
	fmt.Printf("승률: %.2f%% (%d승 %d패)\n",
		result.WinRate, result.WinningTrades, result.LosingTrades)
	fmt.Printf("평균 수익률: %.2f%%\n", result.AverageReturn)
	fmt.Printf("누적 수익률: %.2f%%\n", result.CumulativeReturn)
	fmt.Printf("최대 낙폭: %.2f%%\n", result.MaxDrawdown)
	fmt.Println("--------------------------------")

	// 거래 요약 (옵션)
	if len(result.Trades) > 0 {
		fmt.Println("\n거래 요약:")
		fmt.Println("--------------------------------")

		// 각 열의 너비를 정의합니다
		const (
			timeWidth     = 20
			sideWidth     = 12
			profitWidth   = 12
			reasonWidth   = 18
			durationWidth = 15
		)

		// 열 제목 출력 (정확한 너비와 정렬로)
		fmt.Printf("%-*s %-*s %*s %-*s %-*s\n",
			timeWidth, "시간",
			sideWidth, "방향",
			profitWidth, "수익률",
			reasonWidth, "청산이유",
			durationWidth, "보유기간")

		for i, trade := range result.Trades {
			// 최대 20개 거래만 출력
			if i >= 20 {
				fmt.Printf("... 외 %d개 거래\n", len(result.Trades)-20)
				break
			}

			duration := trade.ExitTime.Sub(trade.EntryTime)
			hours := int(duration.Hours())
			minutes := int(duration.Minutes()) % 60

			// 청산이유가 비어있으면 "알 수 없음"으로 표시
			exitReason := trade.ExitReason
			if exitReason == "" {
				exitReason = "알 수 없음"
			}

			// 각 열을 정확한 너비와 정렬로 출력
			fmt.Printf("%-*s %-*s %*s %-*s %-*s\n",
				timeWidth, trade.EntryTime.Format("2006-01-02 15:04"),
				sideWidth, string(trade.Side),
				profitWidth, fmt.Sprintf("%+.2f%%", trade.ProfitPct),
				reasonWidth, exitReason,
				durationWidth, fmt.Sprintf("%d시간 %d분", hours, minutes))
		}
		fmt.Println("--------------------------------")
	}
}

// sendBacktestResultToDiscord는 백테스트 결과를 Discord로 전송합니다
func sendBacktestResultToDiscord(client *discord.Client, result *backtest.Result, symbol string, interval domain.TimeInterval) {
	message := fmt.Sprintf(
		"## 백테스트 결과: %s (%s)\n"+
			"**기간**: %s ~ %s\n"+
			"**총 거래**: %d (승: %d, 패: %d)\n"+
			"**승률**: %.2f%%\n"+
			"**누적 수익률**: %.2f%%\n"+
			"**최대 낙폭**: %.2f%%\n"+
			"**평균 수익률**: %.2f%%",
		symbol, interval,
		result.StartTime.Format("2006-01-02"),
		result.EndTime.Format("2006-01-02"),
		result.TotalTrades, result.WinningTrades, result.LosingTrades,
		result.WinRate,
		result.CumulativeReturn,
		result.MaxDrawdown,
		result.AverageReturn,
	)

	client.SendInfo(message)
}
