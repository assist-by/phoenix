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
        └── types.go
    ├── config/
        └── config.go
    ├── domain/
        ├── account.go
        ├── candle.go
        ├── order.go
        ├── signal.go
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
		// 플래그가 설정되었으면 .env 설정보다 우선
		symbol := cfg.Backtest.Symbol
		days := cfg.Backtest.Days
		interval := domain.TimeInterval(cfg.Backtest.Interval)

		runBacktest(ctx, symbol, days, interval, discordClient, binanceClient, tradingStrategy)
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

func runBacktest(
	ctx context.Context,
	symbol string,
	days int,
	interval domain.TimeInterval,
	discordClient *discord.Client,
	binanceClient *eBinance.Client,
	strategy strategy.Strategy,
) {
	log.Printf("'%s' 심볼에 대해 %d일 동안의 백테스트를 %s 간격으로 시작합니다...", symbol, days, interval)

	// 필요한 캔들 개수 계산 (일별 캔들 수 * 일수 + 여유분)
	candlesPerDay := 24 * 60 / domain.TimeIntervalToDuration(interval).Minutes()
	requiredCandles := int(candlesPerDay*float64(days)) + 200 // 지표 계산을 위한 여유분

	// 데이터 로드
	log.Printf("바이낸스에서 %d개의 캔들 데이터를 로드합니다...", requiredCandles)
	candles, err := binanceClient.GetKlines(ctx, symbol, interval, requiredCandles)
	if err != nil {
		log.Fatalf("캔들 데이터 로드 실패: %v", err)
	}
	log.Printf("%d개의 캔들 데이터를 성공적으로 로드했습니다.", len(candles))

	// 백테스트 엔진 초기화 및 실행 (다음 단계에서 구현)
	log.Printf("백테스트 엔진을 초기화하는 중...")
	// 여기에 백테스트 엔진 초기화 및 실행 코드 추가 예정

	// 임시 결과 표시
	log.Printf("백테스트가 완료되었습니다. 자세한 결과는 향후 구현 예정입니다.")

	// 결과를 Discord로 알림 (옵션)
	if discordClient != nil {
		discordClient.SendInfo(fmt.Sprintf("✅ %s 심볼에 대한 백테스트가 완료되었습니다. 자세한 결과는 로그를 확인하세요.", symbol))
	}
}

```
## internal/backtest/engine.go
```go
package backtest

import (
	"context"
	"log"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/strategy"
)

// Engine은 백테스트 실행을 담당하는 구조체입니다
type Engine struct {
	Symbol    string
	Interval  domain.TimeInterval
	Candles   domain.CandleList
	Strategy  strategy.Strategy
	StartTime time.Time
	EndTime   time.Time
	cache     *IndicatorCache
}

// NewEngine은 새로운 백테스트 엔진을 생성합니다
func NewEngine(symbol string, interval domain.TimeInterval, candles domain.CandleList, strat strategy.Strategy) *Engine {
	startTime := candles[0].OpenTime
	endTime := candles[len(candles)-1].CloseTime

	return &Engine{
		Symbol:    symbol,
		Interval:  interval,
		Candles:   candles,
		Strategy:  strat,
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// Run은 백테스트를 실행합니다
func (e *Engine) Run(ctx context.Context) (*Result, error) {
	log.Printf("백테스트 실행: %s (%s - %s)", e.Symbol,
		e.StartTime.Format("2006-01-02"),
		e.EndTime.Format("2006-01-02"))

	// 지표 캐싱 - 2단계에서 구현 예정
	e.cacheIndicators()

	// 백테스트 결과 준비
	result := &Result{
		Symbol:    e.Symbol,
		Interval:  e.Interval,
		StartTime: e.StartTime,
		EndTime:   e.EndTime,
		Trades:    make([]Trade, 0),
	}

	// 여기에 백테스트 루프 로직 추가 예정 (3, 4단계에서 구현)

	// 임시 결과 반환
	return result, nil
}

// cacheIndicators는 백테스트에 필요한 모든 지표를 미리 계산합니다
func (e *Engine) cacheIndicators() {
	log.Printf("지표 계산 및 캐싱 중...")
	// 2단계에서 구현 예정
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
		Symbol   string `envconfig:"BACKTEST_SYMBOL" default:"BTCUSDT"`
		Days     int    `envconfig:"BACKTEST_DAYS" default:"30"`
		Interval string `envconfig:"BACKTEST_INTERVAL" default:"15m"`
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

// GetKlines는 캔들 데이터를 조회합니다
func (c *Client) GetKlines(ctx context.Context, symbol string, interval domain.TimeInterval, limit int) (domain.CandleList, error) {
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
	"time"
)

// EMAResult는 EMA 지표 계산 결과입니다
type EMAResult struct {
	Value     float64
	Timestamp time.Time
}

// GetTimestamp는 결과의 타임스탬프를 반환합니다 (Result 인터페이스 구현)
func (r EMAResult) GetTimestamp() time.Time {
	return r.Timestamp
}

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

	period := e.Period
	// EMA 계산을 위한 승수 계산
	multiplier := 2.0 / float64(period+1)
	results := make([]Result, len(prices))

	// 초기 SMA 계산
	var sma float64
	for i := 0; i < period; i++ {
		sma += prices[i].Close
	}
	sma /= float64(period)

	// 첫 번째 EMA는 SMA 값으로 설정
	results[period-1] = EMAResult{
		Value:     sma,
		Timestamp: prices[period-1].Time,
	}

	// EMA 계산: EMA = 이전 EMA + (현재가 - 이전 EMA) × 승수
	for i := period; i < len(prices); i++ {
		// 이전 결과가 nil인지 확인
		if results[i-1] == nil {
			continue // nil이면 이 단계 건너뜀
		}

		// 안전한 타입 변환
		prevResult, ok := results[i-1].(EMAResult)
		if !ok {
			return nil, fmt.Errorf("EMA 결과 타입 변환 실패 (인덱스: %d)", i-1)
		}

		prevEMA := prevResult.Value
		ema := (prices[i].Close-prevEMA)*multiplier + prevEMA
		results[i] = EMAResult{
			Value:     ema,
			Timestamp: prices[i].Time,
		}
	}

	return results, nil
}

// validateInput은 입력 데이터가 유효한지 검증합니다
func (e *EMA) validateInput(prices []PriceData) error {
	if len(prices) == 0 {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 비어있습니다"),
		}
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
	"time"
)

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

	// 단기 EMA 계산
	shortEMA := NewEMA(m.ShortPeriod)
	shortEMAResults, err := shortEMA.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("단기 EMA 계산 실패: %w", err)
	}

	// 장기 EMA 계산
	longEMA := NewEMA(m.LongPeriod)
	longEMAResults, err := longEMA.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("장기 EMA 계산 실패: %w", err)
	}

	// MACD 라인 계산 (단기 EMA - 장기 EMA)
	macdStartIdx := m.LongPeriod - 1
	macdLine := make([]PriceData, len(prices)-macdStartIdx)

	// 이 부분에 타입 안전성 확보를 위한 체크 추가
	for i := range macdLine {
		idx := i + macdStartIdx
		// nil 체크 추가
		if shortEMAResults[idx] == nil || longEMAResults[idx] == nil {
			continue // nil인 경우 건너뛰기
		}

		// 안전한 타입 변환
		shortVal, ok1 := shortEMAResults[idx].(EMAResult)
		longVal, ok2 := longEMAResults[idx].(EMAResult)

		if !ok1 || !ok2 {
			return nil, fmt.Errorf("EMA 결과 타입 변환 실패 (인덱스: %d)", idx)
		}

		macdLine[i] = PriceData{
			Time:  prices[idx].Time,
			Close: shortVal.Value - longVal.Value,
		}
	}

	// 시그널 라인 계산 (MACD의 EMA)
	signalEMA := NewEMA(m.SignalPeriod)
	signalEMAResults, err := signalEMA.Calculate(macdLine)
	if err != nil {
		return nil, fmt.Errorf("시그널 라인 계산 실패: %w", err)
	}

	// 최종 결과 생성
	resultStartIdx := m.SignalPeriod - 1
	results := make([]Result, len(prices))

	// 결과가 없는 초기 인덱스는 nil로 설정
	for i := 0; i < macdStartIdx+resultStartIdx; i++ {
		results[i] = nil
	}

	// 실제 MACD 결과 설정
	for i := 0; i < len(macdLine)-resultStartIdx; i++ {
		resultIdx := i + macdStartIdx + resultStartIdx

		// nil 체크 추가
		if i+resultStartIdx >= len(macdLine) || i >= len(signalEMAResults) || signalEMAResults[i] == nil {
			results[resultIdx] = nil
			continue
		}

		macdValue := macdLine[i+resultStartIdx].Close

		// 안전한 타입 변환
		signalEMAResult, ok := signalEMAResults[i].(EMAResult)
		if !ok {
			results[resultIdx] = nil
			continue
		}

		signalValue := signalEMAResult.Value

		results[resultIdx] = MACDResult{
			MACD:      macdValue,
			Signal:    signalValue,
			Histogram: macdValue - signalValue,
			Timestamp: macdLine[i+resultStartIdx].Time,
		}
	}

	return results, nil
}

// validateInput은 입력 데이터가 유효한지 검증합니다
func (m *MACD) validateInput(prices []PriceData) error {
	if len(prices) == 0 {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 비어있습니다"),
		}
	}

	minPeriod := m.LongPeriod + m.SignalPeriod - 1
	if len(prices) < minPeriod {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d, 현재: %d", minPeriod, len(prices)),
		}
	}

	return nil
}

```
## internal/indicator/sar.go
```go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// SARResult는 Parabolic SAR 지표 계산 결과입니다
type SARResult struct {
	SAR       float64   // SAR 값
	IsLong    bool      // 현재 추세가 상승인지 여부
	Timestamp time.Time // 계산 시점
}

// GetTimestamp는 결과의 타임스탬프를 반환합니다 (Result 인터페이스 구현)
func (r SARResult) GetTimestamp() time.Time {
	return r.Timestamp
}

// SAR은 Parabolic SAR 지표를 구현합니다
type SAR struct {
	BaseIndicator
	AccelerationInitial float64 // 초기 가속도
	AccelerationMax     float64 // 최대 가속도
}

// NewSAR는 새로운 Parabolic SAR 지표 인스턴스를 생성합니다
func NewSAR(accelerationInitial, accelerationMax float64) *SAR {
	return &SAR{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("SAR(%.2f,%.2f)", accelerationInitial, accelerationMax),
			Config: map[string]interface{}{
				"AccelerationInitial": accelerationInitial,
				"AccelerationMax":     accelerationMax,
			},
		},
		AccelerationInitial: accelerationInitial,
		AccelerationMax:     accelerationMax,
	}
}

// NewDefaultSAR는 기본 설정으로 SAR 인스턴스를 생성합니다
func NewDefaultSAR() *SAR {
	return NewSAR(0.02, 0.2)
}

// Calculate는 주어진 가격 데이터에 대해 Parabolic SAR을 계산합니다
func (s *SAR) Calculate(prices []PriceData) ([]Result, error) {
	if err := s.validateInput(prices); err != nil {
		return nil, err
	}

	results := make([]Result, len(prices))
	// 초기값 설정
	af := s.AccelerationInitial
	sar := prices[0].Low
	ep := prices[0].High
	isLong := true

	results[0] = SARResult{
		SAR:       sar,
		IsLong:    isLong,
		Timestamp: prices[0].Time,
	}

	// SAR 계산
	for i := 1; i < len(prices); i++ {
		if isLong {
			sar = sar + af*(ep-sar)

			// 새로운 고점 발견
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// 추세 전환 체크
			if sar > prices[i].Low {
				isLong = false
				sar = ep
				ep = prices[i].Low
				af = s.AccelerationInitial
			}
		} else {
			sar = sar - af*(sar-ep)

			// 새로운 저점 발견
			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// 추세 전환 체크
			if sar < prices[i].High {
				isLong = true
				sar = ep
				ep = prices[i].High
				af = s.AccelerationInitial
			}
		}

		results[i] = SARResult{
			SAR:       sar,
			IsLong:    isLong,
			Timestamp: prices[i].Time,
		}
	}

	return results, nil
}

// validateInput은 입력 데이터가 유효한지 검증합니다
func (s *SAR) validateInput(prices []PriceData) error {
	if len(prices) < 2 {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("SAR 계산에는 최소 2개의 가격 데이터가 필요합니다"),
		}
	}

	return nil
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

			// 반대 방향의 포지션이 있으면 청산 필요
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
						m.notifier.SendInfo(fmt.Sprintf("✅ %s 포지션이 성공적으로 청산되었습니다.", symbol))
					}
					return true, nil
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
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// 심볼별 상태를 관리하기 위한 구조체
type SymbolState struct {
	PrevMACD       float64                // 이전 MACD 값
	PrevSignal     float64                // 이전 Signal 값
	PrevHistogram  float64                // 이전 히스토그램 값
	LastSignal     domain.SignalInterface // 마지막 발생 시그널
	PendingSignal  domain.SignalType      // 대기중인 시그널 타입
	WaitedCandles  int                    // 대기한 캔들 수
	MaxWaitCandles int                    // 최대 대기 캔들 수
}

// MACDSAREMAStrategy는 MACD + SAR + EMA 전략을 구현합니다
type MACDSAREMAStrategy struct {
	strategy.BaseStrategy
	emaIndicator  *indicator.EMA  // EMA 지표
	macdIndicator *indicator.MACD // MACD 지표
	sarIndicator  *indicator.SAR  // SAR 지표

	stopLossPct    float64 // 손절 비율
	takeProfitPct  float64 // 익절 비율
	minHistogram   float64 // MACD 히스토그램 최소값 (기본값: 0.00005)
	maxWaitCandles int     // 최대 대기 캔들 수 (기본값: 5)

	states map[string]*SymbolState
	mu     sync.RWMutex
}

// NewStrategy는 새로운 MACD+SAR+EMA 전략 인스턴스를 생성합니다
func NewStrategy(config map[string]interface{}) (strategy.Strategy, error) {
	// 기본 설정값
	emaLength := 200
	stopLossPct := 0.02
	takeProfitPct := 0.04
	minHistogram := 0.00005
	maxWaitCandles := 5

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
		if val, ok := config["maxWaitCandles"].(int); ok {
			maxWaitCandles = val
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
		emaIndicator:   emaIndicator,
		macdIndicator:  macdIndicator,
		sarIndicator:   sarIndicator,
		stopLossPct:    stopLossPct,
		takeProfitPct:  takeProfitPct,
		minHistogram:   minHistogram,
		maxWaitCandles: maxWaitCandles,
		states:         make(map[string]*SymbolState),
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

	// 1. 대기 상태 확인 및 업데이트
	if state.PendingSignal != domain.NoSignal {
		pendingSignal := s.processPendingState(state, symbol, conditions, currentPrice, currentHistogram, sarBelowCandle, sarAboveCandle, currentSAR)
		if pendingSignal != nil {
			// 상태 업데이트
			state.PrevMACD = currentMACD
			state.PrevSignal = currentSignal
			state.PrevHistogram = currentHistogram
			state.LastSignal = pendingSignal
			return pendingSignal, nil
		}
	}

	// 2. 일반 시그널 조건 확인
	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= s.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signalType = domain.Long
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice + (currentPrice - stopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= s.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signalType = domain.Short
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice - (stopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// 3. 새로운 대기 상태 설정 (일반 시그널이 아닌 경우)
	if signalType == domain.NoSignal {
		// MACD 상향돌파 + EMA 위 + SAR 캔들 아래가 아닌 경우 -> 롱 대기 상태
		if isAboveEMA && macdCross == 1 && !sarBelowCandle && currentHistogram > 0 {
			state.PendingSignal = domain.PendingLong
			state.WaitedCandles = 0
			log.Printf("[%s] Long 대기 상태 시작: MACD 상향돌파, SAR 반전 대기", symbol)
		}

		// MACD 하향돌파 + EMA 아래 + SAR이 캔들 위가 아닌 경우 → 숏 대기 상태
		if !isAboveEMA && macdCross == -1 && !sarAboveCandle && currentHistogram < 0 {
			state.PendingSignal = domain.PendingShort
			state.WaitedCandles = 0
			log.Printf("[%s] Short 대기 상태 시작: MACD 하향돌파, SAR 반전 대기", symbol)
		}
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
		state = &SymbolState{
			PendingSignal:  domain.NoSignal,
			WaitedCandles:  0,
			MaxWaitCandles: s.maxWaitCandles,
		}
		s.states[symbol] = state
		s.mu.Unlock()
	}

	return state
}

// resetPendingState는 심볼의 대기 상태를 초기화합니다
func (s *MACDSAREMAStrategy) resetPendingState(state *SymbolState) {
	state.PendingSignal = domain.NoSignal
	state.WaitedCandles = 0
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

// processPendingState는 대기 상태를 처리하고 시그널을 생성합니다
func (s *MACDSAREMAStrategy) processPendingState(
	state *SymbolState,
	symbol string,
	conditions map[string]interface{},
	currentPrice float64,
	currentHistogram float64,
	sarBelowCandle bool,
	sarAboveCandle bool,
	currentSAR float64,
) domain.SignalInterface {
	// 캔들 카운트 증가
	state.WaitedCandles++

	// 최대 대기 시간 초과 체크
	if state.WaitedCandles > state.MaxWaitCandles {
		log.Printf("[%s] 대기 상태 취소: 최대 대기 캔들 수 (%d) 초과", symbol, state.MaxWaitCandles)
		s.resetPendingState(state)
		return nil
	}

	var resultSignal domain.SignalInterface = nil
	var stopLoss, takeProfit float64
	var resultType domain.SignalType = domain.NoSignal

	// Long 대기 상태 처리
	if state.PendingSignal == domain.PendingLong {
		// 히스토그램이 음수로 바뀌면 취소(추세 역전)
		if currentHistogram < 0 && state.PrevHistogram > 0 {
			log.Printf("[%s] Long 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			s.resetPendingState(state)
			return nil
		}

		// SAR가 캔들 아래로 이동하면 롱 시그널 생성
		if sarBelowCandle {
			resultType = domain.Long
			stopLoss = currentSAR
			takeProfit = currentPrice + (currentPrice - stopLoss)

			log.Printf("[%s] Long 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			s.resetPendingState(state)
			// return resultSignal
		}
	}

	// Short 대기 상태 처리
	if state.PendingSignal == domain.PendingShort {
		// 히스토그램이 양수로 바뀌면 취소 (추세 역전)
		if currentHistogram > 0 && state.PrevHistogram < 0 {
			log.Printf("[%s] Short 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			s.resetPendingState(state)
			return nil
		}

		// SAR이 캔들 위로 이동하면 숏 시그널 생성
		if sarAboveCandle {
			resultType = domain.Short
			stopLoss = currentSAR
			takeProfit = currentPrice - (stopLoss - currentPrice)

			log.Printf("[%s] Short 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			s.resetPendingState(state)
			// return resultSignal
		}
	}

	// 최종 시그널 생성
	if resultType != domain.NoSignal {
		macdSignal := &MACDSAREMASignal{
			BaseSignal: domain.NewBaseSignal(
				resultType,
				symbol,
				currentPrice,
				time.Now(), // or use a proper timestamp
				stopLoss,
				takeProfit,
			),
			// 특화 필드 설정
			EMAValue:    conditions["EMAValue"].(float64),
			EMAAbove:    conditions["EMALong"].(bool),
			MACDValue:   conditions["MACDValue"].(float64),
			SignalValue: conditions["SignalValue"].(float64),
			SARValue:    conditions["SARValue"].(float64),
			SARBelow:    conditions["SARLong"].(bool),
			MACDCross:   getMACDCrossValue(conditions),
			Histogram:   conditions["MACDValue"].(float64) - conditions["SignalValue"].(float64),
		}

		// 조건 정보 설정
		for k, v := range conditions {
			macdSignal.SetCondition(k, v)
		}

		resultSignal = macdSignal
	}

	return resultSignal
}

func getMACDCrossValue(conditions map[string]interface{}) int {
	if conditions["MACDLong"].(bool) {
		return 1 // 상향돌파
	} else if conditions["MACDShort"].(bool) {
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
