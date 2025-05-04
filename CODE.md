# phoenix
## Project Structure

```
phoenix/
├── cmd/
    └── trader/
    │   └── main.go
└── internal/
    ├── backtest/
        ├── engine.go
        ├── indicators.go
        ├── manager.go
        └── types.go
    ├── config/
        └── config.go
    ├── domain/
        ├── account.go
        ├── candle.go
        ├── order.go
        ├── signal.go
        ├── timeframe.go
        ├── types.go
        └── utils.go
    ├── exchange/
        ├── binance/
        │   └── client.go
        └── exchange.go
    ├── indicator/
        ├── ema.go
        ├── indicator.go
        ├── macd.go
        ├── rsi.go
        └── sar.go
    ├── market/
        ├── client.go
        ├── collector.go
        └── types.go
    ├── notification/
        ├── discord/
        │   ├── client.go
        │   ├── embed.go
        │   └── webhook.go
        └── types.go
    ├── position/
        ├── binance/
        │   └── manager.go
        ├── errors.go
        ├── manager.go
        ├── sizing.go
        └── utils.go
    ├── scheduler/
        └── scheduler.go
    └── strategy/
        ├── macdsarema/
            ├── init.go
            ├── signal.go
            └── strategy.go
        └── strategy.go
```

## cmd/trader/main.go
```go
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

		// 각 열의 적절한 패딩값 구하기
		timeColWidth := findMaxWidth(result.Trades, func(t backtest.Trade) string {
			return t.EntryTime.Format("2006-01-02 15:04")
		}, "시간")

		sideColWidth := findMaxWidth(result.Trades, func(t backtest.Trade) string {
			return string(t.Side)
		}, "방향")

		profitColWidth := findMaxWidth(result.Trades, func(t backtest.Trade) string {
			return fmt.Sprintf("%+.2f%%", t.ProfitPct)
		}, "수익률")

		reasonColWidth := findMaxWidth(result.Trades, func(t backtest.Trade) string {
			if t.ExitReason == "" {
				return "알 수 없음"
			}
			return t.ExitReason
		}, "청산이유")

		durationColWidth := findMaxWidth(result.Trades, func(t backtest.Trade) string {
			duration := t.ExitTime.Sub(t.EntryTime)
			hours := int(duration.Hours())
			minutes := int(duration.Minutes()) % 60
			return fmt.Sprintf("%d시간 %d분", hours, minutes)
		}, "보유기간")

		// 헤더 출력
		format := fmt.Sprintf("%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds\n",
			timeColWidth, sideColWidth, profitColWidth, reasonColWidth, durationColWidth)

		fmt.Printf(format, "Time", "Side", "Profit", "Exit Reason", "Duration")

		// 데이터 출력
		for i, trade := range result.Trades {
			if i >= 20 {
				fmt.Printf("... 외 %d개 거래\n", len(result.Trades)-20)
				break
			}

			duration := trade.ExitTime.Sub(trade.EntryTime)
			hours := int(duration.Hours())
			minutes := int(duration.Minutes()) % 60

			exitReason := trade.ExitReason
			if exitReason == "" {
				exitReason = "알 수 없음"
			}

			fmt.Printf(format,
				trade.EntryTime.Format("2006-01-02 15:04"),
				string(trade.Side),
				fmt.Sprintf("%+.2f%%", trade.ProfitPct),
				exitReason,
				fmt.Sprintf("%d시간 %d분", hours, minutes))
		}
		fmt.Println("--------------------------------")
	}
}

// findMaxWidth는 지정된 함수를 사용하여 최대 너비를 찾습니다
func findMaxWidth(trades []backtest.Trade, getField func(backtest.Trade) string, header string) int {
	maxLen := len(header)

	for _, trade := range trades {
		fieldLen := len(getField(trade))
		if fieldLen > maxLen {
			maxLen = fieldLen
		}
	}

	// 최소 패딩 추가
	return maxLen + 5
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

```
## internal/backtest/engine.go
```go
package backtest

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// Engine은 백테스트 실행 엔진입니다
type Engine struct {
	Strategy       strategy.Strategy   // 테스트할 전략
	Manager        *Manager            // 백테스트 포지션 관리자
	SymbolInfo     *domain.SymbolInfo  // 심볼 정보
	Candles        domain.CandleList   // 캔들 데이터
	Config         *config.Config      // 백테스트 설정
	IndicatorCache *IndicatorCache     // 지표 캐시
	StartTime      time.Time           // 백테스트 시작 시간
	EndTime        time.Time           // 백테스트 종료 시간
	Symbol         string              // 테스트 심볼
	Interval       domain.TimeInterval // 테스트 간격
	WarmupPeriod   int                 // 웜업 기간 (캔들 수)
}

// NewEngine은 새로운 백테스트 엔진을 생성합니다
func NewEngine(
	cfg *config.Config,
	strategy strategy.Strategy,
	symbolInfo *domain.SymbolInfo,
	candles domain.CandleList,
	symbol string,
	interval domain.TimeInterval,
) *Engine {
	// 백테스트 시작/종료 시간 결정
	startTime := candles[0].OpenTime
	endTime := candles[len(candles)-1].CloseTime

	// 심볼 정보 맵 생성
	symbolInfos := make(map[string]*domain.SymbolInfo)
	symbolInfos[symbol] = symbolInfo

	// 포지션 관리자 생성
	manager := NewManager(cfg, symbolInfos)

	// 지표 캐시 생성
	cache := NewIndicatorCache()

	// 웜업 기간 결정 (최소 200 캔들 또는 전략에 따라 조정)
	warmupPeriod := 200
	if len(candles) < warmupPeriod {
		warmupPeriod = len(candles) / 4 // 데이터가 부족한 경우 25% 사용
	}

	return &Engine{
		Strategy:       strategy,
		Manager:        manager,
		SymbolInfo:     symbolInfo,
		Candles:        candles,
		Config:         cfg,
		IndicatorCache: cache,
		StartTime:      startTime,
		EndTime:        endTime,
		Symbol:         symbol,
		Interval:       interval,
		WarmupPeriod:   warmupPeriod,
	}
}

// Run은 백테스트를 실행합니다
func (e *Engine) Run() (*Result, error) {
	// 데이터 검증
	if len(e.Candles) < e.WarmupPeriod {
		return nil, fmt.Errorf("충분한 캔들 데이터가 없습니다: 필요 %d, 현재 %d",
			e.WarmupPeriod, len(e.Candles))
	}

	// 1. 지표 사전 계산
	if err := e.prepareIndicators(); err != nil {
		return nil, fmt.Errorf("지표 계산 실패: %w", err)
	}

	log.Printf("백테스트 시작: 심볼=%s, 간격=%s, 캔들 수=%d, 웜업 기간=%d",
		e.Symbol, e.Interval, len(e.Candles), e.WarmupPeriod)

	// 2. 순차적 캔들 처리
	ctx := context.Background()
	candleMap := make(map[string]domain.Candle)

	for i := e.WarmupPeriod; i < len(e.Candles); i++ {
		currentCandle := e.Candles[i]

		// 현재 시점까지의 데이터로 서브셋 생성 (미래 정보 누수 방지)
		subsetCandles := e.Candles[:i+1]

		// 전략 분석 및 시그널 생성
		signal, err := e.Strategy.Analyze(ctx, e.Symbol, subsetCandles)
		if err != nil {
			log.Printf("전략 분석 실패 (캔들 %d): %v", i, err)
			continue
		}

		// 신호가 있는 경우 포지션 진입
		if signal != nil && signal.GetType() != domain.NoSignal {
			if _, err := e.Manager.OpenPosition(signal, currentCandle); err != nil {
				log.Printf("포지션 진입 실패 (캔들 %d) (캔들시간: %s): %v", i, err,
					currentCandle.OpenTime.Format("2006-01-02 15:04:05"))
			} else {
				sign := 1.0
				if signal.GetType() != domain.Long {
					sign = -1.0
				}

				log.Printf("포지션 진입: %s %s @ %.2f, SL: %.2f (%.2f%%), TP: %.2f (%.2f%%) (캔들시간: %s)",
					e.Symbol, signal.GetType().String(), signal.GetPrice(),
					signal.GetStopLoss(),
					(signal.GetStopLoss()-signal.GetPrice())/signal.GetPrice()*100*sign,
					signal.GetTakeProfit(),
					(signal.GetTakeProfit()-signal.GetPrice())/signal.GetPrice()*100*sign,
					currentCandle.OpenTime.Format("2006-01-02 15:04:05"))
			}
		}

		// 기존 포지션 업데이트 (TP/SL 체크)
		closedPositions := e.Manager.UpdatePositions(currentCandle, signal)

		// 청산된 포지션 로깅
		for _, pos := range closedPositions {
			log.Printf("포지션 청산: %s %s, 수익: %.2f%%, 이유: %s (캔들시간: %s)",
				pos.Symbol,
				string(pos.Side),
				pos.PnLPercentage,
				getExitReasonString(pos.ExitReason),
				currentCandle.OpenTime.Format("2006-01-02 15:04:05"))
		}

		// 계정 자산 업데이트
		candleMap[e.Symbol] = currentCandle
		e.Manager.UpdateEquity(candleMap)
	}

	// 3. 미청산 포지션 처리
	e.closeAllPositions()

	// 4. 결과 계산 및 반환
	result := e.Manager.GetBacktestResult(e.StartTime, e.EndTime, e.Symbol, e.Interval)

	log.Printf("백테스트 완료: 총 거래=%d, 승률=%.2f%%, 누적 수익률=%.2f%%, 최대 낙폭=%.2f%%",
		result.TotalTrades,
		result.WinRate,
		result.CumulativeReturn,
		result.MaxDrawdown)

	return result, nil
}

// prepareIndicators는 지표를 미리 계산하고 캐싱합니다
func (e *Engine) prepareIndicators() error {
	// 캔들 데이터를 지표 계산용 형식으로 변환
	prices := indicator.ConvertCandlesToPriceData(e.Candles)

	// 기본 지표 집합 가져오기
	indicatorSpecs := GetDefaultIndicators()

	// 지표 계산 및 캐싱
	if err := e.IndicatorCache.CacheIndicators(indicatorSpecs, prices); err != nil {
		return fmt.Errorf("지표 캐싱 실패: %w", err)
	}

	return nil
}

// closeAllPositions은 모든 열린 포지션을 마지막 가격으로 청산합니다
func (e *Engine) closeAllPositions() {
	if len(e.Candles) == 0 {
		return
	}

	// 마지막 캔들 가져오기
	lastCandle := e.Candles[len(e.Candles)-1]

	// 모든 열린 포지션 가져오기 및 청산
	positions := e.Manager.Account.Positions

	// 복사본 생성 (청산 중 슬라이스 변경 방지)
	positionsCopy := make([]*Position, len(positions))
	copy(positionsCopy, positions)

	for _, pos := range positionsCopy {
		if err := e.Manager.ClosePosition(pos, lastCandle.Close, lastCandle.CloseTime, EndOfBacktest); err != nil {
			log.Printf("백테스트 종료 시 포지션 청산 실패: %v", err)
		} else {
			log.Printf("백테스트 종료 포지션 청산: %s %s, 수익: %.2f%%",
				pos.Symbol, string(pos.Side), pos.PnLPercentage)
		}
	}
}

// getExitReasonString은 청산 이유를 문자열로 변환합니다
func getExitReasonString(reason ExitReason) string {
	switch reason {
	case StopLossHit:
		return "손절(SL)"
	case TakeProfitHit:
		return "익절(TP)"
	case SignalReversal:
		return "신호 반전"
	case EndOfBacktest:
		return "백테스트 종료"
	default:
		return "알 수 없음"
	}
}

```
## internal/backtest/indicators.go
```go
package backtest

import (
	"fmt"
	"log"
	"sync"

	"github.com/assist-by/phoenix/internal/indicator"
)

// IndicatorCache는 다양한 지표를 캐싱하는 범용 저장소입니다
type IndicatorCache struct {
	indicators map[string][]indicator.Result // 지표 이름을 키로 하는 결과 맵
	mutex      sync.RWMutex                  // 동시성 제어
}

// NewIndicatorCache는 새로운 지표 캐시를 생성합니다
func NewIndicatorCache() *IndicatorCache {
	return &IndicatorCache{
		indicators: make(map[string][]indicator.Result),
	}
}

// CacheIndicator는 특정 지표를 계산하고 캐싱합니다
func (cache *IndicatorCache) CacheIndicator(name string, ind indicator.Indicator, prices []indicator.PriceData) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	log.Printf("지표 '%s' 계산 중...", name)
	results, err := ind.Calculate(prices)
	if err != nil {
		return fmt.Errorf("지표 '%s' 계산 실패: %w", name, err)
	}

	cache.indicators[name] = results
	log.Printf("지표 '%s' 계산 완료: %d개 결과", name, len(results))
	return nil
}

// GetIndicator는 특정 이름과 인덱스의 지표 결과를 반환합니다
func (cache *IndicatorCache) GetIndicator(name string, index int) (indicator.Result, error) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	results, exists := cache.indicators[name]
	if !exists {
		return nil, fmt.Errorf("캐시에 '%s' 지표가 없습니다", name)
	}

	if index < 0 || index >= len(results) {
		return nil, fmt.Errorf("유효하지 않은 인덱스: %d", index)
	}

	return results[index], nil
}

// HasIndicator는 특정 지표가 캐시에 있는지 확인합니다
func (cache *IndicatorCache) HasIndicator(name string) bool {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	_, exists := cache.indicators[name]
	return exists
}

// GetIndicators는 지표명 목록을 반환합니다
func (cache *IndicatorCache) GetIndicators() []string {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	var names []string
	for name := range cache.indicators {
		names = append(names, name)
	}
	return names
}

// IndicatorSpec은 지표 명세를 나타냅니다
type IndicatorSpec struct {
	Type       string                 // 지표 유형 (EMA, MACD, SAR 등)
	Parameters map[string]interface{} // 지표 파라미터
}

// CreateIndicator는 지표 명세에 따라 지표 인스턴스를 생성합니다
func CreateIndicator(spec IndicatorSpec) (indicator.Indicator, error) {
	switch spec.Type {
	case "EMA":
		period, ok := spec.Parameters["period"].(int)
		if !ok {
			return nil, fmt.Errorf("EMA에는 'period' 파라미터가 필요합니다")
		}
		return indicator.NewEMA(period), nil

	case "MACD":
		shortPeriod, shortOk := spec.Parameters["shortPeriod"].(int)
		longPeriod, longOk := spec.Parameters["longPeriod"].(int)
		signalPeriod, signalOk := spec.Parameters["signalPeriod"].(int)
		if !shortOk || !longOk || !signalOk {
			return nil, fmt.Errorf("MACD에는 'shortPeriod', 'longPeriod', 'signalPeriod' 파라미터가 필요합니다")
		}
		return indicator.NewMACD(shortPeriod, longPeriod, signalPeriod), nil

	case "SAR":
		accelInitial, initialOk := spec.Parameters["accelerationInitial"].(float64)
		accelMax, maxOk := spec.Parameters["accelerationMax"].(float64)
		if !initialOk || !maxOk {
			// 기본값 사용
			return indicator.NewDefaultSAR(), nil
		}
		return indicator.NewSAR(accelInitial, accelMax), nil

	default:
		return nil, fmt.Errorf("지원하지 않는 지표 유형: %s", spec.Type)
	}
}

// CacheIndicators는 여러 지표를 한 번에 캐싱합니다
func (cache *IndicatorCache) CacheIndicators(specs []IndicatorSpec, prices []indicator.PriceData) error {
	for _, spec := range specs {
		// 지표 이름 생성 (유형 + 파라미터)
		name := spec.Type
		switch spec.Type {
		case "EMA":
			if period, ok := spec.Parameters["period"].(int); ok {
				name = fmt.Sprintf("%s(%d)", spec.Type, period)
			}
		case "MACD":
			if short, shortOk := spec.Parameters["shortPeriod"].(int); shortOk {
				if long, longOk := spec.Parameters["longPeriod"].(int); longOk {
					if signal, signalOk := spec.Parameters["signalPeriod"].(int); signalOk {
						name = fmt.Sprintf("%s(%d,%d,%d)", spec.Type, short, long, signal)
					}
				}
			}
		case "SAR":
			if initial, initialOk := spec.Parameters["accelerationInitial"].(float64); initialOk {
				if max, maxOk := spec.Parameters["accelerationMax"].(float64); maxOk {
					name = fmt.Sprintf("%s(%.2f,%.2f)", spec.Type, initial, max)
				}
			}
		}

		// 이미 캐시에 있는지 확인
		if cache.HasIndicator(name) {
			log.Printf("지표 '%s'가 이미 캐시에 있습니다", name)
			continue
		}

		// 지표 생성 및 캐싱
		ind, err := CreateIndicator(spec)
		if err != nil {
			return fmt.Errorf("지표 생성 실패 '%s': %w", name, err)
		}

		if err := cache.CacheIndicator(name, ind, prices); err != nil {
			return err
		}
	}

	return nil
}

// GetDefaultIndicators는 MACD+SAR+EMA 전략에 필요한 기본 지표 명세를 반환합니다
func GetDefaultIndicators() []IndicatorSpec {
	return []IndicatorSpec{
		{
			Type: "EMA",
			Parameters: map[string]interface{}{
				"period": 200,
			},
		},
		{
			Type: "MACD",
			Parameters: map[string]interface{}{
				"shortPeriod":  12,
				"longPeriod":   26,
				"signalPeriod": 9,
			},
		},
		{
			Type: "SAR",
			Parameters: map[string]interface{}{
				"accelerationInitial": 0.02,
				"accelerationMax":     0.2,
			},
		},
	}
}

```
## internal/backtest/manager.go
```go
package backtest

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
)

// Manager는 백테스트용 포지션 관리자입니다
type Manager struct {
	Account     *Account                      // 계정 정보
	Leverage    int                           // 레버리지
	SlippagePct float64                       // 슬리피지 비율 (%)
	TakerFee    float64                       // Taker 수수료 (%)
	MakerFee    float64                       // Maker 수수료 (%)
	Rules       TradingRules                  // 트레이딩 규칙
	SymbolInfos map[string]*domain.SymbolInfo // 심볼 정보
}

// NewManager는 새로운 백테스트 매니저를 생성합니다
func NewManager(cfg *config.Config, symbolInfos map[string]*domain.SymbolInfo) *Manager {
	return &Manager{
		Account: &Account{
			InitialBalance: cfg.Backtest.InitialBalance,
			Balance:        cfg.Backtest.InitialBalance,
			Positions:      make([]*Position, 0),
			ClosedTrades:   make([]*Position, 0),
			HighWaterMark:  cfg.Backtest.InitialBalance,
		},
		Leverage:    cfg.Backtest.Leverage,
		SlippagePct: cfg.Backtest.SlippagePct,
		TakerFee:    0.0004, // 0.04% 기본값
		MakerFee:    0.0002, // 0.02% 기본값
		Rules: TradingRules{
			MaxPositions:    1,    // 기본값: 심볼당 1개 포지션
			MaxRiskPerTrade: 2,    // 기본값: 거래당 2% 리스크
			SlPriority:      true, // 기본값: SL 우선
		},
		SymbolInfos: symbolInfos,
	}
}

// OpenPosition은 새 포지션을 생성합니다
func (m *Manager) OpenPosition(signal domain.SignalInterface, candle domain.Candle) (*Position, error) {
	symbol := signal.GetSymbol()
	signalType := signal.GetType()

	// 이미 열린 포지션이 있는지 확인
	for _, pos := range m.Account.Positions {
		if pos.Symbol == symbol {
			return nil, fmt.Errorf("이미 %s 심볼에 열린 포지션이 있습니다", symbol)
		}
	}

	// 포지션 사이드 결정
	var side domain.PositionSide
	if signalType == domain.Long || signalType == domain.PendingLong {
		side = domain.LongPosition
	} else {
		side = domain.ShortPosition
	}

	// 진입가 결정 (슬리피지 적용)
	entryPrice := signal.GetPrice()
	if side == domain.LongPosition {
		entryPrice *= (1 + m.SlippagePct/100)
	} else {
		entryPrice *= (1 - m.SlippagePct/100)
	}

	// 포지션 크기 계산
	positionSize := m.calculatePositionSize(m.Account.Balance, m.Rules.MaxRiskPerTrade, entryPrice, signal.GetStopLoss())
	quantity := positionSize / entryPrice

	// 심볼 정보가 있다면 최소 단위에 맞게 조정
	if info, exists := m.SymbolInfos[symbol]; exists {
		quantity = domain.AdjustQuantity(quantity, info.StepSize, info.QuantityPrecision)
	}

	// 포지션 생성에 필요한 자금이 충분한지 확인
	requiredMargin := (positionSize / float64(m.Leverage))
	entryFee := positionSize * m.TakerFee

	if m.Account.Balance < (requiredMargin + entryFee) {
		return nil, fmt.Errorf("잔고 부족: 필요 %.2f, 보유 %.2f", requiredMargin+entryFee, m.Account.Balance)
	}

	// 수수료 차감
	m.Account.Balance -= entryFee

	// 새 포지션 생성
	position := &Position{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: entryPrice,
		Quantity:   math.Abs(quantity),
		EntryTime:  candle.OpenTime,
		StopLoss:   signal.GetStopLoss(),
		TakeProfit: signal.GetTakeProfit(),
		Status:     Open,
		ExitReason: NoExit,
	}

	// 포지션 목록에 추가
	m.Account.Positions = append(m.Account.Positions, position)

	return position, nil
}

// ClosePosition은 특정 포지션을 청산합니다
func (m *Manager) ClosePosition(position *Position, closePrice float64, closeTime time.Time, reason ExitReason) error {
	if position.Status == Closed {
		return fmt.Errorf("이미 청산된 포지션입니다")
	}

	// 청산가가 유효한지 확인 (0이나 음수인 경우 처리)
	if closePrice <= 0 {
		log.Printf("경고: 유효하지 않은 청산가 (%.2f), 현재 가격으로 대체합니다", closePrice)
		return fmt.Errorf("유효하지 않은 청산가: %.2f", closePrice)
	}

	// 청산 시간이 진입 시간보다 이전이면 경고 로그
	if closeTime.Before(position.EntryTime) {
		log.Printf("경고: 청산 시간이 진입 시간보다 이전입니다 (포지션: %s, 진입: %s, 청산: %s)",
			position.Symbol, position.EntryTime.Format("2006-01-02 15:04:05"),
			closeTime.Format("2006-01-02 15:04:05"))
		// 청산 시간을 진입 시간 이후로 조정
		closeTime = position.EntryTime.Add(time.Minute)
	}

	// 특히 Signal Reversal 경우 로그 추가
	if reason == SignalReversal {
		log.Printf("Signal Reversal 청산: %s %s, 진입가: %.2f, 청산가: %.2f (시간: %s)",
			position.Symbol, string(position.Side), position.EntryPrice, closePrice,
			time.Now().Format("2006-01-02 15:04:05"))
	}

	// 청산가 조정 (슬리피지 적용)
	if position.Side == domain.LongPosition {
		closePrice *= (1 - m.SlippagePct/100)
	} else {
		closePrice *= (1 + m.SlippagePct/100)
	}

	// 포지션 가치 계산
	positionValue := position.Quantity * closePrice

	// 수수료 계산
	closeFee := positionValue * m.TakerFee

	log.Printf("DEBUG: 청산 직전 포지션 정보 - 심볼: %s, 사이드: %s, 수량: %.8f, 진입가: %.2f, 청산가: %.2f",
		position.Symbol, position.Side, position.Quantity, position.EntryPrice, closePrice)

	// PnL 계산
	var pnl float64
	if position.Side == domain.LongPosition {
		pnl = (closePrice - position.EntryPrice) * position.Quantity
	} else {
		pnl = (position.EntryPrice - closePrice) * position.Quantity
	}

	log.Printf("DEBUG: PnL 계산 결과 - PnL: %.2f, 사이드: %s, 진입가: %.2f, 청산가: %.2f, 수량: %.8f",
		pnl, position.Side, position.EntryPrice, closePrice, position.Quantity)

	// 수수료 차감
	pnl -= closeFee

	// 레버리지 적용 (선물)
	pnl *= float64(m.Leverage)

	// 포지션 초기 가치 계산 (레버리지 적용 전)
	initialValue := position.EntryPrice * position.Quantity

	log.Printf("DEBUG: 초기 포지션 가치 - 가치: %.2f, 진입가: %.2f, 수량: %.8f",
		initialValue, position.EntryPrice, position.Quantity)

	// PnL 퍼센트 계산 전에 초기 가치가 0인지 체크 (0으로 나누기 방지)
	var pnlPercentage float64
	if initialValue > 0 {
		pnlPercentage = (pnl / initialValue) * 100
	} else {
		pnlPercentage = 0 // 기본값 설정
		log.Printf("경고: 포지션 초기 가치가 0 또는 음수입니다 (심볼: %s)", position.Symbol)
	}

	// 포지션 상태 업데이트
	position.ClosePrice = closePrice
	position.CloseTime = closeTime
	position.Status = Closed
	position.ExitReason = reason
	position.PnL = pnl
	position.PnLPercentage = pnlPercentage

	// 계정 잔고 업데이트
	m.Account.Balance += pnl

	// 계정 기록 업데이트
	m.Account.ClosedTrades = append(m.Account.ClosedTrades, position)

	// 포지션 목록에서 제거
	for i, p := range m.Account.Positions {
		if p == position {
			m.Account.Positions = append(m.Account.Positions[:i], m.Account.Positions[i+1:]...)
			break
		}
	}

	return nil
}

// UpdatePositions은 새 캔들 데이터로 모든 포지션을 업데이트합니다
func (m *Manager) UpdatePositions(currentCandle domain.Candle, signal domain.SignalInterface) []*Position {
	symbol := currentCandle.Symbol
	closedPositions := make([]*Position, 0)

	// 현재 열린 포지션 중 해당 심볼에 대한 포지션 확인
	for _, position := range m.Account.Positions {
		if position.Symbol != symbol {
			continue
		}

		// 1. TP/SL 도달 여부 확인
		tpHit := false
		slHit := false

		if position.Side == domain.LongPosition {
			// 롱 포지션: 고가가 TP 이상이면 TP 도달, 저가가 SL 이하면 SL 도달
			if currentCandle.High >= position.TakeProfit {
				tpHit = true
			}
			if currentCandle.Low <= position.StopLoss {
				slHit = true
			}
		} else {
			// 숏 포지션: 저가가 TP 이하면 TP 도달, 고가가 SL 이상이면 SL 도달
			if currentCandle.Low <= position.TakeProfit {
				tpHit = true
			}
			if currentCandle.High >= position.StopLoss {
				slHit = true
			}
		}

		// 2. 시그널 반전 여부 확인
		signalReversal := false
		var reversalSignalType domain.SignalType = domain.NoSignal
		if signal != nil && signal.GetType() != domain.NoSignal {
			if (position.Side == domain.LongPosition && signal.GetType() == domain.Short) ||
				(position.Side == domain.ShortPosition && signal.GetType() == domain.Long) {
				signalReversal = true
				reversalSignalType = signal.GetType() // 반전 시그널 타입 저장
			}
		}

		// 3. 청산 처리
		// 동일 캔들에서 TP와 SL 모두 도달하면 SL 우선 처리 (Rules.SlPriority 설정에 따라)
		if slHit && (m.Rules.SlPriority || !tpHit) {
			// SL 청산 (저점 또는 고점이 아닌 SL 가격으로 청산)
			m.ClosePosition(position, position.StopLoss, currentCandle.CloseTime, StopLossHit)
			closedPositions = append(closedPositions, position)
		} else if tpHit {
			// TP 청산 (TP 가격으로 청산)
			m.ClosePosition(position, position.TakeProfit, currentCandle.CloseTime, TakeProfitHit)
			closedPositions = append(closedPositions, position)
		} else if signalReversal {
			// 시그널 반전으로 청산 (현재 캔들 종가로 청산)
			if err := m.ClosePosition(position, currentCandle.Close, currentCandle.CloseTime, SignalReversal); err != nil {
				// 오류 처리 (예: 청산가가 유효하지 않은 경우)
				log.Printf("시그널 반전으로 청산 실패 (%s): %v", symbol, err)
				continue
			}
			closedPositions = append(closedPositions, position)

			// 시그널 반전 후 즉시 신규 포지션 진입
			if reversalSignalType != domain.NoSignal && signal != nil {
				// 새로운 포지션 생성
				_, err := m.OpenPosition(signal, currentCandle)
				if err != nil {
					log.Printf("시그널 반전 후 새 포지션 진입 실패 (%s): %v", symbol, err)
				} else {
					log.Printf("시그널 반전 후 즉시 %s %s 포지션 진입 @ %.2f",
						symbol, signal.GetType().String(), signal.GetPrice())
				}
			}
		}
	}

	return closedPositions
}

// UpdateEquity는 계정 자산을 업데이트합니다
func (m *Manager) UpdateEquity(candles map[string]domain.Candle) {
	// 총 자산 계산 (잔고 + 열린 포지션의 현재 가치)
	equity := m.Account.Balance

	// 모든 열린 포지션에 대해 미실현 손익 계산
	for _, position := range m.Account.Positions {
		// 해당 심볼의 최신 캔들 가져오기
		candle, exists := candles[position.Symbol]
		if !exists {
			continue
		}

		// 포지션 현재 가치 계산
		var unrealizedPnl float64
		if position.Side == domain.LongPosition {
			unrealizedPnl = (candle.Close - position.EntryPrice) * position.Quantity
		} else {
			unrealizedPnl = (position.EntryPrice - candle.Close) * position.Quantity
		}

		// 레버리지 적용
		unrealizedPnl *= float64(m.Leverage)

		// 총 자산에 추가
		equity += unrealizedPnl
	}

	// 계정 자산 업데이트
	m.Account.Equity = equity

	// 최고 자산 갱신
	if equity > m.Account.HighWaterMark {
		m.Account.HighWaterMark = equity
	}

	// 현재 낙폭 계산
	if m.Account.HighWaterMark > 0 {
		currentDrawdown := (m.Account.HighWaterMark - equity) / m.Account.HighWaterMark * 100
		m.Account.Drawdown = currentDrawdown

		// 최대 낙폭 갱신
		if currentDrawdown > m.Account.MaxDrawdown {
			m.Account.MaxDrawdown = currentDrawdown
		}
	}
}

// GetBacktestResult는 백테스트 결과를 계산합니다
func (m *Manager) GetBacktestResult(startTime, endTime time.Time, symbol string, interval domain.TimeInterval) *Result {
	totalTrades := len(m.Account.ClosedTrades)
	winningTrades := 0
	losingTrades := 0
	totalProfitPct := 0.0
	validTradeCount := 0

	// 포지션 분석
	for _, trade := range m.Account.ClosedTrades {
		// NaN 값 검사 추가
		if !math.IsNaN(trade.PnLPercentage) {
			if trade.PnL > 0 {
				winningTrades++
			} else {
				losingTrades++
			}
			totalProfitPct += trade.PnLPercentage
			validTradeCount++
		} else {
			// NaN인 경우 로그에 기록 (디버깅용)
			log.Printf("경고: NaN 수익률이 발견됨 (심볼: %s, 포지션: %s, 청산이유: %d)",
				trade.Symbol, string(trade.Side), trade.ExitReason)
		}
	}

	// 승률 계산
	var winRate float64
	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades) * 100
	}

	// 평균 수익률 계산
	var avgReturn float64
	if totalTrades > 0 {
		avgReturn = totalProfitPct / float64(totalTrades)
	}

	// 누적 수익률 계산
	cumulativeReturn := 0.0
	if m.Account.InitialBalance > 0 {
		cumulativeReturn = (m.Account.Balance - m.Account.InitialBalance) / m.Account.InitialBalance * 100
	}

	// 결과 생성
	return &Result{
		TotalTrades:      validTradeCount,
		WinningTrades:    winningTrades,
		LosingTrades:     losingTrades,
		WinRate:          winRate,
		CumulativeReturn: cumulativeReturn,
		AverageReturn:    avgReturn,
		MaxDrawdown:      m.Account.MaxDrawdown,
		Trades:           m.convertTradesToResultTrades(),
		StartTime:        startTime,
		EndTime:          endTime,
		Symbol:           symbol,
		Interval:         interval,
	}
}

// calculatePositionSize는 리스크 기반으로 포지션 크기를 계산합니다
func (m *Manager) calculatePositionSize(balance float64, riskPercent float64, entryPrice, stopLoss float64) float64 {
	// 해당 거래에 할당할 자금
	riskAmount := balance * (riskPercent / 100)

	// 손절가와 진입가의 차이 (%)
	var priceDiffPct float64
	if entryPrice > stopLoss { // 롱 포지션
		priceDiffPct = (entryPrice - stopLoss) / entryPrice * 100
	} else { // 숏 포지션
		priceDiffPct = (stopLoss - entryPrice) / entryPrice * 100
	}

	// 레버리지 고려
	priceDiffPct = priceDiffPct * float64(m.Leverage)

	// 리스크 기반 포지션 크기
	if priceDiffPct > 0 {
		return (riskAmount / priceDiffPct) * 100
	}

	// 기본값: 잔고의 1%
	return balance * 0.01
}

// convertTradesToResultTrades는 내부 포지션 기록을 결과용 Trade 구조체로 변환합니다
func (m *Manager) convertTradesToResultTrades() []Trade {
	trades := make([]Trade, len(m.Account.ClosedTrades))

	for i, position := range m.Account.ClosedTrades {
		exitReason := ""
		switch position.ExitReason {
		case StopLossHit:
			exitReason = "SL"
		case TakeProfitHit:
			exitReason = "TP"
		case SignalReversal:
			exitReason = "Signal Reversal"
		case EndOfBacktest:
			exitReason = "End of Backtest"
		}

		trades[i] = Trade{
			EntryTime:  position.EntryTime,
			ExitTime:   position.CloseTime,
			EntryPrice: position.EntryPrice,
			ExitPrice:  position.ClosePrice,
			Side:       position.Side,
			ProfitPct:  position.PnLPercentage,
			ExitReason: exitReason,
		}
	}

	return trades
}

```
## internal/backtest/types.go
```go
package backtest

import (
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// Result는 백테스트 결과를 저장하는 구조체입니다
type Result struct {
	TotalTrades      int                 // 총 거래 횟수
	WinningTrades    int                 // 승리 거래 횟수
	LosingTrades     int                 // 패배 거래 횟수
	WinRate          float64             // 승률 (%)
	CumulativeReturn float64             // 누적 수익률 (%)
	AverageReturn    float64             // 평균 수익률 (%)
	MaxDrawdown      float64             // 최대 낙폭 (%)
	Trades           []Trade             // 개별 거래 기록
	StartTime        time.Time           // 백테스트 시작 시간
	EndTime          time.Time           // 백테스트 종료 시간
	Symbol           string              // 테스트한 심볼
	Interval         domain.TimeInterval // 테스트 간격
}

// Trade는 개별 거래 정보를 저장합니다
type Trade struct {
	EntryTime  time.Time           // 진입 시간
	ExitTime   time.Time           // 종료 시간
	EntryPrice float64             // 진입 가격
	ExitPrice  float64             // 종료 가격
	Side       domain.PositionSide // 포지션 방향
	ProfitPct  float64             // 수익률 (%)
	ExitReason string              // 종료 이유 (TP, SL, 신호 반전 등)
}

// PositionStatus는 포지션 상태를 정의합니다
type PositionStatus int

const (
	Open   PositionStatus = iota // 열린 포지션
	Closed                       // 청산된 포지션
)

// ExitReason은 포지션 청산 이유를 정의합니다
type ExitReason int

const (
	NoExit         ExitReason = iota // 청산되지 않음
	StopLossHit                      // 손절
	TakeProfitHit                    // 익절
	SignalReversal                   // 반대 신호 발생
	EndOfBacktest                    // 백테스트 종료
)

// Position은 백테스트 중 포지션 정보를 나타냅니다
type Position struct {
	Symbol        string              // 심볼 (예: BTCUSDT)
	Side          domain.PositionSide // 롱/숏 포지션
	EntryPrice    float64             // 진입가
	Quantity      float64             // 수량
	EntryTime     time.Time           // 진입 시간
	StopLoss      float64             // 손절가
	TakeProfit    float64             // 익절가
	ClosePrice    float64             // 청산가 (청산 시에만 설정)
	CloseTime     time.Time           // 청산 시간 (청산 시에만 설정)
	PnL           float64             // 손익 (청산 시에만 설정)
	PnLPercentage float64             // 손익률 % (청산 시에만 설정)
	Status        PositionStatus      // 포지션 상태
	ExitReason    ExitReason          // 청산 이유
}

// Account는 백테스트 계정 상태를 나타냅니다
type Account struct {
	InitialBalance float64     // 초기 잔고
	Balance        float64     // 현재 잔고
	Positions      []*Position // 열린 포지션
	ClosedTrades   []*Position // 청산된 포지션 기록
	Equity         float64     // 총 자산 (잔고 + 미실현 손익)
	HighWaterMark  float64     // 최고 자산 기록 (MDD 계산용)
	Drawdown       float64     // 현재 낙폭
	MaxDrawdown    float64     // 최대 낙폭
}

// TradingRules는 백테스트 트레이딩 규칙을 정의합니다
type TradingRules struct {
	MaxPositions    int     // 동시 오픈 가능한 최대 포지션 수
	MaxRiskPerTrade float64 // 거래당 최대 리스크 (%)
	SlPriority      bool    // 동일 시점에 TP/SL 모두 조건 충족시 SL 우선 적용 여부
}

```
## internal/config/config.go
```go
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// 바이낸스 API 설정
	Binance struct {
		// 메인넷 API 키
		APIKey    string `envconfig:"BINANCE_API_KEY" required:"true"`
		SecretKey string `envconfig:"BINANCE_SECRET_KEY" required:"true"`

		// 테스트넷 API 키
		TestAPIKey    string `envconfig:"BINANCE_TEST_API_KEY" required:"false"`
		TestSecretKey string `envconfig:"BINANCE_TEST_SECRET_KEY" required:"false"`

		// 테스트넷 사용 여부
		UseTestnet bool `envconfig:"USE_TESTNET" default:"false"`
	}

	// 디스코드 웹훅 설정
	Discord struct {
		SignalWebhook string `envconfig:"DISCORD_SIGNAL_WEBHOOK" required:"true"`
		TradeWebhook  string `envconfig:"DISCORD_TRADE_WEBHOOK" required:"true"`
		ErrorWebhook  string `envconfig:"DISCORD_ERROR_WEBHOOK" required:"true"`
		InfoWebhook   string `envconfig:"DISCORD_INFO_WEBHOOK" required:"true"`
	}

	// 애플리케이션 설정
	App struct {
		FetchInterval   time.Duration `envconfig:"FETCH_INTERVAL" default:"15m"`
		CandleLimit     int           `envconfig:"CANDLE_LIMIT" default:"100"`
		Symbols         []string      `envconfig:"SYMBOLS" default:""`              // 커스텀 심볼 목록
		UseTopSymbols   bool          `envconfig:"USE_TOP_SYMBOLS" default:"false"` // 거래량 상위 심볼 사용 여부
		TopSymbolsCount int           `envconfig:"TOP_SYMBOLS_COUNT" default:"3"`   // 거래량 상위 심볼 개수
	}

	// 거래 설정
	Trading struct {
		Leverage int `envconfig:"LEVERAGE" default:"5" validate:"min=1,max=100"`
	}

	// 백테스트 설정 추가
	Backtest struct {
		Strategy       string  `envconfig:"BACKTEST_STRATEGY" default:"MACD+SAR+EMA"`
		Symbol         string  `envconfig:"BACKTEST_SYMBOL" default:"BTCUSDT"`
		Days           int     `envconfig:"BACKTEST_DAYS" default:"30"`
		Interval       string  `envconfig:"BACKTEST_INTERVAL" default:"15m"`
		InitialBalance float64 `envconfig:"BACKTEST_INITIAL_BALANCE" default:"1000.0"` // 초기 잔고
		Leverage       int     `envconfig:"BACKTEST_LEVERAGE" default:"5"`             // 레버리지
		SlippagePct    float64 `envconfig:"BACKTEST_SLIPPAGE_PCT" default:"0.0"`       // 슬리피지 비율
		SaveResults    bool    `envconfig:"BACKTEST_SAVE_RESULTS" default:"false"`     // 결과 저장 여부
		ResultsPath    string  `envconfig:"BACKTEST_RESULTS_PATH" default:"./results"` // 결과 저장 경로
	}
}

// ValidateConfig는 설정이 유효한지 확인합니다.
func ValidateConfig(cfg *Config) error {
	if cfg.Binance.UseTestnet {
		// 테스트넷 모드일 때 테스트넷 API 키 검증
		if cfg.Binance.TestAPIKey == "" || cfg.Binance.TestSecretKey == "" {
			return fmt.Errorf("테스트넷 모드에서는 BINANCE_TEST_API_KEY와 BINANCE_TEST_SECRET_KEY가 필요합니다")
		}
	} else {
		// 메인넷 모드일 때 메인넷 API 키 검증
		if cfg.Binance.APIKey == "" || cfg.Binance.SecretKey == "" {
			return fmt.Errorf("메인넷 모드에서는 BINANCE_API_KEY와 BINANCE_SECRET_KEY가 필요합니다")
		}
	}

	if cfg.Trading.Leverage < 1 || cfg.Trading.Leverage > 100 {
		return fmt.Errorf("레버리지는 1 이상 100 이하이어야 합니다")
	}

	if cfg.App.FetchInterval < 1*time.Minute {
		return fmt.Errorf("FETCH_INTERVAL은 1분 이상이어야 합니다")
	}

	if cfg.App.CandleLimit < 300 {
		return fmt.Errorf("CANDLE_LIMIT은 300 이상이어야 합니다")
	}

	return nil
}

// LoadConfig는 환경변수에서 설정을 로드합니다.
func LoadConfig() (*Config, error) {
	// .env 파일 로드
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf(".env 파일 로드 실패: %w", err)
	}

	var cfg Config
	// 환경변수를 구조체로 파싱
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("환경변수 처리 실패: %w", err)
	}

	// 심볼 문자열 파싱
	if symbolsStr := os.Getenv("SYMBOLS"); symbolsStr != "" {
		cfg.App.Symbols = strings.Split(symbolsStr, ",")
		for i, s := range cfg.App.Symbols {
			cfg.App.Symbols[i] = strings.TrimSpace(s)
		}
	}

	// 설정값 검증
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("설정값 검증 실패: %w", err)
	}

	return &cfg, nil
}

```
## internal/domain/account.go
```go
package domain

// Balance는 계정 잔고 정보를 표현합니다
type Balance struct {
	Asset              string  // 자산 심볼 (예: USDT, BTC)
	Available          float64 // 사용 가능한 잔고
	Locked             float64 // 주문 등에 잠긴 잔고
	CrossWalletBalance float64 // 교차 마진 지갑 잔고
}

// AccountInfo는 계정 정보를 표현합니다
type AccountInfo struct {
	Balances              map[string]Balance // 자산별 잔고
	CanTrade              bool               // 거래 가능 여부
	CanDeposit            bool               // 입금 가능 여부
	CanWithdraw           bool               // 출금 가능 여부
	TotalMarginBalance    float64            // 총 마진 잔고
	TotalUnrealizedProfit float64            // 총 미실현 손익
}

// TradeInfo는 거래 실행 정보를 정의합니다
type TradeInfo struct {
	Symbol        string       // 심볼 (예: BTCUSDT)
	PositionSide  PositionSide // 포지션 방향 (LONG/SHORT)
	PositionValue float64      // 포지션 크기 (USDT)
	Quantity      float64      // 구매/판매 수량 (코인)
	EntryPrice    float64      // 진입가
	StopLoss      float64      // 손절가
	TakeProfit    float64      // 익절가
	Balance       float64      // 현재 USDT 잔고
	Leverage      int          // 사용 레버리지
}

```
## internal/domain/candle.go
```go
package domain

import "time"

// Candle은 캔들 데이터를 표현합니다
type Candle struct {
	OpenTime  time.Time    // 캔들 시작 시간
	CloseTime time.Time    // 캔들 종료 시간
	Open      float64      // 시가
	High      float64      // 고가
	Low       float64      // 저가
	Close     float64      // 종가
	Volume    float64      // 거래량
	Symbol    string       // 심볼 (예: BTCUSDT)
	Interval  TimeInterval // 시간 간격 (예: 15m, 1h)
}

// CandleList는 캔들 데이터 목록입니다
type CandleList []Candle

// GetLastCandle은 가장 최근 캔들을 반환합니다
func (cl CandleList) GetLastCandle() (Candle, bool) {
	if len(cl) == 0 {
		return Candle{}, false
	}
	return cl[len(cl)-1], true
}

// GetPriceAtIndex는 특정 인덱스의 가격을 반환합니다
func (cl CandleList) GetPriceAtIndex(index int) (float64, bool) {
	if index < 0 || index >= len(cl) {
		return 0, false
	}
	return cl[index].Close, true
}

// GetSubList는 지정된 범위의 부분 리스트를 반환합니다
func (cl CandleList) GetSubList(start, end int) (CandleList, bool) {
	if start < 0 || end > len(cl) || start >= end {
		return nil, false
	}
	return cl[start:end], true
}

```
## internal/domain/order.go
```go
package domain

import "time"

// OrderRequest는 주문 요청 정보를 표현합니다
type OrderRequest struct {
	Symbol        string       // 심볼 (예: BTCUSDT)
	Side          OrderSide    // 매수/매도
	PositionSide  PositionSide // 롱/숏 포지션
	Type          OrderType    // 주문 유형 (시장가, 지정가 등)
	Quantity      float64      // 수량
	QuoteQuantity float64      // 명목 가치 (USDT 기준)
	Price         float64      // 지정가 (Limit 주문 시)
	StopPrice     float64      // 스탑 가격 (Stop 주문 시)
	TimeInForce   string       // 주문 유효 기간 (GTC, IOC 등)
	ClientOrderID string       // 클라이언트 측 주문 ID
}

// OrderResponse는 주문 응답을 표현합니다
type OrderResponse struct {
	OrderID          int64        // 주문 ID
	Symbol           string       // 심볼
	Status           string       // 주문 상태
	ClientOrderID    string       // 클라이언트 측 주문 ID
	Price            float64      // 주문 가격
	AvgPrice         float64      // 평균 체결 가격
	OrigQuantity     float64      // 원래 주문 수량
	ExecutedQuantity float64      // 체결된 수량
	Side             OrderSide    // 매수/매도
	PositionSide     PositionSide // 롱/숏 포지션
	Type             OrderType    // 주문 유형
	CreateTime       time.Time    // 주문 생성 시간
}

// Position은 포지션 정보를 표현합니다
type Position struct {
	Symbol        string       // 심볼 (예: BTCUSDT)
	PositionSide  PositionSide // 롱/숏 포지션
	Quantity      float64      // 포지션 수량 (양수: 롱, 음수: 숏)
	EntryPrice    float64      // 평균 진입가
	Leverage      int          // 레버리지
	MarkPrice     float64      // 마크 가격
	UnrealizedPnL float64      // 미실현 손익
	InitialMargin float64      // 초기 마진
	MaintMargin   float64      // 유지 마진
}

// SymbolInfo는 심볼의 거래 정보를 나타냅니다
type SymbolInfo struct {
	Symbol            string  // 심볼 이름 (예: BTCUSDT)
	StepSize          float64 // 수량 최소 단위 (예: 0.001 BTC)
	TickSize          float64 // 가격 최소 단위 (예: 0.01 USDT)
	MinNotional       float64 // 최소 주문 가치 (예: 10 USDT)
	PricePrecision    int     // 가격 소수점 자릿수
	QuantityPrecision int     // 수량 소수점 자릿수
}

// LeverageBracket은 레버리지 구간 정보를 나타냅니다
type LeverageBracket struct {
	Bracket          int     // 구간 번호
	InitialLeverage  int     // 최대 레버리지
	MaintMarginRatio float64 // 유지증거금 비율
	Notional         float64 // 명목가치 상한
}

```
## internal/domain/signal.go
```go
package domain

import "time"

// SignalInterface는 모든 시그널 타입이 구현해야 하는 인터페이스입니다
type SignalInterface interface {
	// 기본 정보 조회 메서드
	GetType() SignalType
	GetSymbol() string
	GetPrice() float64
	GetTimestamp() time.Time
	GetStopLoss() float64
	GetTakeProfit() float64

	// 유효성 검사
	IsValid() bool

	// 알림 데이터 변환 - 각 전략별 구현체에서 구체적으로 구현
	ToNotificationData() map[string]interface{}

	GetCondition(key string) (interface{}, bool)
	SetCondition(key string, value interface{})
	GetAllConditions() map[string]interface{}
}

// BaseSignal은 모든 시그널 구현체가 공유하는 기본 필드와 메서드를 제공합니다
type BaseSignal struct {
	Type       SignalType
	Symbol     string
	Price      float64
	Timestamp  time.Time
	StopLoss   float64
	TakeProfit float64
	Conditions map[string]interface{}
}

/// 생성자
func NewBaseSignal(signalType SignalType, symbol string, price float64, timestamp time.Time, stopLoss, takeProfit float64) BaseSignal {
	return BaseSignal{
		Type:       signalType,
		Symbol:     symbol,
		Price:      price,
		Timestamp:  timestamp,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Conditions: make(map[string]interface{}),
	}
}

// GetType은 시그널 타입을 반환합니다
func (s *BaseSignal) GetType() SignalType {
	return s.Type
}

// GetSymbol은 시그널의 심볼을 반환합니다
func (s *BaseSignal) GetSymbol() string {
	return s.Symbol
}

// GetPrice는 시그널의 가격을 반환합니다
func (s *BaseSignal) GetPrice() float64 {
	return s.Price
}

// GetTimestamp는 시그널의 생성 시간을 반환합니다
func (s *BaseSignal) GetTimestamp() time.Time {
	return s.Timestamp
}

// GetStopLoss는 시그널의 손절가를 반환합니다
func (s *BaseSignal) GetStopLoss() float64 {
	return s.StopLoss
}

// GetTakeProfit는 시그널의 익절가를 반환합니다
func (s *BaseSignal) GetTakeProfit() float64 {
	return s.TakeProfit
}

// IsValid는 시그널이 유효한지 확인합니다
func (s *BaseSignal) IsValid() bool {
	return s.Type != NoSignal && s.Symbol != "" && s.Price > 0
}

// ToNotificationData는 알림 시스템에서 사용할 기본 데이터를 반환합니다
// 구체적인 시그널 구현체에서 오버라이딩해야 합니다
func (s *BaseSignal) ToNotificationData() map[string]interface{} {
	data := map[string]interface{}{
		"Type":       s.Type.String(),
		"Symbol":     s.Symbol,
		"Price":      s.Price,
		"Timestamp":  s.Timestamp.Format("2006-01-02 15:04:05"),
		"StopLoss":   s.StopLoss,
		"TakeProfit": s.TakeProfit,
	}

	// 조건 정보 추가
	if s.Conditions != nil {
		for k, v := range s.Conditions {
			data[k] = v
		}
	}

	return data
}

// GetCondition는 특정 키의 조건 값을 반환합니다
func (s *BaseSignal) GetCondition(key string) (interface{}, bool) {
	if s.Conditions == nil {
		return nil, false
	}
	value, exists := s.Conditions[key]
	return value, exists
}

// SetCondition는 특정 키에 조건 값을 설정합니다
func (s *BaseSignal) SetCondition(key string, value interface{}) {
	if s.Conditions == nil {
		s.Conditions = make(map[string]interface{})
	}
	s.Conditions[key] = value
}

// GetAllConditions는 모든 조건을 맵으로 반환합니다
func (s *BaseSignal) GetAllConditions() map[string]interface{} {
	// 원본 맵의 복사본 반환
	if s.Conditions == nil {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	for k, v := range s.Conditions {
		result[k] = v
	}
	return result
}

// SignalConditions는 시그널 발생 조건들의 상세 정보를 저장합니다
type SignalConditions struct {
	EMALong     bool    // 가격이 EMA 위
	EMAShort    bool    // 가격이 EMA 아래
	MACDLong    bool    // MACD 상향돌파
	MACDShort   bool    // MACD 하향돌파
	SARLong     bool    // SAR이 가격 아래
	SARShort    bool    // SAR이 가격 위
	EMAValue    float64 // EMA 값
	MACDValue   float64 // MACD 값
	SignalValue float64 // MACD Signal 값
	SARValue    float64 // SAR 값
}

// Signal은 생성된 시그널 정보를 담습니다
type Signal struct {
	Type       SignalType       // 시그널 유형 (Long, Short 등)
	Symbol     string           // 심볼 (예: BTCUSDT)
	Price      float64          // 현재 가격
	Timestamp  time.Time        // 시그널 생성 시간
	Conditions SignalConditions // 시그널 발생 조건 상세
	StopLoss   float64          // 손절가
	TakeProfit float64          // 익절가
}

// IsValid는 시그널이 유효한지 확인합니다
func (s *Signal) IsValid() bool {
	return s.Type != NoSignal && s.Symbol != "" && s.Price > 0
}

// IsLong은 시그널이 롱 포지션인지 확인합니다
func (s *Signal) IsLong() bool {
	return s.Type == Long
}

// IsShort은 시그널이 숏 포지션인지 확인합니다
func (s *Signal) IsShort() bool {
	return s.Type == Short
}

// IsPending은 시그널이 대기 상태인지 확인합니다
func (s *Signal) IsPending() bool {
	return s.Type == PendingLong || s.Type == PendingShort
}

```
## internal/domain/timeframe.go
```go
package domain

import (
	"fmt"
	"time"
)

// ConvertHourlyToCurrentDaily는 1시간봉 데이터를 사용하여 특정 시점까지의
// "현재 진행 중인" 일봉을 생성합니다.
// hourlyCandles: 1시간봉 캔들 리스트
// asOf: 기준 시점 (이 시점까지의 데이터로 일봉 구성)
// Returns: 구성된 일봉 캔들 또는 데이터가 없는 경우 nil
func ConvertHourlyToCurrentDaily(hourlyCandles CandleList, asOf time.Time) (*Candle, error) {
	if len(hourlyCandles) == 0 {
		return nil, fmt.Errorf("캔들 데이터가 비어있습니다")
	}

	// 시간 정렬 확인 (첫 캔들이 가장 오래된 데이터)
	for i := 1; i < len(hourlyCandles); i++ {
		if hourlyCandles[i].OpenTime.Before(hourlyCandles[i-1].OpenTime) {
			return nil, fmt.Errorf("캔들 데이터가 시간순으로 정렬되어 있지 않습니다")
		}
	}

	// asOf와 같은 날짜(UTC 기준)의 캔들만 필터링
	// UTC 기준 자정(00:00:00)
	startOfDay := time.Date(
		asOf.UTC().Year(),
		asOf.UTC().Month(),
		asOf.UTC().Day(),
		0, 0, 0, 0,
		time.UTC,
	)

	// 현재 날짜의 캔들 필터링
	var todayCandles CandleList
	for _, candle := range hourlyCandles {
		// 오늘 자정 이후 & asOf 이전 캔들만 포함
		if !candle.OpenTime.Before(startOfDay) && !candle.OpenTime.After(asOf) {
			todayCandles = append(todayCandles, candle)
		}
	}

	// 해당 날짜의 캔들이 없는 경우
	if len(todayCandles) == 0 {
		return nil, fmt.Errorf("지정된 날짜(%s)에 해당하는 캔들 데이터가 없습니다", asOf.Format("2006-01-02"))
	}

	// 일봉 구성
	// - 시가: 첫 번째 캔들의 시가
	// - 고가: 모든 캔들 중 최고가
	// - 저가: 모든 캔들 중 최저가
	// - 종가: 마지막 캔들의 종가
	// - 거래량: 모든 캔들의 거래량 합계
	dailyCandle := Candle{
		Symbol:    todayCandles[0].Symbol,
		Interval:  Interval1d,
		OpenTime:  startOfDay,
		CloseTime: time.Date(startOfDay.Year(), startOfDay.Month(), startOfDay.Day(), 23, 59, 59, 999999999, time.UTC),
		Open:      todayCandles[0].Open,
		High:      todayCandles[0].High,
		Low:       todayCandles[0].Low,
		Close:     todayCandles[len(todayCandles)-1].Close,
		Volume:    0,
	}

	// 최고가, 최저가, 거래량 계산
	for _, candle := range todayCandles {
		if candle.High > dailyCandle.High {
			dailyCandle.High = candle.High
		}
		if candle.Low < dailyCandle.Low {
			dailyCandle.Low = candle.Low
		}
		dailyCandle.Volume += candle.Volume
	}

	return &dailyCandle, nil
}

```
## internal/domain/types.go
```go
package domain

// SignalType은 트레이딩 시그널 유형을 정의합니다
type SignalType int

const (
	NoSignal SignalType = iota
	Long
	Short
	PendingLong  // MACD 상향 돌파 후 SAR 반전 대기 상태
	PendingShort // MACD 하향돌파 후 SAR 반전 대기 상태
)

// String은 SignalType의 문자열 표현을 반환합니다
func (s SignalType) String() string {
	switch s {
	case NoSignal:
		return "NoSignal"
	case Long:
		return "Long"
	case Short:
		return "Short"
	case PendingLong:
		return "PendingLong"
	case PendingShort:
		return "PendingShort"
	default:
		return "Unknown"
	}
}

// OrderSide는 주문 방향을 정의합니다
type OrderSide string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"
)

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

const (
	LongPosition  PositionSide = "LONG"
	ShortPosition PositionSide = "SHORT"
	BothPosition  PositionSide = "BOTH" // 헤지 모드가 아닌 경우
)

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Market           OrderType = "MARKET"
	Limit            OrderType = "LIMIT"
	StopMarket       OrderType = "STOP_MARKET"
	TakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"
)

// TimeInterval은 캔들 차트의 시간 간격을 정의합니다
type TimeInterval string

const (
	Interval1m  TimeInterval = "1m"
	Interval3m  TimeInterval = "3m"
	Interval5m  TimeInterval = "5m"
	Interval15m TimeInterval = "15m"
	Interval30m TimeInterval = "30m"
	Interval1h  TimeInterval = "1h"
	Interval2h  TimeInterval = "2h"
	Interval4h  TimeInterval = "4h"
	Interval6h  TimeInterval = "6h"
	Interval8h  TimeInterval = "8h"
	Interval12h TimeInterval = "12h"
	Interval1d  TimeInterval = "1d"
)

// NotificationColor는 알림 색상 코드를 정의합니다
const (
	ColorSuccess = 0x00FF00 // 녹색
	ColorError   = 0xFF0000 // 빨간색
	ColorInfo    = 0x0000FF // 파란색
	ColorWarning = 0xFFA500 // 주황색
)

// ErrorCode는 API 에러 코드를 정의합니다
const (
	ErrPositionModeNoChange = -4059 // 포지션 모드 변경 불필요 에러
)

```
## internal/domain/utils.go
```go
package domain

import (
	"math"
	"time"
)

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

// TimeIntervalToDuration은 TimeInterval을 time.Duration으로 변환합니다
func TimeIntervalToDuration(interval TimeInterval) time.Duration {
	switch interval {
	case Interval1m:
		return 1 * time.Minute
	case Interval3m:
		return 3 * time.Minute
	case Interval5m:
		return 5 * time.Minute
	case Interval15m:
		return 15 * time.Minute
	case Interval30m:
		return 30 * time.Minute
	case Interval1h:
		return 1 * time.Hour
	case Interval2h:
		return 2 * time.Hour
	case Interval4h:
		return 4 * time.Hour
	case Interval6h:
		return 6 * time.Hour
	case Interval8h:
		return 8 * time.Hour
	case Interval12h:
		return 12 * time.Hour
	case Interval1d:
		return 24 * time.Hour
	default:
		return 15 * time.Minute // 기본값
	}
}

```
## internal/exchange/binance/client.go
```go
// internal/exchange/binance/client.go
package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// Client는 바이낸스 API 클라이언트를 구현합니다
type Client struct {
	apiKey           string
	secretKey        string
	baseURL          string
	httpClient       *http.Client
	serverTimeOffset int64 // 서버 시간과의 차이를 저장
	mu               sync.RWMutex
}

// ClientOption은 클라이언트 생성 옵션을 정의합니다
type ClientOption func(*Client)

// WithTimeout은 HTTP 클라이언트의 타임아웃을 설정합니다
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithBaseURL은 기본 URL을 설정합니다
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithTestnet은 테스트넷 사용 여부를 설정합니다
func WithTestnet(useTestnet bool) ClientOption {
	return func(c *Client) {
		if useTestnet {
			c.baseURL = "https://testnet.binancefuture.com"
		} else {
			c.baseURL = "https://fapi.binance.com"
		}
	}
}

// NewClient는 새로운 바이낸스 API 클라이언트를 생성합니다
func NewClient(apiKey, secretKey string, opts ...ClientOption) *Client {
	c := &Client{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    "https://fapi.binance.com", // 기본값은 선물 거래소
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	// 옵션 적용
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// GetServerTime은 서버 시간을 조회합니다
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
	// 기존 도메인 모델로 변환하는 부분만 변경
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/time", nil, false)
	if err != nil {
		return time.Time{}, err
	}

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return time.Time{}, fmt.Errorf("서버 시간 파싱 실패: %w", err)
	}

	return time.Unix(0, result.ServerTime*int64(time.Millisecond)), nil
}

// doRequest는 HTTP 요청을 실행하고 결과를 반환합니다
func (c *Client) doRequest(ctx context.Context, method, endpoint string, params url.Values, needSign bool) ([]byte, error) {
	// 기존 코드와 동일
	if params == nil {
		params = url.Values{}
	}

	// URL 생성
	reqURL, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, fmt.Errorf("URL 파싱 실패: %w", err)
	}

	// 타임스탬프 추가
	if needSign {
		timestamp := strconv.FormatInt(c.getServerTime(), 10)
		params.Set("timestamp", timestamp)
		params.Set("recvWindow", "5000")
	}

	// 파라미터 설정
	reqURL.RawQuery = params.Encode()

	// 서명 추가
	if needSign {
		signature := c.sign(params.Encode())
		reqURL.RawQuery = reqURL.RawQuery + "&signature=" + signature
	}

	// 요청 생성
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("요청 생성 실패: %w", err)
	}

	// 헤더 설정
	req.Header.Set("Content-Type", "application/json")
	if needSign {
		req.Header.Set("X-MBX-APIKEY", c.apiKey)
	}

	// 요청 실행
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	// 응답 읽기
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %w", err)
	}

	// 상태 코드 확인
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code    int    `json:"code"`
			Message string `json:"msg"`
		}
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return nil, fmt.Errorf("HTTP 에러(%d): %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("API 에러(코드: %d): %s", apiErr.Code, apiErr.Message)
	}

	return body, nil
}

// sign은 요청에 대한 서명을 생성합니다
func (c *Client) sign(payload string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// getServerTime은 현재 서버 시간을 반환합니다
func (c *Client) getServerTime() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UnixMilli() + c.serverTimeOffset
}

// GetKlines는 캔들 데이터를 조회합니다 (대용량 데이터 처리 지원)
func (c *Client) GetKlines(ctx context.Context, symbol string, interval domain.TimeInterval, limit int) (domain.CandleList, error) {
	const maxLimit = 1000 // 바이낸스 API 최대 제한

	// 요청 캔들 수가 최대 제한보다 적으면 단일 요청
	if limit <= maxLimit {
		return c.getKlinesSimple(ctx, symbol, interval, limit)
	}

	// 대용량 데이터는 여러 번 나눠서 요청
	var allCandles domain.CandleList
	remainingCandles := limit
	var endTime int64 = 0 // 첫 요청은 현재 시간부터

	for remainingCandles > 0 {
		fetchLimit := min(remainingCandles, maxLimit)
		params := url.Values{}
		params.Add("symbol", symbol)
		params.Add("interval", string(interval))
		params.Add("limit", strconv.Itoa(fetchLimit))

		// endTime 파라미터 추가 (두 번째 요청부터)
		if endTime > 0 {
			params.Add("endTime", strconv.FormatInt(endTime, 10))
		}

		resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/klines", params, false)
		if err != nil {
			return nil, err
		}

		var rawCandles [][]interface{}
		if err := json.Unmarshal(resp, &rawCandles); err != nil {
			return nil, fmt.Errorf("캔들 데이터 파싱 실패: %w", err)
		}

		if len(rawCandles) == 0 {
			break // 더 이상 데이터가 없음
		}

		// 캔들 변환 및 추가
		candles := make(domain.CandleList, len(rawCandles))
		for i, raw := range rawCandles {
			openTime := int64(raw[0].(float64))
			closeTime := int64(raw[6].(float64))

			// 다음 요청의 endTime 설정 (가장 오래된 캔들의 시작 시간 - 1ms)
			if i == len(rawCandles)-1 {
				endTime = openTime - 1
			}

			// 가격 문자열 변환
			open, _ := strconv.ParseFloat(raw[1].(string), 64)
			high, _ := strconv.ParseFloat(raw[2].(string), 64)
			low, _ := strconv.ParseFloat(raw[3].(string), 64)
			close, _ := strconv.ParseFloat(raw[4].(string), 64)
			volume, _ := strconv.ParseFloat(raw[5].(string), 64)

			candles[i] = domain.Candle{
				OpenTime:  time.Unix(openTime/1000, 0),
				CloseTime: time.Unix(closeTime/1000, 0),
				Open:      open,
				High:      high,
				Low:       low,
				Close:     close,
				Volume:    volume,
				Symbol:    symbol,
				Interval:  interval,
			}
		}

		// 결과 병합
		allCandles = append(candles, allCandles...)
		remainingCandles -= len(rawCandles)

		// 받은 캔들 수가 요청한 것보다 적으면 더 이상 데이터가 없는 것
		if len(rawCandles) < fetchLimit {
			break
		}

		// API 속도 제한 방지를 위한 짧은 지연
		time.Sleep(100 * time.Millisecond)
	}

	// 요청한 개수만큼 반환 (초과분 제거)
	if len(allCandles) > limit {
		return allCandles[:limit], nil
	}

	return allCandles, nil
}

// getKlinesSimple은 단일 요청으로 캔들 데이터를 조회합니다 (기존 로직)
func (c *Client) getKlinesSimple(ctx context.Context, symbol string, interval domain.TimeInterval, limit int) (domain.CandleList, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("interval", string(interval))
	params.Add("limit", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var rawCandles [][]interface{}
	if err := json.Unmarshal(resp, &rawCandles); err != nil {
		return nil, fmt.Errorf("캔들 데이터 파싱 실패: %w", err)
	}

	// 기존: market.CandleData 배열 -> 새로운: domain.CandleList
	candles := make(domain.CandleList, len(rawCandles))
	for i, raw := range rawCandles {
		// 시간 변환
		openTime := int64(raw[0].(float64))
		closeTime := int64(raw[6].(float64))

		// 가격 문자열 변환
		open, _ := strconv.ParseFloat(raw[1].(string), 64)
		high, _ := strconv.ParseFloat(raw[2].(string), 64)
		low, _ := strconv.ParseFloat(raw[3].(string), 64)
		close, _ := strconv.ParseFloat(raw[4].(string), 64)
		volume, _ := strconv.ParseFloat(raw[5].(string), 64)

		candles[i] = domain.Candle{
			OpenTime:  time.Unix(openTime/1000, 0),
			CloseTime: time.Unix(closeTime/1000, 0),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Symbol:    symbol,
			Interval:  interval,
		}
	}

	return candles, nil
}

// GetSymbolInfo는 특정 심볼의 거래 정보만 조회합니다
func (c *Client) GetSymbolInfo(ctx context.Context, symbol string) (*domain.SymbolInfo, error) {
	// 요청 파라미터에 심볼 추가
	params := url.Values{}
	params.Add("symbol", symbol)

	// 특정 심볼에 대한 exchangeInfo 호출
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/exchangeInfo", params, false)
	if err != nil {
		return nil, fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// exchangeInfo 응답 구조체 정의
	var exchangeInfo struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			PricePrecision    int    `json:"pricePrecision"`
			QuantityPrecision int    `json:"quantityPrecision"`
			Filters           []struct {
				FilterType  string `json:"filterType"`
				StepSize    string `json:"stepSize,omitempty"`
				TickSize    string `json:"tickSize,omitempty"`
				MinNotional string `json:"notional,omitempty"`
			} `json:"filters"`
		} `json:"symbols"`
	}

	// JSON 응답 파싱
	if err := json.Unmarshal(resp, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("심볼 정보 파싱 실패: %w", err)
	}

	// 응답에 심볼 정보가 없는 경우
	if len(exchangeInfo.Symbols) == 0 {
		return nil, fmt.Errorf("심볼 정보를 찾을 수 없음: %s", symbol)
	}

	// 첫 번째(유일한) 심볼 정보 사용
	s := exchangeInfo.Symbols[0]

	info := &domain.SymbolInfo{
		Symbol:            symbol,
		PricePrecision:    s.PricePrecision,
		QuantityPrecision: s.QuantityPrecision,
	}

	// 필터 정보 추출
	for _, filter := range s.Filters {
		switch filter.FilterType {
		case "LOT_SIZE": // 수량 단위 필터
			if filter.StepSize != "" {
				stepSize, err := strconv.ParseFloat(filter.StepSize, 64)
				if err != nil {
					continue
				}
				info.StepSize = stepSize
			}
		case "PRICE_FILTER": // 가격 단위 필터
			if filter.TickSize != "" {
				tickSize, err := strconv.ParseFloat(filter.TickSize, 64)
				if err != nil {
					continue
				}
				info.TickSize = tickSize
			}
		case "MIN_NOTIONAL": // 최소 주문 가치 필터
			if filter.MinNotional != "" {
				minNotional, err := strconv.ParseFloat(filter.MinNotional, 64)
				if err != nil {
					continue
				}
				info.MinNotional = minNotional
			}
		}
	}

	return info, nil
}

// GetTopVolumeSymbols는 거래량 기준 상위 n개 심볼을 조회합니다
func (c *Client) GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("거래량 데이터 조회 실패: %w", err)
	}

	type symbolVolume struct {
		Symbol      string  `json:"symbol"`
		QuoteVolume float64 `json:"quoteVolume,string"`
	}

	var tickers []symbolVolume
	if err := json.Unmarshal(resp, &tickers); err != nil {
		return nil, fmt.Errorf("거래량 데이터 파싱 실패: %w", err)
	}

	// USDT 마진 선물만 필터링
	var filteredTickers []symbolVolume
	for _, ticker := range tickers {
		if strings.HasSuffix(ticker.Symbol, "USDT") {
			filteredTickers = append(filteredTickers, ticker)
		}
	}

	// 거래량 기준 내림차순 정렬
	sort.Slice(filteredTickers, func(i, j int) bool {
		return filteredTickers[i].QuoteVolume > filteredTickers[j].QuoteVolume
	})

	// 상위 n개 심볼 선택
	resultCount := min(n, len(filteredTickers))
	symbols := make([]string, resultCount)
	for i := 0; i < resultCount; i++ {
		symbols[i] = filteredTickers[i].Symbol
	}

	return symbols, nil
}

// GetBalance는 계정의 잔고를 조회합니다
func (c *Client) GetBalance(ctx context.Context) (map[string]domain.Balance, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/account", nil, true)
	if err != nil {
		return nil, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	var result struct {
		Assets []struct {
			Asset              string  `json:"asset"`
			WalletBalance      float64 `json:"walletBalance,string"`
			UnrealizedProfit   float64 `json:"unrealizedProfit,string"`
			MarginBalance      float64 `json:"marginBalance,string"`
			AvailableBalance   float64 `json:"availableBalance,string"`
			InitialMargin      float64 `json:"initialMargin,string"`
			MaintMargin        float64 `json:"maintMargin,string"`
			CrossWalletBalance float64 `json:"crossWalletBalance,string"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	balances := make(map[string]domain.Balance)
	for _, asset := range result.Assets {
		// 잔고가 있는 자산만 포함 (0보다 큰 값)
		if asset.WalletBalance > 0 {
			balances[asset.Asset] = domain.Balance{
				Asset:              asset.Asset,
				Available:          asset.AvailableBalance,
				Locked:             asset.WalletBalance - asset.AvailableBalance,
				CrossWalletBalance: asset.CrossWalletBalance,
			}
		}
	}

	return balances, nil
}

// GetPositions는 현재 열린 포지션을 조회합니다
func (c *Client) GetPositions(ctx context.Context) ([]domain.Position, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/positionRisk", nil, true)
	if err != nil {
		return nil, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	var positionsRaw []struct {
		Symbol           string  `json:"symbol"`
		PositionAmt      float64 `json:"positionAmt,string"`
		EntryPrice       float64 `json:"entryPrice,string"`
		MarkPrice        float64 `json:"markPrice,string"`
		UnrealizedProfit float64 `json:"unRealizedProfit,string"`
		LiquidationPrice float64 `json:"liquidationPrice,string"`
		Leverage         float64 `json:"leverage,string"`
		MaxNotionalValue float64 `json:"maxNotionalValue,string"`
		MarginType       string  `json:"marginType"`
		IsolatedMargin   float64 `json:"isolatedMargin,string"`
		IsAutoAddMargin  string  `json:"isAutoAddMargin"`
		PositionSide     string  `json:"positionSide"`
		Notional         float64 `json:"notional,string"`
		IsolatedWallet   float64 `json:"isolatedWallet,string"`
		UpdateTime       int64   `json:"updateTime"`
		InitialMargin    float64 `json:"initialMargin,string"`
		MaintMargin      float64 `json:"maintMargin,string"`
	}

	if err := json.Unmarshal(resp, &positionsRaw); err != nil {
		return nil, fmt.Errorf("포지션 데이터 파싱 실패: %w", err)
	}

	// 활성 포지션만 필터링 (수량이 0이 아닌 포지션)
	var positions []domain.Position
	for _, p := range positionsRaw {
		if p.PositionAmt != 0 {
			leverage := int(p.Leverage)
			position := domain.Position{
				Symbol:        p.Symbol,
				PositionSide:  domain.PositionSide(p.PositionSide),
				Quantity:      p.PositionAmt,
				EntryPrice:    p.EntryPrice,
				MarkPrice:     p.MarkPrice,
				UnrealizedPnL: p.UnrealizedProfit,
				InitialMargin: p.InitialMargin,
				MaintMargin:   p.MaintMargin,
				Leverage:      leverage,
			}
			positions = append(positions, position)
		}
	}

	return positions, nil
}

// GetOpenOrders는 현재 열린 주문 목록을 조회합니다
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]domain.OrderResponse, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/openOrders", params, true)
	if err != nil {
		return nil, fmt.Errorf("열린 주문 조회 실패: %w", err)
	}

	var ordersRaw []struct {
		OrderID       int64   `json:"orderId"`
		Symbol        string  `json:"symbol"`
		Status        string  `json:"status"`
		ClientOrderID string  `json:"clientOrderId"`
		Price         float64 `json:"price,string"`
		AvgPrice      float64 `json:"avgPrice,string"`
		OrigQty       float64 `json:"origQty,string"`
		ExecutedQty   float64 `json:"executedQty,string"`
		CumQuote      float64 `json:"cumQuote,string"`
		TimeInForce   string  `json:"timeInForce"`
		Type          string  `json:"type"`
		ReduceOnly    bool    `json:"reduceOnly"`
		ClosePosition bool    `json:"closePosition"`
		Side          string  `json:"side"`
		PositionSide  string  `json:"positionSide"`
		StopPrice     float64 `json:"stopPrice,string"`
		WorkingType   string  `json:"workingType"`
		PriceProtect  bool    `json:"priceProtect"`
		OrigType      string  `json:"origType"`
		UpdateTime    int64   `json:"updateTime"`
		Time          int64   `json:"time"`
	}

	if err := json.Unmarshal(resp, &ordersRaw); err != nil {
		return nil, fmt.Errorf("주문 데이터 파싱 실패: %w", err)
	}

	orders := make([]domain.OrderResponse, len(ordersRaw))
	for i, o := range ordersRaw {
		orders[i] = domain.OrderResponse{
			OrderID:          o.OrderID,
			Symbol:           o.Symbol,
			Status:           o.Status,
			ClientOrderID:    o.ClientOrderID,
			Price:            o.Price,
			AvgPrice:         o.AvgPrice,
			OrigQuantity:     o.OrigQty,
			ExecutedQuantity: o.ExecutedQty,
			Side:             domain.OrderSide(o.Side),
			PositionSide:     domain.PositionSide(o.PositionSide),
			Type:             domain.OrderType(o.Type),
			CreateTime:       time.Unix(0, o.Time*int64(time.Millisecond)),
		}
	}

	return orders, nil
}

// GetLeverageBrackets는 심볼의 레버리지 브라켓 정보를 조회합니다
func (c *Client) GetLeverageBrackets(ctx context.Context, symbol string) ([]domain.LeverageBracket, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/leverageBracket", params, true)
	if err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 응답 구조는 심볼 기준으로 다릅니다
	// 단일 심볼 조회시: [{"symbol":"BTCUSDT","brackets":[...]}]
	// 모든 심볼 조회시: [{"symbol":"BTCUSDT","brackets":[...]}, {"symbol":"ETHUSDT","brackets":[...]}]
	var bracketsRaw []struct {
		Symbol   string `json:"symbol"`
		Brackets []struct {
			Bracket          int     `json:"bracket"`
			InitialLeverage  int     `json:"initialLeverage"`
			NotionalCap      float64 `json:"notionalCap"`
			NotionalFloor    float64 `json:"notionalFloor"`
			MaintMarginRatio float64 `json:"maintMarginRatio"`
			Cum              float64 `json:"cum"`
		} `json:"brackets"`
	}

	if err := json.Unmarshal(resp, &bracketsRaw); err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 데이터 파싱 실패: %w", err)
	}

	// 결과 처리
	var result []domain.LeverageBracket
	for _, symbolBrackets := range bracketsRaw {

		for _, b := range symbolBrackets.Brackets {
			bracket := domain.LeverageBracket{
				Bracket:          b.Bracket,
				InitialLeverage:  b.InitialLeverage,
				MaintMarginRatio: b.MaintMarginRatio,
				Notional:         b.NotionalCap,
			}
			result = append(result, bracket)
		}

		// 특정 심볼만 요청했으면 첫 번째 항목만 필요
		if symbol != "" {
			break
		}
	}

	return result, nil
}

// PlaceOrder는 새로운 주문을 생성합니다
func (c *Client) PlaceOrder(ctx context.Context, order domain.OrderRequest) (*domain.OrderResponse, error) {
	params := url.Values{}
	params.Add("symbol", order.Symbol)
	params.Add("side", string(order.Side))

	if order.PositionSide != "" {
		params.Add("positionSide", string(order.PositionSide))
	}

	switch order.Type {
	case domain.Market:
		params.Add("type", "MARKET")
		if order.QuoteQuantity > 0 {
			// USDT 금액으로 주문
			params.Add("quoteOrderQty", strconv.FormatFloat(order.QuoteQuantity, 'f', -1, 64))
		} else {
			// 코인 수량으로 주문
			params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		}

	case domain.Limit:
		params.Add("type", "LIMIT")
		params.Add("timeInForce", order.TimeInForce)
		if order.TimeInForce == "" {
			params.Add("timeInForce", "GTC")
		}
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("price", strconv.FormatFloat(order.Price, 'f', -1, 64))

	case domain.StopMarket:
		params.Add("type", "STOP_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))

	case domain.TakeProfitMarket:
		params.Add("type", "TAKE_PROFIT_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
	}

	// 클라이언트 주문 ID가 설정되었으면 추가
	if order.ClientOrderID != "" {
		params.Add("newClientOrderId", order.ClientOrderID)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/order", params, true)
	if err != nil {
		return nil, fmt.Errorf("주문 실행 실패 [심볼: %s, 타입: %s, 수량: %.8f]: %w",
			order.Symbol, order.Type, order.Quantity, err)
	}

	var result struct {
		OrderID       int64  `json:"orderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		ClientOrderID string `json:"clientOrderId"`
		Price         string `json:"price"`
		AvgPrice      string `json:"avgPrice"`
		OrigQty       string `json:"origQty"`
		ExecutedQty   string `json:"executedQty"`
		Side          string `json:"side"`
		PositionSide  string `json:"positionSide"`
		Type          string `json:"type"`
		UpdateTime    int64  `json:"updateTime"`
		Time          int64  `json:"time"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("주문 응답 파싱 실패: %w", err)
	}

	// 문자열을 숫자로 변환
	price, _ := strconv.ParseFloat(result.Price, 64)
	avgPrice, _ := strconv.ParseFloat(result.AvgPrice, 64)
	origQuantity, _ := strconv.ParseFloat(result.OrigQty, 64)
	executedQuantity, _ := strconv.ParseFloat(result.ExecutedQty, 64)

	return &domain.OrderResponse{
		OrderID:          result.OrderID,
		Symbol:           result.Symbol,
		Status:           result.Status,
		ClientOrderID:    result.ClientOrderID,
		Price:            price,
		AvgPrice:         avgPrice,
		OrigQuantity:     origQuantity,
		ExecutedQuantity: executedQuantity,
		Side:             domain.OrderSide(result.Side),
		PositionSide:     domain.PositionSide(result.PositionSide),
		Type:             domain.OrderType(result.Type),
		CreateTime:       time.Unix(0, result.Time*int64(time.Millisecond)),
	}, nil
}

// CancelOrder는 주문을 취소합니다
func (c *Client) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("orderId", strconv.FormatInt(orderID, 10))

	_, err := c.doRequest(ctx, http.MethodDelete, "/fapi/v1/order", params, true)
	if err != nil {
		return fmt.Errorf("주문 취소 실패: %w", err)
	}

	return nil
}

// SetLeverage는 레버리지를 설정합니다
func (c *Client) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("leverage", strconv.Itoa(leverage))

	_, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/leverage", params, true)
	if err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	return nil
}

// SetPositionMode는 포지션 모드를 설정합니다
func (c *Client) SetPositionMode(ctx context.Context, hedgeMode bool) error {
	params := url.Values{}
	params.Add("dualSidePosition", strconv.FormatBool(hedgeMode))

	_, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/positionSide/dual", params, true)
	if err != nil {
		// API 에러 타입 확인
		if strings.Contains(err.Error(), "No need to change position side") {
			// 이미 원하는 모드로 설정된 경우, 에러가 아님
			return nil
		}
		return fmt.Errorf("포지션 모드 설정 실패: %w", err)
	}

	return nil
}

// SyncTime은 바이낸스 서버와 시간을 동기화합니다
func (c *Client) SyncTime(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/time", nil, false)
	if err != nil {
		return fmt.Errorf("서버 시간 조회 실패: %w", err)
	}

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("서버 시간 파싱 실패: %w", err)
	}

	c.serverTimeOffset = result.ServerTime - time.Now().UnixMilli()
	return nil
}

```
## internal/exchange/exchange.go
```go
// internal/exchange/exchange.go
package exchange

import (
	"context"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// Exchange는 거래소와의 상호작용을 위한 인터페이스입니다.
type Exchange interface {
	// 시장 데이터 조회
	GetServerTime(ctx context.Context) (time.Time, error)
	GetKlines(ctx context.Context, symbol string, interval domain.TimeInterval, limit int) (domain.CandleList, error)
	GetSymbolInfo(ctx context.Context, symbol string) (*domain.SymbolInfo, error)
	GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error)

	// 계정 데이터 조회
	GetBalance(ctx context.Context) (map[string]domain.Balance, error)
	GetPositions(ctx context.Context) ([]domain.Position, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]domain.OrderResponse, error)
	GetLeverageBrackets(ctx context.Context, symbol string) ([]domain.LeverageBracket, error)

	// 거래 기능
	PlaceOrder(ctx context.Context, order domain.OrderRequest) (*domain.OrderResponse, error)
	CancelOrder(ctx context.Context, symbol string, orderID int64) error

	// 설정 기능
	SetLeverage(ctx context.Context, symbol string, leverage int) error
	SetPositionMode(ctx context.Context, hedgeMode bool) error

	// 시간 동기화
	SyncTime(ctx context.Context) error
}

```
## internal/indicator/ema.go
```go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// ------------ 결과 -------------------------------------------------------
// EMAResult는 EMA 지표 계산 결과입니다
type EMAResult struct {
	Value     float64
	Timestamp time.Time
}

// GetTimestamp는 결과의 타임스탬프를 반환합니다 (Result 인터페이스 구현)
func (r EMAResult) GetTimestamp() time.Time {
	return r.Timestamp
}

// ------------ 본체 -------------------------------------------------------
// EMA는 지수이동평균 지표를 구현합니다
type EMA struct {
	BaseIndicator
	Period int // EMA 기간
}

// NewEMA는 새로운 EMA 지표 인스턴스를 생성합니다
func NewEMA(period int) *EMA {
	return &EMA{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("EMA(%d)", period),
			Config: map[string]interface{}{
				"Period": period,
			},
		},
		Period: period,
	}
}

// Calculate는 주어진 가격 데이터에 대해 EMA를 계산합니다
func (e *EMA) Calculate(prices []PriceData) ([]Result, error) {
	if err := e.validateInput(prices); err != nil {
		return nil, err
	}

	p := e.Period
	multiplier := 2.0 / float64(p+1)
	results := make([]Result, len(prices))

	// --- 1. 초기 SMA ----------------------------------------------------
	sum := 0.0
	for i := 0; i < p; i++ {
		sum += prices[i].Close
		results[i] = EMAResult{Value: math.NaN(), Timestamp: prices[i].Time}
	}
	ema := sum / float64(p)
	results[p-1] = EMAResult{Value: ema, Timestamp: prices[p-1].Time}

	// --- 2. 이후 EMA ----------------------------------------------------
	for i := p; i < len(prices); i++ {
		ema = (prices[i].Close-ema)*multiplier + ema
		results[i] = EMAResult{Value: ema, Timestamp: prices[i].Time}
	}
	return results, nil
}

// validateInput은 입력 데이터가 유효한지 검증합니다
func (e *EMA) validateInput(prices []PriceData) error {
	if e.Period <= 0 {
		return &ValidationError{Field: "period", Err: fmt.Errorf("period must be > 0")}
	}
	if len(prices) == 0 {
		return &ValidationError{Field: "prices", Err: fmt.Errorf("가격 데이터가 비어있습니다")}
	}
	if len(prices) < e.Period {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d, 현재: %d", e.Period, len(prices)),
		}
	}
	return nil
}

```
## internal/indicator/indicator.go
```go
package indicator

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// PriceData는 지표 계산에 필요한 가격 정보를 정의합니다
type PriceData struct {
	Time   time.Time // 타임스탬프
	Open   float64   // 시가
	High   float64   // 고가
	Low    float64   // 저가
	Close  float64   // 종가
	Volume float64   // 거래량
}

// Result는 지표 계산의 기본 결과 구조체입니다
type Result interface {
	GetTimestamp() time.Time
}

// ValidationError는 입력값 검증 에러를 정의합니다
type ValidationError struct {
	Field string
	Err   error
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("유효하지 않은 %s: %v", e.Field, e.Err)
}

// Indicator는 모든 기술적 지표가 구현해야 하는 인터페이스입니다
type Indicator interface {
	// Calculate는 가격 데이터를 기반으로 지표를 계산합니다
	Calculate(data []PriceData) ([]Result, error)

	// GetName은 지표의 이름을 반환합니다
	GetName() string

	// GetConfig는 지표의 현재 설정을 반환합니다
	GetConfig() map[string]interface{}

	// UpdateConfig는 지표 설정을 업데이트합니다
	UpdateConfig(config map[string]interface{}) error
}

// BaseIndicator는 모든 지표 구현체에서 공통적으로 사용할 수 있는 기본 구현을 제공합니다
type BaseIndicator struct {
	Name   string
	Config map[string]interface{}
}

// GetName은 지표의 이름을 반환합니다
func (b *BaseIndicator) GetName() string {
	return b.Name
}

// GetConfig는 지표의 현재 설정을 반환합니다
func (b *BaseIndicator) GetConfig() map[string]interface{} {
	// 설정의 복사본 반환
	configCopy := make(map[string]interface{})
	for k, v := range b.Config {
		configCopy[k] = v
	}
	return configCopy
}

// UpdateConfig는 지표 설정을 업데이트합니다
func (b *BaseIndicator) UpdateConfig(config map[string]interface{}) error {
	// 설정 업데이트
	for k, v := range config {
		b.Config[k] = v
	}
	return nil
}

// ConvertCandlesToPriceData는 캔들 데이터를 지표 계산용 PriceData로 변환합니다
func ConvertCandlesToPriceData(candles domain.CandleList) []PriceData {
	priceData := make([]PriceData, len(candles))
	for i, candle := range candles {
		priceData[i] = PriceData{
			Time:   candle.OpenTime,
			Open:   candle.Open,
			High:   candle.High,
			Low:    candle.Low,
			Close:  candle.Close,
			Volume: candle.Volume,
		}
	}
	return priceData
}

```
## internal/indicator/macd.go
```go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// ---------------- 결과 -----------------------------------------------

// MACDResult는 MACD 지표 계산 결과입니다
type MACDResult struct {
	MACD      float64   // MACD 라인
	Signal    float64   // 시그널 라인
	Histogram float64   // 히스토그램
	Timestamp time.Time // 계산 시점
}

// GetTimestamp는 결과의 타임스탬프를 반환합니다 (Result 인터페이스 구현)
func (r MACDResult) GetTimestamp() time.Time {
	return r.Timestamp
}

// ---------------- 본체 -------------------------------------------------
// MACD는 Moving Average Convergence Divergence 지표를 구현합니다
type MACD struct {
	BaseIndicator
	ShortPeriod  int // 단기 EMA 기간
	LongPeriod   int // 장기 EMA 기간
	SignalPeriod int // 시그널 라인 기간
}

// NewMACD는 새로운 MACD 지표 인스턴스를 생성합니다
func NewMACD(shortPeriod, longPeriod, signalPeriod int) *MACD {
	return &MACD{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("MACD(%d,%d,%d)", shortPeriod, longPeriod, signalPeriod),
			Config: map[string]interface{}{
				"ShortPeriod":  shortPeriod,
				"LongPeriod":   longPeriod,
				"SignalPeriod": signalPeriod,
			},
		},
		ShortPeriod:  shortPeriod,
		LongPeriod:   longPeriod,
		SignalPeriod: signalPeriod,
	}
}

// Calculate는 주어진 가격 데이터에 대해 MACD를 계산합니다
func (m *MACD) Calculate(prices []PriceData) ([]Result, error) {
	if err := m.validateInput(prices); err != nil {
		return nil, err
	}

	// -------- ① 두 EMA 계산 ------------------------------------------
	shortEMARes, _ := NewEMA(m.ShortPeriod).Calculate(prices)
	longEMARes, _ := NewEMA(m.LongPeriod).Calculate(prices)

	longStart := m.LongPeriod - 1 // 첫 MACD가 존재하는 인덱스
	macdLen := len(prices) - longStart
	macdPD := make([]PriceData, macdLen) // MACD 값을 PriceData 형태로 저장

	for i := 0; i < macdLen; i++ {
		idx := i + longStart
		se := shortEMARes[idx].(EMAResult)
		le := longEMARes[idx].(EMAResult)
		macdPD[i] = PriceData{
			Time:  prices[idx].Time,
			Close: se.Value - le.Value,
		}
	}

	// -------- ② 시그널 EMA 계산 --------------------------------------
	signalRes, _ := NewEMA(m.SignalPeriod).Calculate(macdPD)

	// -------- ③ 최종 결과 조합 ---------------------------------------
	out := make([]Result, len(prices))
	for i := 0; i < longStart+m.SignalPeriod-1; i++ {
		out[i] = MACDResult{
			MACD:      math.NaN(),
			Signal:    math.NaN(),
			Histogram: math.NaN(),
			Timestamp: prices[i].Time,
		}
	}

	for i := m.SignalPeriod - 1; i < macdLen; i++ {
		priceIdx := i + longStart             // 원본 가격 인덱스
		sig := signalRes[i].(EMAResult).Value // 시그널 값
		macdVal := macdPD[i].Close            // MACD 값
		out[priceIdx] = MACDResult{macdVal, sig, macdVal - sig, macdPD[i].Time}
	}
	return out, nil
}

// ---------------- 검증 -------------------------------------------------
// validateInput은 입력 데이터가 유효한지 검증합니다
func (m *MACD) validateInput(prices []PriceData) error {
	switch {
	case m.ShortPeriod <= 0, m.LongPeriod <= 0, m.SignalPeriod <= 0:
		return &ValidationError{Field: "period", Err: fmt.Errorf("periods must be > 0")}
	case m.ShortPeriod >= m.LongPeriod:
		return &ValidationError{Field: "period", Err: fmt.Errorf("ShortPeriod must be < LongPeriod")}
	}

	need := (m.LongPeriod - 1) + m.SignalPeriod
	if len(prices) < need {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d, 현재: %d", need, len(prices)),
		}
	}
	return nil
}

```
## internal/indicator/rsi.go
```go
// internal/indicator/rsi.go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// RSIResult는 RSI 지표 계산 결과
type RSIResult struct {
	Value     float64   // RSI 값 (0–100, 계산 불가 구간은 math.NaN())
	AvgGain   float64   // 평균 이득
	AvgLoss   float64   // 평균 손실
	Timestamp time.Time // 계산 시점
}

// GetTimestamp는 결과의 타임스탬프를 반환
func (r RSIResult) GetTimestamp() time.Time { return r.Timestamp }

// RSI는 Relative Strength Index 지표를 구현
type RSI struct {
	BaseIndicator
	Period int // RSI 계산 기간
}

// NewRSI는 새로운 RSI 지표 인스턴스를 생성
func NewRSI(period int) *RSI {
	return &RSI{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("RSI(%d)", period),
			Config: map[string]interface{}{
				"Period": period,
			},
		},
		Period: period,
	}
}

// Calculate는 주어진 가격 데이터에 대해 RSI를 계산
func (r *RSI) Calculate(prices []PriceData) ([]Result, error) {
	if err := r.validateInput(prices); err != nil {
		return nil, err
	}

	p := r.Period
	results := make([]Result, len(prices))

	// ---------- 1. 첫 p 개의 변동 Δ 합산 (SMA) ----------------------------
	sumGain, sumLoss := 0.0, 0.0
	for i := 1; i <= p; i++ { // i <= p  ⬅ off-by-one 수정
		delta := prices[i].Close - prices[i-1].Close
		if delta > 0 {
			sumGain += delta
		} else {
			sumLoss += -delta
		}
	}
	avgGain, avgLoss := sumGain/float64(p), sumLoss/float64(p)
	results[p] = toRSI(avgGain, avgLoss, prices[p].Time)

	// ---------- 2. 이후 구간 Wilder EMA 방식 ----------------------------
	for i := p + 1; i < len(prices); i++ {
		delta := prices[i].Close - prices[i-1].Close
		gain, loss := 0.0, 0.0
		if delta > 0 {
			gain = delta
		} else {
			loss = -delta
		}

		avgGain = (avgGain*float64(p-1) + gain) / float64(p)
		avgLoss = (avgLoss*float64(p-1) + loss) / float64(p)
		results[i] = toRSI(avgGain, avgLoss, prices[i].Time)
	}

	// ---------- 3. 앞 구간(NaN) 표시 ------------------------------------
	for i := 0; i < p; i++ {
		results[i] = RSIResult{
			Value:     math.NaN(),
			AvgGain:   math.NaN(),
			AvgLoss:   math.NaN(),
			Timestamp: prices[i].Time,
		}
	}
	return results, nil
}

// --- 유틸 ---------------------------------------------------------------

func toRSI(avgGain, avgLoss float64, ts time.Time) RSIResult {
	var rsi float64
	switch {
	case avgGain == 0 && avgLoss == 0:
		rsi = 50 // 완전 횡보
	case avgLoss == 0:
		rsi = 100
	default:
		rs := avgGain / avgLoss
		rsi = 100 - 100/(1+rs)
	}
	return RSIResult{Value: rsi, AvgGain: avgGain, AvgLoss: avgLoss, Timestamp: ts}
}

func (r *RSI) validateInput(prices []PriceData) error {
	if r.Period <= 0 {
		return &ValidationError{Field: "period", Err: fmt.Errorf("period must be > 0")}
	}
	if len(prices) == 0 {
		return &ValidationError{Field: "prices", Err: fmt.Errorf("가격 데이터가 비어있습니다")}
	}
	if len(prices) <= r.Period {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d+1, 현재: %d", r.Period, len(prices)),
		}
	}
	return nil
}

```
## internal/indicator/sar.go
```go
// internal/indicator/sar.go
package indicator

import (
	"fmt"
	"math"
	"time"
)

/* -------------------- 결과 타입 -------------------- */

// SARResult 는 Parabolic SAR 한 개 지점의 계산 결과
type SARResult struct {
	SAR       float64   // SAR 값
	IsLong    bool      // 현재 상승 추세 여부
	Timestamp time.Time // 캔들 시각
}

func (r SARResult) GetTimestamp() time.Time { return r.Timestamp }

/* -------------------- 본체 -------------------- */

// SAR 은 Parabolic SAR 지표를 구현
type SAR struct {
	BaseIndicator
	AccelerationInitial float64 // AF(가속도) 시작값
	AccelerationMax     float64 // AF 최대값
}

// 새 인스턴스
func NewSAR(step, max float64) *SAR {
	return &SAR{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("SAR(%.2f,%.2f)", step, max),
			Config: map[string]interface{}{
				"AccelerationInitial": step,
				"AccelerationMax":     max,
			},
		},
		AccelerationInitial: step,
		AccelerationMax:     max,
	}
}

// 기본 파라미터(0.02, 0.2)
func NewDefaultSAR() *SAR { return NewSAR(0.02, 0.2) }

/* -------------------- 핵심 계산 -------------------- */

func (s *SAR) Calculate(prices []PriceData) ([]Result, error) {
	if err := s.validateInput(prices); err != nil {
		return nil, err
	}

	out := make([]Result, len(prices))

	// ---------- ① 초기 추세 및 SAR 결정 ----------
	isLong := prices[1].Close >= prices[0].Close // 첫 두 캔들로 방향 판정
	var sar, ep float64
	if isLong {
		// TV 방식: 직전 Low 하나만 사용
		sar = prices[0].Low
		ep = prices[1].High
	} else {
		sar = prices[0].High
		ep = prices[1].Low
	}
	af := s.AccelerationInitial

	// 첫 캔들은 NaN, 두 번째는 계산값
	out[0] = SARResult{SAR: math.NaN(), IsLong: isLong, Timestamp: prices[0].Time}
	out[1] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[1].Time}

	// ---------- ② 메인 루프 ----------
	for i := 2; i < len(prices); i++ {
		prevSAR := sar
		// 기본 파라볼릭 공식
		sar = prevSAR + af*(ep-prevSAR)

		if isLong {
			// 상승일 때: 직전 2개 Low 이하로 clamp
			sar = s.Min(sar, prices[i-1].Low, prices[i-2].Low)

			// 새로운 고점 나오면 EP·AF 갱신
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// ★ 추세 전환 조건(포함 비교) ★
			if sar >= prices[i].Low {
				// 반전 → 하락
				isLong = false
				sar = ep           // 반전 시 SAR = 직전 EP
				ep = prices[i].Low // 새 EP
				af = s.AccelerationInitial

				// TV와 동일하게 즉시 clamp
				sar = s.Max(sar, prices[i-1].High, prices[i-2].High)
			}
		} else { // 하락 추세
			// 하락일 때: 직전 2개 High 이상으로 clamp
			sar = s.Max(sar, prices[i-1].High, prices[i-2].High)

			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			if sar <= prices[i].High { // ★ 포함 비교 ★
				// 반전 → 상승
				isLong = true
				sar = ep
				ep = prices[i].High
				af = s.AccelerationInitial

				// 즉시 clamp
				sar = s.Min(sar, prices[i-1].Low, prices[i-2].Low)
			}
		}

		out[i] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[i].Time}
	}

	// /* ---------- ③ (선택) TV와 동일하게 한 캔들 우측으로 밀기 ----------
	//    TradingView 는 t-1 시점에 계산한 SAR 을 t 캔들 밑에 찍는다.
	//    차트에 그대로 맞추고 싶다면 주석 해제.

	for i := len(out) - 1; i > 0; i-- {
		out[i] = out[i-1]
	}
	out[0] = SARResult{SAR: math.NaN(), IsLong: out[1].(SARResult).IsLong, Timestamp: prices[0].Time}
	// ------------------------------------------------------------------ */

	return out, nil
}

/* -------------------- 입력 검증 -------------------- */

func (s *SAR) validateInput(prices []PriceData) error {
	switch {
	case len(prices) < 3:
		return &ValidationError{Field: "prices", Err: fmt.Errorf("가격 데이터가 최소 3개 필요합니다")}
	case s.AccelerationInitial <= 0 || s.AccelerationMax <= 0:
		return &ValidationError{Field: "acceleration", Err: fmt.Errorf("가속도(step/max)는 0보다 커야 합니다")}
	case s.AccelerationInitial > s.AccelerationMax:
		return &ValidationError{Field: "acceleration", Err: fmt.Errorf("AccelerationMax는 AccelerationInitial 이상이어야 합니다")}
	}
	return nil
}

/* -------------------- 보조: 다중 min/max -------------------- */

func (SAR) Min(vals ...float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func (SAR) Max(vals ...float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

```
## internal/market/client.go
```go
package market

```
## internal/market/collector.go
```go
package market

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/exchange"
	"github.com/assist-by/phoenix/internal/notification/discord"
	"github.com/assist-by/phoenix/internal/position"
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
	exchange        exchange.Exchange
	discord         *discord.Client
	strategy        strategy.Strategy
	config          *config.Config
	positionManager position.Manager

	retry RetryConfig
	mu    sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(exchange exchange.Exchange, discord *discord.Client, strategy strategy.Strategy, positionManager position.Manager, config *config.Config, opts ...CollectorOption) *Collector {
	c := &Collector{
		exchange:        exchange,
		discord:         discord,
		strategy:        strategy,
		positionManager: positionManager,
		config:          config,
		retry: RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
			Factor:     2.0,
		},
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
			errMsg := fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
			if discordErr := c.discord.SendError(errMsg); discordErr != nil {
				log.Printf("에러 알림 전송 실패: %v", discordErr)
			}
			return errMsg
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

	// 잔고 정보 조회
	balances, err := c.exchange.GetBalance(ctx)
	if err != nil {
		errMsg := fmt.Errorf("잔고 조회 실패: %w", err)
		if discordErr := c.discord.SendError(errMsg); discordErr != nil {
			log.Printf("에러 알림 전송 실패: %v", discordErr)
		}
		return errMsg
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

			// 시그널 감지
			signal, err := c.strategy.Analyze(ctx, symbol, candles)
			if err != nil {
				log.Printf("시그널 감지 실패 (%s): %v", symbol, err)
				return nil
			}

			// 시그널 정보 로깅
			log.Printf("%s 시그널 감지 결과: %+v", symbol, signal)

			if signal != nil {
				// Discord로 시그널 알림 전송
				if err := c.discord.SendSignal(signal); err != nil {
					log.Printf("시그널 알림 전송 실패 (%s): %v", symbol, err)
				}

				if signal.GetType() != domain.NoSignal {
					// 매매 실행
					if err := c.ExecuteSignalTrade(ctx, signal); err != nil {
						errMsg := fmt.Errorf("매매 실행 실패 (%s): %w", symbol, err)
						if discordErr := c.discord.SendError(errMsg); discordErr != nil {
							log.Printf("에러 알림 전송 실패: %v", discordErr)
						}
						return errMsg
					} else {
						log.Printf("%s %s 포지션 진입 및 TP/SL 설정 완료", signal.GetSymbol(), signal.GetType().String())
					}
				}
			}

			return nil
		})

		if err != nil {
			log.Printf("%s 심볼 데이터 수집 실패: %v", symbol, err)
			continue // 한 심볼 처리 실패해도 다음 심볼 진행
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

// TODO: 단순 상향돌파만 체크하는게 아니라 MACD가 0 이상인지 이하인지 그거도 추세 판단하는데 사용되는걸 적용해야한다.
// ExecuteSignalTrade는 감지된 시그널에 따라 매매를 실행합니다
func (c *Collector) ExecuteSignalTrade(ctx context.Context, s domain.SignalInterface) error {
	if s.GetType() == domain.NoSignal {
		return nil // 시그널이 없으면 아무것도 하지 않음
	}

	// 포지션 요청 객체 생성
	req := &position.PositionRequest{
		Signal:     s,
		Leverage:   c.config.Trading.Leverage,
		RiskFactor: 0.9, // 계정 잔고의 90% 사용 (설정에서 가져올 수도 있음)
	}

	// 포지션 매니저를 통해 포지션 오픈
	_, err := c.positionManager.OpenPosition(ctx, req)
	if err != nil {
		// 에러 발생 시 Discord 알림 전송
		errorMsg := fmt.Sprintf("포지션 진입 실패 (%s): %v", s.GetSymbol(), err)
		if discordErr := c.discord.SendError(fmt.Errorf(errorMsg)); discordErr != nil {
			log.Printf("에러 알림 전송 실패: %v", discordErr)
		}

		return fmt.Errorf("매매 실행 실패: %w", err)
	}

	return nil
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

// IsRetryableError 함수는 재 시도 할 작업인지 검사하는 함수
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// HTTP 관련 일시적 오류인 경우만 재시도
	if strings.Contains(errStr, "HTTP 에러(502)") || // Bad Gateway
		strings.Contains(errStr, "HTTP 에러(503)") || // Service Unavailable
		strings.Contains(errStr, "HTTP 에러(504)") || // Gateway Timeout
		strings.Contains(errStr, "i/o timeout") || // 타임아웃
		strings.Contains(errStr, "connection refused") || // 연결 거부
		strings.Contains(errStr, "EOF") || // 예기치 않은 연결 종료
		strings.Contains(errStr, "no such host") { // DNS 해석 실패
		return true
	}

	return false
}

```
## internal/market/types.go
```go
package market

import "fmt"

// APIError는 바이낸스 API 에러를 표현합니다
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("바이낸스 API 에러(코드: %d): %s", e.Code, e.Message)
}

// OrderSide는 주문 방향을 정의합니다
type OrderSide string

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"

	Long  PositionSide = "LONG"
	Short PositionSide = "SHORT"
)

// PositionSizeResult는 포지션 계산 결과를 담는 구조체입니다
type PositionSizeResult struct {
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매 수량 (코인)
}

```
## internal/notification/discord/client.go
```go
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client는 Discord 웹훅 클라이언트입니다
type Client struct {
	signalWebhook string // 시그널 알림용 웹훅
	tradeWebhook  string // 거래 실행 알림용 웹훅
	errorWebhook  string // 에러 알림용 웹훅
	infoWebhook   string // 정보 알림용 웹훅
	client        *http.Client
}

// ClientOption은 Discord 클라이언트 옵션을 정의합니다
type ClientOption func(*Client)

// WithTimeout은 HTTP 클라이언트의 타임아웃을 설정합니다
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.client.Timeout = timeout
	}
}

// NewClient는 새로운 Discord 클라이언트를 생성합니다
func NewClient(signalWebhook, tradeWebhook, errorWebhook, infoWebhook string, opts ...ClientOption) *Client {
	c := &Client{
		signalWebhook: signalWebhook,
		tradeWebhook:  tradeWebhook,
		errorWebhook:  errorWebhook,
		infoWebhook:   infoWebhook,
		client:        &http.Client{Timeout: 10 * time.Second},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// sendToWebhook은 지정된 웹훅으로 메시지를 전송합니다
func (c *Client) sendToWebhook(webhookURL string, message WebhookMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("메시지 마샬링 실패: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("요청 생성 실패: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("요청 전송 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("웹훅 전송 실패: status=%d", resp.StatusCode)
	}

	return nil
}

```
## internal/notification/discord/embed.go
```go
package discord

import (
	"time"
)

// WebhookMessage는 Discord 웹훅 메시지를 정의합니다
type WebhookMessage struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// Embed는 Discord 메시지 임베드를 정의합니다
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// EmbedField는 임베드 필드를 정의합니다
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// EmbedFooter는 임베드 푸터를 정의합니다
type EmbedFooter struct {
	Text string `json:"text"`
}

// NewEmbed는 새로운 임베드를 생성합니다
func NewEmbed() *Embed {
	return &Embed{}
}

// SetTitle은 임베드 제목을 설정합니다
func (e *Embed) SetTitle(title string) *Embed {
	e.Title = title
	return e
}

// SetDescription은 임베드 설명을 설정합니다
func (e *Embed) SetDescription(desc string) *Embed {
	e.Description = desc
	return e
}

// SetColor는 임베드 색상을 설정합니다
func (e *Embed) SetColor(color int) *Embed {
	e.Color = color
	return e
}

// AddField는 임베드에 필드를 추가합니다
func (e *Embed) AddField(name, value string, inline bool) *Embed {
	e.Fields = append(e.Fields, EmbedField{
		Name:   name,
		Value:  value,
		Inline: inline,
	})
	return e
}

// SetFooter는 임베드 푸터를 설정합니다
func (e *Embed) SetFooter(text string) *Embed {
	e.Footer = &EmbedFooter{Text: text}
	return e
}

// SetTimestamp는 임베드 타임스탬프를 설정합니다
func (e *Embed) SetTimestamp(t time.Time) *Embed {
	e.Timestamp = t.Format(time.RFC3339)
	return e
}

```
## internal/notification/discord/webhook.go
```go
package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s domain.SignalInterface) error {
	if s == nil {
		return fmt.Errorf("nil signal received")
	}

	var title, emoji string
	var color int

	switch s.GetType() {
	case domain.Long:
		emoji = "🚀"
		title = "LONG"
		color = notification.ColorSuccess
	case domain.Short:
		emoji = "🔻"
		title = "SHORT"
		color = notification.ColorError
	case domain.PendingLong:
		emoji = "⏳"
		title = "PENDING LONG"
		color = notification.ColorWarning
	case domain.PendingShort:
		emoji = "⏳"
		title = "PENDING SHORT"
		color = notification.ColorWarning
	default:
		emoji = "⚠️"
		title = "NO SIGNAL"
		color = notification.ColorInfo
	}

	// 알림 데이터 가져오기
	notificationData := s.ToNotificationData()

	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s %s/USDT", emoji, title, s.GetSymbol())).
		SetColor(color)

	// 기본 정보 설정 - 모든 시그널 타입에 공통
	if s.GetType() != domain.NoSignal {
		// 손익률 계산 및 표시
		var slPct, tpPct float64
		switch s.GetType() {
		case domain.Long:
			// Long: 실제 수치 그대로 표시
			slPct = (s.GetStopLoss() - s.GetPrice()) / s.GetPrice() * 100
			tpPct = (s.GetTakeProfit() - s.GetPrice()) / s.GetPrice() * 100
		case domain.Short:
			// Short: 부호 반대로 표시
			slPct = (s.GetPrice() - s.GetStopLoss()) / s.GetPrice() * 100
			tpPct = (s.GetPrice() - s.GetTakeProfit()) / s.GetPrice() * 100
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.GetTimestamp().Format("2006-01-02 15:04:05 KST"),
			s.GetPrice(),
			s.GetStopLoss(),
			slPct,
			s.GetTakeProfit(),
			tpPct,
		))
	} else if s.GetType() == domain.PendingLong || s.GetType() == domain.PendingShort {
		// 대기 상태 정보 표시
		var waitingFor string
		if s.GetType() == domain.PendingLong {
			waitingFor = "진입 대기 중"
		} else {
			waitingFor = "진입 대기 중"
		}

		// notificationData에서 대기 상태 설명이 있으면 사용
		if waitDesc, hasWaitDesc := notificationData["대기상태"]; hasWaitDesc {
			waitingFor = waitDesc.(string)
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
**현재가**: $%.2f
**대기 상태**: %s`,
			s.GetTimestamp().Format("2006-01-02 15:04:05 KST"),
			s.GetPrice(),
			waitingFor,
		))
	} else {
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f`,
			s.GetTimestamp().Format("2006-01-02 15:04:05 KST"),
			s.GetPrice(),
		))
	}

	// 전략별 필드들 추가
	// 전략은 ToNotificationData에서 "필드" 키로 필드 목록을 제공할 수 있음
	if fields, hasFields := notificationData["field"].([]map[string]interface{}); hasFields {
		for _, field := range fields {
			name, _ := field["name"].(string)
			value, _ := field["value"].(string)
			inline, _ := field["inline"].(bool)
			embed.AddField(name, value, inline)
		}
	} else {
		// 기본 필드 추가 (전략이 "필드"를 제공하지 않는 경우)
		// 기술적 지표 요약 표시
		if technicalSummary, hasSummary := notificationData["기술지표요약"].(string); hasSummary {
			embed.AddField("기술적 지표", technicalSummary, false)
		}

		// 기타 조건들 표시
		if conditions, hasConditions := notificationData["조건"].(string); hasConditions {
			embed.AddField("조건", conditions, false)
		}
	}

	return c.sendToWebhook(c.signalWebhook, WebhookMessage{
		Embeds: []Embed{*embed},
	})
}

// SendError는 에러 알림을 전송합니다
func (c *Client) SendError(err error) error {
	embed := NewEmbed().
		SetTitle("에러 발생").
		SetDescription(fmt.Sprintf("```%v```", err)).
		SetColor(notification.ColorError).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.errorWebhook, msg)
}

// SendInfo는 일반 정보 알림을 전송합니다
func (c *Client) SendInfo(message string) error {
	embed := NewEmbed().
		SetDescription(message).
		SetColor(notification.ColorInfo).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.infoWebhook, msg)
}

// SendTradeInfo는 거래 실행 정보를 전송합니다
func (c *Client) SendTradeInfo(info notification.TradeInfo) error {
	embed := NewEmbed().
		SetTitle(fmt.Sprintf("거래 실행: %s", info.Symbol)).
		SetDescription(fmt.Sprintf(
			"**포지션**: %s\n**수량**: %.8f %s\n**포지션 크기**: %.2f USDT\n**레버리지**: %dx\n**진입가**: $%.2f\n**손절가**: $%.2f\n**목표가**: $%.2f\n**현재 잔고**: %.2f USDT",
			info.PositionType,
			info.Quantity,
			strings.TrimSuffix(info.Symbol, "USDT"), // BTCUSDT에서 BTC만 추출
			info.PositionValue,
			info.Leverage,
			info.EntryPrice,
			info.StopLoss,
			info.TakeProfit,
			info.Balance,
		)).
		SetColor(notification.GetColorForPosition(info.PositionType)).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.tradeWebhook, msg)
}

```
## internal/notification/types.go
```go
package notification

import "github.com/assist-by/phoenix/internal/domain"

const (
	ColorSuccess = 0x00FF00 // 녹색
	ColorError   = 0xFF0000 // 빨간색
	ColorInfo    = 0x0000FF // 파란색
	ColorWarning = 0xFFA500 // 주황색
)

// Notifier는 알림 전송 인터페이스를 정의합니다
type Notifier interface {
	// SendSignal은 트레이딩 시그널 알림을 전송합니다
	SendSignal(signal domain.SignalInterface) error

	// SendError는 에러 알림을 전송합니다
	SendError(err error) error

	// SendInfo는 일반 정보 알림을 전송합니다
	SendInfo(message string) error

	// SendTradeInfo는 거래 실행 정보를 전송합니다
	SendTradeInfo(info TradeInfo) error
}

// TradeInfo는 거래 실행 정보를 정의합니다
type TradeInfo struct {
	Symbol        string  // 심볼 (예: BTCUSDT)
	PositionType  string  // "LONG" or "SHORT"
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매/판매 수량 (코인)
	EntryPrice    float64 // 진입가
	StopLoss      float64 // 손절가
	TakeProfit    float64 // 익절가
	Balance       float64 // 현재 USDT 잔고
	Leverage      int     // 사용 레버리지
}

// GetColorForPosition은 포지션 타입에 따른 색상을 반환합니다
func GetColorForPosition(positionType string) int {
	switch positionType {
	case "LONG":
		return ColorSuccess
	case "SHORT":
		return ColorError
	case "PENDINGLONG", "PENDINGSHORT":
		return ColorWarning
	default:
		return ColorInfo
	}
}

```
## internal/position/binance/manager.go
```go
package binance

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/exchange"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/position"
	"github.com/assist-by/phoenix/internal/strategy"
)

// BinancePositionManager는 바이낸스에서 포지션 관리를 담당합니다
type BinancePositionManager struct {
	exchange   exchange.Exchange
	notifier   notification.Notifier
	strategy   strategy.Strategy
	maxRetries int
	retryDelay time.Duration
}

// NewManager는 새로운 바이낸스 포지션 매니저를 생성합니다
func NewManager(exchange exchange.Exchange, notifier notification.Notifier, strategy strategy.Strategy) position.Manager {
	return &BinancePositionManager{
		exchange:   exchange,
		notifier:   notifier,
		strategy:   strategy,
		maxRetries: 5,
		retryDelay: 1 * time.Second,
	}
}

// OpenPosition은 시그널에 따라 새 포지션을 생성합니다
func (m *BinancePositionManager) OpenPosition(ctx context.Context, req *position.PositionRequest) (*position.PositionResult, error) {
	symbol := req.Signal.GetSymbol()
	signalType := req.Signal.GetType()

	// 1. 진입 가능 여부 확인
	available, err := m.IsEntryAvailable(ctx, symbol, signalType)
	if err != nil {
		return nil, position.NewPositionError(symbol, "check_availability", err)
	}

	if !available {
		return nil, position.NewPositionError(symbol, "check_availability", position.ErrPositionExists)
	}

	// 2. 현재 가격 확인
	candles, err := m.exchange.GetKlines(ctx, symbol, "1m", 1)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_price", err)
	}
	if len(candles) == 0 {
		return nil, position.NewPositionError(symbol, "get_price", fmt.Errorf("캔들 데이터를 가져오지 못했습니다"))
	}
	currentPrice := candles[0].Close

	// 3. 심볼 정보 조회
	symbolInfo, err := m.exchange.GetSymbolInfo(ctx, symbol)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_symbol_info", err)
	}

	// 4. HEDGE 모드 설정
	if err := m.exchange.SetPositionMode(ctx, true); err != nil {
		return nil, position.NewPositionError(symbol, "set_hedge_mode", err)
	}

	// 5. 레버리지 설정
	leverage := req.Leverage
	if err := m.exchange.SetLeverage(ctx, symbol, leverage); err != nil {
		return nil, position.NewPositionError(symbol, "set_leverage", err)
	}

	// 6. 잔고 확인
	balances, err := m.exchange.GetBalance(ctx)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_balance", err)
	}

	usdtBalance, exists := balances["USDT"]
	if !exists || usdtBalance.Available <= 0 {
		return nil, position.NewPositionError(symbol, "check_balance", position.ErrInsufficientBalance)
	}

	// 7. 레버리지 브라켓 정보 조회
	brackets, err := m.exchange.GetLeverageBrackets(ctx, symbol)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_leverage_brackets", err)
	}

	if len(brackets) == 0 {
		return nil, position.NewPositionError(symbol, "get_leverage_brackets", fmt.Errorf("레버리지 브라켓 정보가 없습니다"))
	}

	// 적절한 브라켓 찾기
	var maintMarginRate float64 = 0.01 // 기본값
	for _, bracket := range brackets {
		if bracket.InitialLeverage >= leverage {
			maintMarginRate = bracket.MaintMarginRatio
			break
		}
	}

	// 8. 포지션 크기 계산
	sizingConfig := position.SizingConfig{
		AccountBalance:   usdtBalance.CrossWalletBalance,
		AvailableBalance: usdtBalance.Available,
		Leverage:         leverage,
		MaxAllocation:    0.9, // 90% 사용
		StepSize:         symbolInfo.StepSize,
		TickSize:         symbolInfo.TickSize,
		MinNotional:      symbolInfo.MinNotional,
		MaintMarginRate:  maintMarginRate,
	}

	posResult, err := position.CalculatePositionSize(currentPrice, sizingConfig)
	if err != nil {
		return nil, position.NewPositionError(symbol, "calculate_position", err)
	}
	if m.notifier != nil {
		m.notifier.SendInfo(fmt.Sprintf("💰 포지션 계산: %.2f USDT, 수량: %.8f",
			posResult.PositionValue, posResult.Quantity))
	}

	// 9. 수량 정밀도 조정
	adjustedQuantity := domain.AdjustQuantity(posResult.Quantity, symbolInfo.StepSize, symbolInfo.QuantityPrecision)

	// 10. 포지션 방향 결정
	positionSide := position.GetPositionSideFromSignal(req.Signal.GetType())
	orderSide := position.GetOrderSideForEntry(positionSide)

	// 11. 진입 주문 생성
	entryOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         orderSide,
		PositionSide: positionSide,
		Type:         domain.Market,
		Quantity:     adjustedQuantity,
	}

	// 12. 진입 주문 실행
	orderResponse, err := m.exchange.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return nil, position.NewPositionError(symbol, "place_entry_order", err)
	}

	if m.notifier != nil {
		m.notifier.SendInfo(fmt.Sprintf("✅ 포지션 진입 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
			symbol, adjustedQuantity, orderResponse.OrderID))
	}

	// 13. 포지션 확인
	var actualPosition *domain.Position
	for i := 0; i < m.maxRetries; i++ {
		positions, err := m.exchange.GetPositions(ctx)
		if err != nil {
			time.Sleep(m.retryDelay)
			continue
		}

		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionSide == positionSide && math.Abs(pos.Quantity) > 0 {
				actualPosition = &pos
				break
			}
		}

		if actualPosition != nil {
			break
		}

		time.Sleep(m.retryDelay)
	}

	if actualPosition == nil {
		return nil, position.NewPositionError(symbol, "confirm_position", fmt.Errorf("포지션 확인 실패"))
	}

	// 14. TP/SL 설정
	// 시그널에서 직접 TP/SL 값 사용
	stopLoss := domain.AdjustPrice(req.Signal.GetStopLoss(), symbolInfo.TickSize, symbolInfo.PricePrecision)
	takeProfit := domain.AdjustPrice(req.Signal.GetTakeProfit(), symbolInfo.TickSize, symbolInfo.PricePrecision)

	// 15. TP/SL 주문 생성
	exitSide := position.GetOrderSideForExit(positionSide)

	// 손절 주문
	slOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         exitSide,
		PositionSide: positionSide,
		Type:         domain.StopMarket,
		Quantity:     math.Abs(actualPosition.Quantity),
		StopPrice:    stopLoss,
	}

	// 익절 주문
	tpOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         exitSide,
		PositionSide: positionSide,
		Type:         domain.TakeProfitMarket,
		Quantity:     math.Abs(actualPosition.Quantity),
		StopPrice:    takeProfit,
	}

	// 16. TP/SL 주문 실행
	slResponse, err := m.exchange.PlaceOrder(ctx, slOrder)
	if err != nil {
		log.Printf("손절 주문 실패: %v", err)
		// 진입은 성공했으므로 에러는 기록만 하고 계속 진행
	}

	tpResponse, err := m.exchange.PlaceOrder(ctx, tpOrder)
	if err != nil {
		log.Printf("익절 주문 실패: %v", err)
		// 진입은 성공했으므로 에러는 기록만 하고 계속 진행
	}

	if m.notifier != nil {
		m.notifier.SendInfo(fmt.Sprintf("🔄 TP/SL 설정 완료: %s\n손절(SL): %.2f\n익절(TP): %.2f",
			symbol, stopLoss, takeProfit))
	}

	// 17. 결과 생성
	result := &position.PositionResult{
		Symbol:        symbol,
		PositionSide:  positionSide,
		EntryPrice:    actualPosition.EntryPrice,
		Quantity:      math.Abs(actualPosition.Quantity),
		PositionValue: posResult.PositionValue,
		Leverage:      leverage,
		StopLoss:      stopLoss,
		TakeProfit:    takeProfit,
		OrderIDs: map[string]int64{
			"entry": orderResponse.OrderID,
		},
	}

	// TP/SL 주문 ID 추가 (성공한 경우만)
	if slResponse != nil {
		result.OrderIDs["sl"] = slResponse.OrderID
	}
	if tpResponse != nil {
		result.OrderIDs["tp"] = tpResponse.OrderID
	}

	// 18. 알림 전송
	if m.notifier != nil {
		tradeInfo := notification.TradeInfo{
			Symbol:        symbol,
			PositionType:  string(positionSide),
			PositionValue: posResult.PositionValue,
			Quantity:      result.Quantity,
			EntryPrice:    result.EntryPrice,
			StopLoss:      stopLoss,
			TakeProfit:    takeProfit,
			Balance:       usdtBalance.Available - posResult.PositionValue,
			Leverage:      leverage,
		}

		if err := m.notifier.SendTradeInfo(tradeInfo); err != nil {
			log.Printf("거래 정보 알림 전송 실패: %v", err)
		}
	}

	return result, nil
}

// IsEntryAvailable은 새 포지션 진입이 가능한지 확인합니다
func (m *BinancePositionManager) IsEntryAvailable(ctx context.Context, symbol string, signalType domain.SignalType) (bool, error) {
	// 1. 현재 포지션 조회
	positions, err := m.exchange.GetPositions(ctx)
	if err != nil {
		return false, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 결정할 포지션 사이드
	targetSide := position.GetPositionSideFromSignal(signalType)

	// 기존 포지션 확인
	for _, pos := range positions {
		if pos.Symbol == symbol && math.Abs(pos.Quantity) > 0 {
			// 같은 방향의 포지션이 있으면 진입 불가
			if (pos.PositionSide == targetSide) ||
				(pos.PositionSide == domain.BothPosition &&
					((targetSide == domain.LongPosition && pos.Quantity > 0) ||
						(targetSide == domain.ShortPosition && pos.Quantity < 0))) {
				return false, nil
			}

			// 반대 방향의 포지션이 있을 경우, Signal Reversal 처리
			// 기존 포지션은 청산하고, 새 포지션 진입은 허용 (true 반환)
			if m.notifier != nil {
				m.notifier.SendInfo(fmt.Sprintf("반대 방향 포지션 감지: %s, 수량: %.8f, 방향: %s",
					symbol, math.Abs(pos.Quantity), pos.PositionSide))
			}

			// 기존 주문 취소
			if err := m.CancelAllOrders(ctx, symbol); err != nil {
				return false, fmt.Errorf("기존 주문 취소 실패: %w", err)
			}

			// 포지션 청산
			closeOrder := domain.OrderRequest{
				Symbol:       symbol,
				Side:         position.GetOrderSideForExit(pos.PositionSide),
				PositionSide: pos.PositionSide,
				Type:         domain.Market,
				Quantity:     math.Abs(pos.Quantity),
			}

			_, err := m.exchange.PlaceOrder(ctx, closeOrder)
			if err != nil {
				return false, fmt.Errorf("포지션 청산 실패: %w", err)
			}

			// 포지션 청산 확인
			for i := 0; i < m.maxRetries; i++ {
				cleared := true
				positions, err := m.exchange.GetPositions(ctx)
				if err != nil {
					time.Sleep(m.retryDelay)
					continue
				}

				for _, pos := range positions {
					if pos.Symbol == symbol && math.Abs(pos.Quantity) > 0 {
						cleared = false
						break
					}
				}

				if cleared {
					if m.notifier != nil {
						m.notifier.SendInfo(fmt.Sprintf("✅ %s 포지션이 성공적으로 청산되었습니다. 반대 포지션 진입 준비 완료", symbol))
					}
					return true, nil // 청산 성공, 새 포지션 진입 허용
				}

				time.Sleep(m.retryDelay)
			}

			return false, fmt.Errorf("최대 재시도 횟수 초과: 포지션이 청산되지 않음")
		}
	}

	// 2. 열린 주문 확인 및 취소
	openOrders, err := m.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return false, fmt.Errorf("주문 조회 실패: %w", err)
	}

	if len(openOrders) > 0 {
		log.Printf("%s의 기존 주문 %d개를 취소합니다.", symbol, len(openOrders))

		for _, order := range openOrders {
			if err := m.exchange.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				return false, fmt.Errorf("주문 취소 실패 (ID: %d): %w", order.OrderID, err)
			}
			log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
		}
	}

	return true, nil
}

// CancelAllOrders는 특정 심볼의 모든 열린 주문을 취소합니다
func (m *BinancePositionManager) CancelAllOrders(ctx context.Context, symbol string) error {
	openOrders, err := m.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("주문 조회 실패: %w", err)
	}

	if len(openOrders) > 0 {
		for _, order := range openOrders {
			if err := m.exchange.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				return fmt.Errorf("주문 취소 실패 (ID: %d): %w", order.OrderID, err)
			}
			log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
		}

		if m.notifier != nil {
			m.notifier.SendInfo(fmt.Sprintf("🗑️ %s의 기존 주문 %d개가 모두 취소되었습니다.",
				symbol, len(openOrders)))
		}
	}

	return nil
}

// ClosePosition은 특정 심볼의 포지션을 청산합니다
func (m *BinancePositionManager) ClosePosition(ctx context.Context, symbol string, positionSide domain.PositionSide) (*position.PositionResult, error) {
	// 1. 포지션 조회
	positions, err := m.exchange.GetPositions(ctx)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_positions", err)
	}

	// 2. 해당 심볼 포지션 찾기
	var targetPosition *domain.Position
	for _, pos := range positions {
		if pos.Symbol == symbol && pos.PositionSide == positionSide && math.Abs(pos.Quantity) > 0 {
			targetPosition = &pos
			break
		}
	}

	if targetPosition == nil {
		return nil, position.NewPositionError(symbol, "find_position", position.ErrPositionNotFound)
	}

	// 3. 기존 주문 취소
	if err := m.CancelAllOrders(ctx, symbol); err != nil {
		return nil, position.NewPositionError(symbol, "cancel_orders", err)
	}

	// 4. 청산 주문 생성
	exitSide := position.GetOrderSideForExit(positionSide)

	closeOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         exitSide,
		PositionSide: positionSide,
		Type:         domain.Market,
		Quantity:     math.Abs(targetPosition.Quantity),
	}

	// 5. 청산 주문 실행
	orderResponse, err := m.exchange.PlaceOrder(ctx, closeOrder)
	if err != nil {
		return nil, position.NewPositionError(symbol, "place_close_order", err)
	}

	if m.notifier != nil {
		m.notifier.SendInfo(fmt.Sprintf("🔴 포지션 청산 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
			symbol, math.Abs(targetPosition.Quantity), orderResponse.OrderID))
	}

	// 6. 포지션 청산 확인
	cleared := false
	for i := 0; i < m.maxRetries; i++ {
		positions, err := m.exchange.GetPositions(ctx)
		if err != nil {
			time.Sleep(m.retryDelay)
			continue
		}

		found := false
		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionSide == positionSide && math.Abs(pos.Quantity) > 0 {
				found = true
				break
			}
		}

		if !found {
			cleared = true
			break
		}

		time.Sleep(m.retryDelay)
	}

	// 7. 결과 생성
	realizedPnL := targetPosition.UnrealizedPnL

	result := &position.PositionResult{
		Symbol:       symbol,
		PositionSide: positionSide,
		EntryPrice:   targetPosition.EntryPrice,
		Quantity:     math.Abs(targetPosition.Quantity),
		Leverage:     targetPosition.Leverage,
		OrderIDs: map[string]int64{
			"close": orderResponse.OrderID,
		},
		RealizedPnL: &realizedPnL,
	}

	if cleared {
		if m.notifier != nil {
			// 수익/손실 정보 포함
			pnlText := "손실"
			if realizedPnL > 0 {
				pnlText = "수익"
			}
			m.notifier.SendInfo(fmt.Sprintf("✅ %s 포지션 청산 완료: %.2f USDT %s",
				symbol, math.Abs(realizedPnL), pnlText))
		}
	} else {
		// 청산 확인 실패 시
		if m.notifier != nil {
			m.notifier.SendError(fmt.Errorf("❌ %s 포지션 청산 확인 실패", symbol))
		}
	}

	return result, nil
}

// GetActivePositions는 현재 활성화된 포지션 목록을 반환합니다
func (m *BinancePositionManager) GetActivePositions(ctx context.Context) ([]domain.Position, error) {
	positions, err := m.exchange.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 활성 포지션만 필터링
	var activePositions []domain.Position
	for _, pos := range positions {
		if math.Abs(pos.Quantity) > 0 {
			activePositions = append(activePositions, pos)
		}
	}

	return activePositions, nil
}

```
## internal/position/errors.go
```go
package position

import "fmt"

// Error 타입들은 포지션 관리 중 발생할 수 있는 다양한 에러를 정의합니다
var (
	ErrInsufficientBalance   = fmt.Errorf("잔고가 부족합니다")
	ErrPositionExists        = fmt.Errorf("이미 해당 심볼에 포지션이 존재합니다")
	ErrPositionNotFound      = fmt.Errorf("해당 심볼에 포지션이 존재하지 않습니다")
	ErrInvalidTPSLConfig     = fmt.Errorf("잘못된 TP/SL 설정입니다")
	ErrOrderCancellationFail = fmt.Errorf("주문 취소에 실패했습니다")
	ErrOrderPlacementFail    = fmt.Errorf("주문 생성에 실패했습니다")
)

// PositionError는 포지션 관리 에러를 확장한 구조체입니다
type PositionError struct {
	Symbol string
	Op     string
	Err    error
}

// Error는 error 인터페이스를 구현합니다
func (e *PositionError) Error() string {
	if e.Symbol != "" {
		return fmt.Sprintf("포지션 에러 [%s, 작업: %s]: %v", e.Symbol, e.Op, e.Err)
	}
	return fmt.Sprintf("포지션 에러 [작업: %s]: %v", e.Op, e.Err)
}

// Unwrap은 내부 에러를 반환합니다 (errors.Is/As 지원을 위함)
func (e *PositionError) Unwrap() error {
	return e.Err
}

// NewPositionError는 새로운 PositionError를 생성합니다
func NewPositionError(symbol, op string, err error) *PositionError {
	return &PositionError{
		Symbol: symbol,
		Op:     op,
		Err:    err,
	}
}

```
## internal/position/manager.go
```go
package position

import (
	"context"

	"github.com/assist-by/phoenix/internal/domain"
)

// PositionRequest는 포지션 생성/관리 요청 정보를 담습니다
type PositionRequest struct {
	Signal     domain.SignalInterface // 시그널
	Leverage   int                    // 사용할 레버리지
	RiskFactor float64                // 리스크 팩터 (계정 잔고의 몇 %를 리스크로 설정할지)
}

// PositionResult는 포지션 생성/관리 결과 정보를 담습니다
type PositionResult struct {
	Symbol        string              // 심볼 (예: BTCUSDT)
	PositionSide  domain.PositionSide // 롱/숏 포지션
	EntryPrice    float64             // 진입가
	Quantity      float64             // 수량
	PositionValue float64             // 포지션 가치 (USDT)
	Leverage      int                 // 레버리지
	StopLoss      float64             // 손절가
	TakeProfit    float64             // 익절가
	OrderIDs      map[string]int64    // 주문 ID (key: "entry", "tp", "sl")
	RealizedPnL   *float64            // 실현 손익 (청산 시에만 설정)
}

// Manager는 포지션 관리를 담당하는 인터페이스입니다
type Manager interface {
	// OpenPosition은 시그널에 따라 새 포지션을 생성합니다
	OpenPosition(ctx context.Context, req *PositionRequest) (*PositionResult, error)

	// ClosePosition은 특정 심볼의 포지션을 청산합니다
	ClosePosition(ctx context.Context, symbol string, positionSide domain.PositionSide) (*PositionResult, error)

	// GetActivePositions는 현재 활성화된 포지션 목록을 반환합니다
	GetActivePositions(ctx context.Context) ([]domain.Position, error)

	// IsEntryAvailable은 새 포지션 진입이 가능한지 확인합니다
	IsEntryAvailable(ctx context.Context, symbol string, signalType domain.SignalType) (bool, error)

	// CancelAllOrders는 특정 심볼의 모든 열린 주문을 취소합니다
	CancelAllOrders(ctx context.Context, symbol string) error
}

```
## internal/position/sizing.go
```go
package position

import (
	"fmt"
	"math"
)

// SizingConfig는 포지션 사이즈 계산을 위한 설정을 정의합니다
type SizingConfig struct {
	AccountBalance   float64 // 계정 총 잔고 (USDT)
	AvailableBalance float64 // 사용 가능한 잔고 (USDT)
	Leverage         int     // 사용할 레버리지
	MaxAllocation    float64 // 최대 할당 비율 (기본값: 0.9 = 90%)
	StepSize         float64 // 수량 최소 단위
	TickSize         float64 // 가격 최소 단위
	MinNotional      float64 // 최소 주문 가치
	MaintMarginRate  float64 // 유지증거금률
}

// PositionSizeResult는 포지션 계산 결과를 담는 구조체입니다
type PositionSizeResult struct {
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매 수량 (코인)
}

// CalculatePositionSize는 적절한 포지션 크기를 계산합니다
func CalculatePositionSize(price float64, config SizingConfig) (PositionSizeResult, error) {
	// 기본값 설정
	if config.MaxAllocation <= 0 {
		config.MaxAllocation = 0.9 // 기본값 90%
	}

	// 1. 사용 가능한 잔고에서 MaxAllocation만큼만 사용
	allocatedBalance := config.AccountBalance * config.MaxAllocation

	// 가용 잔고가 필요한 할당 금액보다 작은 경우 에러 반환
	if config.AvailableBalance < allocatedBalance {
		return PositionSizeResult{}, fmt.Errorf("가용 잔고가 부족합니다: 필요 %.2f USDT, 현재 %.2f USDT",
			allocatedBalance, config.AvailableBalance)
	}

	// 2. 레버리지 적용 및 수수료 고려
	totalFeeRate := 0.002 // 0.2% (진입 + 청산 수수료 + 여유분)
	effectiveMargin := config.MaintMarginRate + totalFeeRate

	// 안전하게 사용 가능한 최대 포지션 가치 계산
	maxSafePositionValue := (allocatedBalance * float64(config.Leverage)) / (1 + effectiveMargin)

	// 3. 최대 안전 수량 계산
	maxSafeQuantity := maxSafePositionValue / price

	// 4. 최소 주문 단위로 수량 조정
	// stepSize가 0.001이면 소수점 3자리
	precision := 0
	temp := config.StepSize
	for temp < 1.0 {
		temp *= 10
		precision++
	}

	// 소수점 자릿수에 맞춰 내림 계산
	scale := math.Pow(10, float64(precision))
	steps := math.Floor(maxSafeQuantity / config.StepSize)
	adjustedQuantity := steps * config.StepSize

	// 소수점 자릿수 정밀도 보장
	adjustedQuantity = math.Floor(adjustedQuantity*scale) / scale

	// 5. 최종 포지션 가치 계산
	finalPositionValue := adjustedQuantity * price

	// 최소 주문 가치 체크
	if finalPositionValue < config.MinNotional {
		return PositionSizeResult{}, fmt.Errorf("계산된 포지션 가치(%.2f)가 최소 주문 가치(%.2f)보다 작습니다",
			finalPositionValue, config.MinNotional)
	}

	// 소수점 2자리까지 내림 (USDT 기준)
	return PositionSizeResult{
		PositionValue: math.Floor(finalPositionValue*100) / 100,
		Quantity:      adjustedQuantity,
	}, nil
}

```
## internal/position/utils.go
```go
package position

import (
	"github.com/assist-by/phoenix/internal/domain"
)

// GetPositionSideFromSignal은 시그널 타입에 따른 포지션 사이드를 반환합니다
func GetPositionSideFromSignal(signalType domain.SignalType) domain.PositionSide {
	if signalType == domain.Long || signalType == domain.PendingLong {
		return domain.LongPosition
	}
	return domain.ShortPosition
}

// GetOrderSideForEntry는 포지션 진입을 위한 주문 사이드를 반환합니다
func GetOrderSideForEntry(positionSide domain.PositionSide) domain.OrderSide {
	if positionSide == domain.LongPosition {
		return domain.Buy
	}
	return domain.Sell
}

// GetOrderSideForExit는 포지션 청산을 위한 주문 사이드를 반환합니다
func GetOrderSideForExit(positionSide domain.PositionSide) domain.OrderSide {
	if positionSide == domain.LongPosition {
		return domain.Sell
	}
	return domain.Buy
}

```
## internal/scheduler/scheduler.go
```go
package scheduler

import (
	"context"
	"log"
	"time"
)

// Task는 스케줄러가 실행할 작업을 정의하는 인터페이스입니다
type Task interface {
	Execute(ctx context.Context) error
}

// Scheduler는 정해진 시간에 작업을 실행하는 스케줄러입니다
type Scheduler struct {
	interval time.Duration
	task     Task
	stopCh   chan struct{}
}

// NewScheduler는 새로운 스케줄러를 생성합니다
func NewScheduler(interval time.Duration, task Task) *Scheduler {
	return &Scheduler{
		interval: interval,
		task:     task,
		stopCh:   make(chan struct{}),
	}
}

// Start는 스케줄러를 시작합니다
// internal/scheduler/scheduler.go

func (s *Scheduler) Start(ctx context.Context) error {
	// 다음 실행 시간 계산
	now := time.Now()
	nextRun := now.Truncate(s.interval).Add(s.interval)
	waitDuration := nextRun.Sub(now)

	log.Printf("다음 실행까지 %v 대기 (다음 실행: %s)",
		waitDuration.Round(time.Second),
		nextRun.Format("15:04:05"))

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-s.stopCh:
			return nil

		case <-timer.C:
			// 작업 실행
			if err := s.task.Execute(ctx); err != nil {
				log.Printf("작업 실행 실패: %v", err)
				// 에러가 발생해도 계속 실행
			}

			// 다음 실행 시간 계산
			now := time.Now()
			nextRun = now.Truncate(s.interval).Add(s.interval)
			waitDuration = nextRun.Sub(now)

			log.Printf("다음 실행까지 %v 대기 (다음 실행: %s)",
				waitDuration.Round(time.Second),
				nextRun.Format("15:04:05"))

			// 타이머 리셋
			timer.Reset(waitDuration)
		}
	}
}

// Stop은 스케줄러를 중지합니다
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

```
## internal/strategy/macdsarema/init.go
```go
// internal/strategy/macdsarema/init.go
package macdsarema

// init 함수는 패키지가 로드될 때 자동으로 실행됩니다
func init() {
	// 전역 전략 레지스트리가 있다면 여기서 자동 등록할 수 있습니다
	// 그러나 일반적으로는 애플리케이션 시작 시 명시적으로 등록하는 것이 더 좋습니다
}

```
## internal/strategy/macdsarema/signal.go
```go
package macdsarema

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

type MACDSAREMASignal struct {
	domain.BaseSignal // 기본 시그널 필드와 메서드 상속

	// MACD+SAR+EMA 전략 특화 필드
	EMAValue    float64 // 200 EMA 값
	EMAAbove    bool    // 가격이 EMA 위에 있는지 여부
	MACDValue   float64 // MACD 라인 값
	SignalValue float64 // 시그널 라인 값
	Histogram   float64 // 히스토그램 값
	MACDCross   int     // MACD 크로스 상태 (1: 상향돌파, -1: 하향돌파, 0: 크로스 없음)
	SARValue    float64 // SAR 값
	SARBelow    bool    // SAR이 캔들 아래에 있는지 여부
}

// NewMACDSAREMASignal은 기본 필드로 새 MACDSAREMASignal을 생성합니다
func NewMACDSAREMASignal(
	signalType domain.SignalType,
	symbol string,
	price float64,
	timestamp time.Time,
	stopLoss float64,
	takeProfit float64,
) *MACDSAREMASignal {
	return &MACDSAREMASignal{
		BaseSignal: domain.NewBaseSignal(
			signalType,
			symbol,
			price,
			timestamp,
			stopLoss,
			takeProfit,
		),
	}
}

// CreateFromConditions은 전략 분석 시 생성된 조건 맵에서 MACDSAREMASignal을 생성합니다
func CreateFromConditions(
	signalType domain.SignalType,
	symbol string,
	price float64,
	timestamp time.Time,
	stopLoss float64,
	takeProfit float64,
	conditions map[string]interface{},
) *MACDSAREMASignal {
	macdSignal := NewMACDSAREMASignal(
		signalType,
		symbol,
		price,
		timestamp,
		stopLoss,
		takeProfit,
	)

	// conditions 맵에서 값 추출
	if val, ok := conditions["EMAValue"].(float64); ok {
		macdSignal.EMAValue = val
	}
	if val, ok := conditions["EMALong"].(bool); ok {
		macdSignal.EMAAbove = val
	}
	if val, ok := conditions["MACDValue"].(float64); ok {
		macdSignal.MACDValue = val
	}
	if val, ok := conditions["SignalValue"].(float64); ok {
		macdSignal.SignalValue = val
	}
	if val, ok := conditions["SARValue"].(float64); ok {
		macdSignal.SARValue = val
	}
	if val, ok := conditions["SARLong"].(bool); ok {
		macdSignal.SARBelow = val
	}

	// 히스토그램 계산
	macdSignal.Histogram = macdSignal.MACDValue - macdSignal.SignalValue

	// MACD 크로스 상태 결정
	if val, ok := conditions["MACDLong"].(bool); ok && val {
		macdSignal.MACDCross = 1 // 상향돌파
	} else if val, ok := conditions["MACDShort"].(bool); ok && val {
		macdSignal.MACDCross = -1 // 하향돌파
	} else {
		macdSignal.MACDCross = 0 // 크로스 없음
	}

	return macdSignal
}

// ToNotificationData는 MACD+SAR+EMA 전략에 특화된 알림 데이터를 반환합니다
func (s *MACDSAREMASignal) ToNotificationData() map[string]interface{} {
	data := s.BaseSignal.ToNotificationData() // 기본 필드 가져오기

	// 롱 조건 메시지
	longConditionValue := fmt.Sprintf(
		"%s EMA200 (가격이 EMA 위)\n%s MACD (시그널 상향돌파)\n%s SAR (SAR이 가격 아래)",
		getCheckMark(s.EMAAbove),
		getCheckMark(s.MACDCross > 0),
		getCheckMark(s.SARBelow),
	)

	// 숏 조건 메시지
	shortConditionValue := fmt.Sprintf(
		"%s EMA200 (가격이 EMA 아래)\n%s MACD (시그널 하향돌파)\n%s SAR (SAR이 가격 위)",
		getCheckMark(!s.EMAAbove),
		getCheckMark(s.MACDCross < 0),
		getCheckMark(!s.SARBelow),
	)

	// 기술적 지표 값 메시지 - 코드 블록으로 감싸기
	technicalValue := fmt.Sprintf(
		"```\n[EMA200]: %.5f\n[MACD Line]: %.5f\n[Signal Line]: %.5f\n[Histogram]: %.5f\n[SAR]: %.5f\n```",
		s.EMAValue,
		s.MACDValue,
		s.SignalValue,
		s.Histogram,
		s.SARValue,
	)

	// Discord의 embed 필드로 구성 - inline 속성 true로 설정
	fields := []map[string]interface{}{
		{
			"name":   "LONG 조건",
			"value":  longConditionValue,
			"inline": true, // 인라인으로 설정
		},
		{
			"name":   "SHORT 조건",
			"value":  shortConditionValue,
			"inline": true, // 인라인으로 설정
		},
		{
			"name":   "기술적 지표",
			"value":  technicalValue,
			"inline": false, // 이건 전체 폭 사용
		},
	}

	// 필드 배열을 데이터에 추가
	data["field"] = fields

	return data
}

// getCheckMark는 조건에 따라 체크마크나 X를 반환합니다
func getCheckMark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}

```
## internal/strategy/macdsarema/strategy.go
```go
package macdsarema

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// 심볼별 상태를 관리하기 위한 구조체
type SymbolState struct {
	PrevMACD      float64                // 이전 MACD 값
	PrevSignal    float64                // 이전 Signal 값
	PrevHistogram float64                // 이전 히스토그램 값
	LastSignal    domain.SignalInterface // 마지막 발생 시그널
}

// MACDSAREMAStrategy는 MACD + SAR + EMA 전략을 구현합니다
type MACDSAREMAStrategy struct {
	strategy.BaseStrategy
	emaIndicator  *indicator.EMA  // EMA 지표
	macdIndicator *indicator.MACD // MACD 지표
	sarIndicator  *indicator.SAR  // SAR 지표

	stopLossPct   float64 // 손절 비율
	takeProfitPct float64 // 익절 비율
	minHistogram  float64 // MACD 히스토그램 최소값 (기본값: 0.00005)

	states map[string]*SymbolState
	mu     sync.RWMutex
}

// NewStrategy는 새로운 MACD+SAR+EMA 전략 인스턴스를 생성합니다
func NewStrategy(config map[string]interface{}) (strategy.Strategy, error) {
	// 기본 설정값
	emaLength := 200
	stopLossPct := 0.02
	takeProfitPct := 0.04
	minHistogram := 0.005

	// 설정에서 값 로드
	if config != nil {
		if val, ok := config["emaLength"].(int); ok {
			emaLength = val
		}
		if val, ok := config["stopLossPct"].(float64); ok {
			stopLossPct = val
		}
		if val, ok := config["takeProfitPct"].(float64); ok {
			takeProfitPct = val
		}
		if val, ok := config["minHistogram"].(float64); ok {
			minHistogram = val
		}
	}

	// 필요한 지표 인스턴스 생성
	emaIndicator := indicator.NewEMA(emaLength)
	macdIndicator := indicator.NewMACD(12, 26, 9) // 기본 MACD 설정
	sarIndicator := indicator.NewDefaultSAR()     // 기본 SAR 설정

	s := &MACDSAREMAStrategy{
		BaseStrategy: strategy.BaseStrategy{
			Name:        "MACD+SAR+EMA",
			Description: "MACD, Parabolic SAR, 200 EMA를 조합한 트렌드 팔로잉 전략",
			Config:      config,
		},
		emaIndicator:  emaIndicator,
		macdIndicator: macdIndicator,
		sarIndicator:  sarIndicator,
		stopLossPct:   stopLossPct,
		takeProfitPct: takeProfitPct,
		minHistogram:  minHistogram,
		states:        make(map[string]*SymbolState),
	}

	return s, nil
}

// Initialize는 전략을 초기화합니다
func (s *MACDSAREMAStrategy) Initialize(ctx context.Context) error {
	// 필요한 초기화 작업 수행
	log.Printf("전략 초기화: %s", s.GetName())
	return nil
}

// Analyze는 주어진 캔들 데이터를 분석하여 매매 신호를 생성합니다
func (s *MACDSAREMAStrategy) Analyze(ctx context.Context, symbol string, candles domain.CandleList) (domain.SignalInterface, error) {
	// 데이터 검증
	emaLength := s.emaIndicator.Period
	if len(candles) < emaLength {
		return nil, fmt.Errorf("insufficient data: need at least %d candles", emaLength)
	}

	// 캔들 데이터를 지표 계산에 필요한 형식으로 변환
	prices := indicator.ConvertCandlesToPriceData(candles)

	// 심볼별 상태 가져오기
	state := s.getSymbolState(symbol)

	// 지표 계산
	emaResults, err := s.emaIndicator.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("calculating EMA: %w", err)
	}

	macdResults, err := s.macdIndicator.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("calculating MACD: %w", err)
	}

	sarResults, err := s.sarIndicator.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("calculating SAR: %w", err)
	}

	// 마지막 캔들 정보
	lastCandle := prices[len(prices)-1]
	currentPrice := lastCandle.Close

	// 필요한 지표 값 추출
	lastEMA := emaResults[len(emaResults)-1].(indicator.EMAResult)
	currentEMA := lastEMA.Value

	var currentMACD, currentSignal, currentHistogram float64
	// MACD 결과에서 마지막 유효한 값 찾기
	for i := len(macdResults) - 1; i >= 0; i-- {
		if macdResults[i] != nil {
			macdResult := macdResults[i].(indicator.MACDResult)
			currentMACD = macdResult.MACD
			currentSignal = macdResult.Signal
			currentHistogram = macdResult.Histogram
			break
		}
	}

	lastSAR := sarResults[len(sarResults)-1].(indicator.SARResult)
	currentSAR := lastSAR.SAR

	// 현재 캔들 고가와 저가
	currentHigh := lastCandle.High
	currentLow := lastCandle.Low

	// EMA 및 SAR 조건 확인
	isAboveEMA := currentPrice > currentEMA
	sarBelowCandle := currentSAR < currentLow
	sarAboveCandle := currentSAR > currentHigh

	// MACD 크로스 확인
	macdCross := s.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 시그널 객체 초기화
	signalType := domain.NoSignal
	var stopLoss, takeProfit float64

	// 조건 맵 생성 (기존과 동일)
	conditions := map[string]interface{}{
		"EMALong":     isAboveEMA,
		"EMAShort":    !isAboveEMA,
		"MACDLong":    macdCross == 1,
		"MACDShort":   macdCross == -1,
		"SARLong":     sarBelowCandle,
		"SARShort":    !sarBelowCandle,
		"EMAValue":    currentEMA,
		"MACDValue":   currentMACD,
		"SignalValue": currentSignal,
		"SARValue":    currentSAR,
	}

	// 1. 일반 시그널 조건 확인
	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= s.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signalType = domain.Long
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice + (currentPrice - stopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f (시간: %s)",
			symbol, currentPrice, currentEMA, currentSAR,
			lastCandle.Time.Format("2006-01-02 15:04:05"))
	}

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= s.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signalType = domain.Short
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice - (stopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f (시간: %s)",
			symbol, currentPrice, currentEMA, currentSAR,
			lastCandle.Time.Format("2006-01-02 15:04:05"))
	}

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal
	state.PrevHistogram = currentHistogram

	macdSignal := &MACDSAREMASignal{
		BaseSignal: domain.NewBaseSignal(
			signalType,
			symbol,
			currentPrice,
			lastCandle.Time,
			stopLoss,
			takeProfit,
		),
		EMAValue:    currentEMA,
		EMAAbove:    isAboveEMA,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		Histogram:   currentHistogram,
		MACDCross:   macdCross,
		SARValue:    currentSAR,
		SARBelow:    sarBelowCandle,
	}

	// 조건 정보 설정
	for k, v := range conditions {
		macdSignal.SetCondition(k, v)
	}

	// 시그널이 생성되었으면 상태에 저장
	if signalType != domain.NoSignal {
		state.LastSignal = macdSignal
	}

	return macdSignal, nil
}

// getSymbolState는 심볼별 상태를 가져옵니다
func (s *MACDSAREMAStrategy) getSymbolState(symbol string) *SymbolState {
	s.mu.RLock()
	state, exists := s.states[symbol]
	s.mu.RUnlock()

	if !exists {
		s.mu.Lock()
		state = &SymbolState{}
		s.states[symbol] = state
		s.mu.Unlock()
	}

	return state
}

// checkMACDCross는 MACD 크로스를 확인합니다
// 반환값: 1 (상향돌파), -1 (하향돌파), 0 (크로스 없음)
func (s *MACDSAREMAStrategy) checkMACDCross(currentMACD, currentSignal, prevMACD, prevSignal float64) int {
	if prevMACD <= prevSignal && currentMACD > currentSignal {
		return 1 // 상향돌파
	}
	if prevMACD >= prevSignal && currentMACD < currentSignal {
		return -1 // 하향돌파
	}
	return 0 // 크로스 없음
}

// CalculateTPSL은 현재 SAR 값을 기반으로 TP/SL 가격을 계산합니다
func (s *MACDSAREMAStrategy) CalculateTPSL(
	ctx context.Context,
	symbol string,
	entryPrice float64,
	signalType domain.SignalType,
	currentSAR float64, // SAR 값을 파라미터로 받음
	symbolInfo *domain.SymbolInfo, // 심볼 정보도 파라미터로 받음
) (stopLoss, takeProfit float64) {
	isLong := signalType == domain.Long || signalType == domain.PendingLong

	// SAR 기반 손절가 및 1:1 비율 익절가 계산
	if isLong {
		stopLoss = domain.AdjustPrice(currentSAR, symbolInfo.TickSize, symbolInfo.PricePrecision)
		// 1:1 비율로 익절가 설정
		tpDistance := entryPrice - stopLoss
		takeProfit = domain.AdjustPrice(entryPrice+tpDistance, symbolInfo.TickSize, symbolInfo.PricePrecision)
	} else {
		stopLoss = domain.AdjustPrice(currentSAR, symbolInfo.TickSize, symbolInfo.PricePrecision)
		// 1:1 비율로 익절가 설정
		tpDistance := stopLoss - entryPrice
		takeProfit = domain.AdjustPrice(entryPrice-tpDistance, symbolInfo.TickSize, symbolInfo.PricePrecision)
	}

	return stopLoss, takeProfit
}

// RegisterStrategy는 이 전략을 레지스트리에 등록합니다
func RegisterStrategy(registry *strategy.Registry) {
	registry.Register("MACD+SAR+EMA", NewStrategy)
}

```
## internal/strategy/strategy.go
```go
package strategy

import (
	"context"
	"fmt"

	"github.com/assist-by/phoenix/internal/domain"
)

// Strategy는 트레이딩 전략의 인터페이스를 정의합니다
type Strategy interface {
	// Initialize는 전략을 초기화합니다
	Initialize(ctx context.Context) error

	// Analyze는 주어진 데이터를 분석하여 매매 신호를 생성합니다
	Analyze(ctx context.Context, symbol string, candles domain.CandleList) (domain.SignalInterface, error)

	// GetName은 전략의 이름을 반환합니다
	GetName() string

	// GetDescription은 전략의 설명을 반환합니다
	GetDescription() string

	// GetConfig는 전략의 현재 설정을 반환합니다
	GetConfig() map[string]interface{}

	// UpdateConfig는 전략 설정을 업데이트합니다
	UpdateConfig(config map[string]interface{}) error

	// CalculateTPSL은 주어진 진입가와 시그널에 기반하여 TP/SL 가격을 계산합니다
	CalculateTPSL(ctx context.Context, symbol string, entryPrice float64, signalType domain.SignalType, currentSAR float64, symbolInfo *domain.SymbolInfo) (stopLoss, takeProfit float64)
}

// BaseStrategy는 모든 전략 구현체에서 공통적으로 사용할 수 있는 기본 구현을 제공합니다
type BaseStrategy struct {
	Name        string
	Description string
	Config      map[string]interface{}
}

// GetName은 전략의 이름을 반환합니다
func (b *BaseStrategy) GetName() string {
	return b.Name
}

// GetDescription은 전략의 설명을 반환합니다
func (b *BaseStrategy) GetDescription() string {
	return b.Description
}

// GetConfig는 전략의 현재 설정을 반환합니다
func (b *BaseStrategy) GetConfig() map[string]interface{} {
	// 설정의 복사본 반환
	configCopy := make(map[string]interface{})
	for k, v := range b.Config {
		configCopy[k] = v
	}
	return configCopy
}

// UpdateConfig는 전략 설정을 업데이트합니다
func (b *BaseStrategy) UpdateConfig(config map[string]interface{}) error {
	// 설정 업데이트
	for k, v := range config {
		b.Config[k] = v
	}
	return nil
}

// BaseStrategy에 기본 구현 추가
func (b *BaseStrategy) CalculateTPSL(ctx context.Context, symbol string, entryPrice float64, signalType domain.SignalType, currentSAR float64, symbolInfo *domain.SymbolInfo) (stopLoss, takeProfit float64) {
	// 하위 클래스에서 구현해야 함
	return 0, 0
}

// Factory는 전략 인스턴스를 생성하는 함수 타입입니다
type Factory func(config map[string]interface{}) (Strategy, error)

// Registry는 사용 가능한 모든 전략을 등록하고 관리합니다
type Registry struct {
	strategies map[string]Factory
}

// NewRegistry는 새로운 전략 레지스트리를 생성합니다
func NewRegistry() *Registry {
	return &Registry{
		strategies: make(map[string]Factory),
	}
}

// Register는 새로운 전략 팩토리를 레지스트리에 등록합니다
func (r *Registry) Register(name string, factory Factory) {
	r.strategies[name] = factory
}

// Create는 주어진 이름과 설정으로 전략 인스턴스를 생성합니다
func (r *Registry) Create(name string, config map[string]interface{}) (Strategy, error) {
	factory, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("존재하지 않는 전략: %s", name)
	}
	return factory(config)
}

// ListStrategies는 사용 가능한 모든 전략 이름을 반환합니다
func (r *Registry) ListStrategies() []string {
	var names []string
	for name := range r.strategies {
		names = append(names, name)
	}
	return names
}

```
