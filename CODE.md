# phoenix
## Project Structure

```
phoenix/
├── cmd/
    └── trader/
    │   └── main.go
└── internal/
    ├── analysis/
        ├── indicator/
        │   ├── ema.go
        │   ├── indicator_test.go
        │   ├── macd.go
        │   ├── sar.go
        │   └── types.go
        └── signal/
        │   ├── detector.go
        │   ├── signal_test.go
        │   ├── state.go
        │   └── types.go
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
    ├── market/
        ├── client.go
        ├── collector.go
        └── types.go
    ├── notification/
        ├── discord/
        │   ├── client.go
        │   ├── embed.go
        │   ├── signal.go
        │   └── webhook.go
        └── types.go
    └── scheduler/
        └── scheduler.go
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

	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/exchange/binance"
	"github.com/assist-by/phoenix/internal/market"
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
	binanceClient := binance.NewClient(
		apiKey,
		secretKey,
		binance.WithTimeout(10*time.Second),
		binance.WithTestnet(cfg.Binance.UseTestnet),
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

	// 테스트 모드 실행 (플래그 기반)
	if *testLongFlag || *testShortFlag {
		testType := "Long"
		signalType := signal.Long

		if *testShortFlag {
			testType = "Short"
			signalType = signal.Short
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
		var testSignal *signal.Signal

		if signalType == signal.Long {
			testSignal = &signal.Signal{
				Type:       signal.Long,
				Symbol:     symbol,
				Price:      currentPrice,
				Timestamp:  time.Now(),
				StopLoss:   currentPrice * 0.99, // 가격의 99% (1% 손절)
				TakeProfit: currentPrice * 1.01, // 가격의 101% (1% 익절)
			}
		} else {
			testSignal = &signal.Signal{
				Type:       signal.Short,
				Symbol:     symbol,
				Price:      currentPrice,
				Timestamp:  time.Now(),
				StopLoss:   currentPrice * 1.01, // 가격의 101% (1% 손절)
				TakeProfit: currentPrice * 0.99, // 가격의 99% (1% 익절)
			}
		}

		// 시그널 알림 전송
		if err := discordClient.SendSignal(testSignal); err != nil {
			log.Printf("시그널 알림 전송 실패: %v", err)
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

```
## internal/analysis/indicator/ema.go
```go
package indicator

import "fmt"

// EMAOption은 EMA 계산에 필요한 옵션을 정의합니다
type EMAOption struct {
	Period int // 기간
}

// ValidateEMAOption은 EMA 옵션을 검증합니다
func ValidateEMAOption(opt EMAOption) error {
	if opt.Period < 1 {
		return &ValidationError{
			Field: "Period",
			Err:   fmt.Errorf("기간은 1 이상이어야 합니다: %d", opt.Period),
		}
	}
	return nil
}

// EMA는 지수이동평균을 계산합니다
func EMA(prices []PriceData, opt EMAOption) ([]Result, error) {
	if err := ValidateEMAOption(opt); err != nil {
		return nil, err
	}

	if len(prices) == 0 {
		return nil, &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 비어있습니다"),
		}
	}

	if len(prices) < opt.Period {
		return nil, &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d, 현재: %d", opt.Period, len(prices)),
		}
	}

	// EMA 계산을 위한 승수 계산
	multiplier := 2.0 / float64(opt.Period+1)

	results := make([]Result, len(prices))

	// 초기 SMA 계산
	var sma float64
	for i := 0; i < opt.Period; i++ {
		sma += prices[i].Close
	}
	sma /= float64(opt.Period)

	// 첫 번째 EMA는 SMA 값으로 설정
	results[opt.Period-1] = Result{
		Value:     sma,
		Timestamp: prices[opt.Period-1].Time,
	}

	// EMA 계산
	// EMA = 이전 EMA + (현재가 - 이전 EMA) × 승수
	for i := opt.Period; i < len(prices); i++ {
		ema := (prices[i].Close-results[i-1].Value)*multiplier + results[i-1].Value
		results[i] = Result{
			Value:     ema,
			Timestamp: prices[i].Time,
		}
	}

	return results, nil
}

```
## internal/analysis/indicator/indicator_test.go
```go
package indicator

import (
	"fmt"
	"testing"
	"time"
)

// 테스트용 가격 데이터 생성
func generateTestPrices() []PriceData {
	// 2024년 1월 1일부터 시작하는 50일간의 가격 데이터
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := []PriceData{
		// 상승 구간 (변동성 있는)
		{Time: baseTime, Open: 100, High: 105, Low: 98, Close: 103, Volume: 1000},
		{Time: baseTime.AddDate(0, 0, 1), Open: 103, High: 108, Low: 102, Close: 106, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 2), Open: 106, High: 110, Low: 104, Close: 108, Volume: 1100},
		{Time: baseTime.AddDate(0, 0, 3), Open: 108, High: 112, Low: 107, Close: 110, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 4), Open: 110, High: 115, Low: 109, Close: 113, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 5), Open: 113, High: 116, Low: 111, Close: 114, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 6), Open: 114, High: 118, Low: 113, Close: 116, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 7), Open: 116, High: 119, Low: 115, Close: 117, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 8), Open: 117, High: 120, Low: 114, Close: 119, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 9), Open: 119, High: 122, Low: 118, Close: 120, Volume: 1600},

		// 하락 구간 (급격한 하락)
		{Time: baseTime.AddDate(0, 0, 10), Open: 120, High: 121, Low: 115, Close: 116, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 11), Open: 116, High: 117, Low: 112, Close: 113, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 12), Open: 113, High: 115, Low: 108, Close: 109, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 13), Open: 109, High: 110, Low: 105, Close: 106, Volume: 2400},
		{Time: baseTime.AddDate(0, 0, 14), Open: 106, High: 107, Low: 102, Close: 103, Volume: 2600},

		// 횡보 구간 (변동성 있는)
		{Time: baseTime.AddDate(0, 0, 15), Open: 103, High: 107, Low: 102, Close: 105, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 16), Open: 105, High: 108, Low: 103, Close: 104, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 17), Open: 104, High: 106, Low: 102, Close: 106, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 18), Open: 106, High: 108, Low: 104, Close: 105, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 19), Open: 105, High: 107, Low: 103, Close: 104, Volume: 1600},

		// 추가 하락 구간
		{Time: baseTime.AddDate(0, 0, 20), Open: 104, High: 105, Low: 100, Close: 101, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 21), Open: 101, High: 102, Low: 98, Close: 99, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 22), Open: 99, High: 100, Low: 96, Close: 97, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 23), Open: 97, High: 98, Low: 94, Close: 95, Volume: 2400},
		{Time: baseTime.AddDate(0, 0, 24), Open: 95, High: 96, Low: 92, Close: 93, Volume: 2600},
		{Time: baseTime.AddDate(0, 0, 25), Open: 93, High: 94, Low: 90, Close: 91, Volume: 2800},

		// 반등 구간
		{Time: baseTime.AddDate(0, 0, 26), Open: 91, High: 96, Low: 91, Close: 95, Volume: 3000},
		{Time: baseTime.AddDate(0, 0, 27), Open: 95, High: 98, Low: 94, Close: 97, Volume: 2800},
		{Time: baseTime.AddDate(0, 0, 28), Open: 97, High: 100, Low: 96, Close: 99, Volume: 2600},
		{Time: baseTime.AddDate(0, 0, 29), Open: 99, High: 102, Low: 98, Close: 101, Volume: 2400},

		// MACD 계산을 위한 추가 데이터
		{Time: baseTime.AddDate(0, 0, 30), Open: 101, High: 104, Low: 100, Close: 103, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 31), Open: 103, High: 106, Low: 102, Close: 105, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 32), Open: 105, High: 108, Low: 104, Close: 107, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 33), Open: 107, High: 110, Low: 106, Close: 109, Volume: 1600},
		{Time: baseTime.AddDate(0, 0, 34), Open: 109, High: 112, Low: 108, Close: 111, Volume: 1400},
	}
	return prices
}

func TestEMA(t *testing.T) {
	prices := generateTestPrices()

	testCases := []struct {
		period int
		name   string
	}{
		{9, "EMA(9)"},
		{12, "EMA(12)"},
		{26, "EMA(26)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := EMA(prices, EMAOption{Period: tc.period})
			if err != nil {
				t.Errorf("%s 계산 중 에러 발생: %v", tc.name, err)
				return
			}

			fmt.Printf("\n%s 결과:\n", tc.name)
			for i, result := range results {
				if i < tc.period-1 { // 초기값 건너뜀
					continue
				}
				fmt.Printf("날짜: %s, EMA: %.2f\n",
					result.Timestamp.Format("2006-01-02"), result.Value)

				// EMA 값 검증
				if result.Value <= 0 {
					t.Errorf("잘못된 EMA 값: %.2f", result.Value)
				}
			}
		})
	}
}

func TestMACD(t *testing.T) {
	prices := generateTestPrices()

	macdResults, err := MACD(prices, MACDOption{
		ShortPeriod:  12,
		LongPeriod:   26,
		SignalPeriod: 9,
	})
	if err != nil {
		t.Errorf("MACD 계산 중 에러 발생: %v", err)
		return
	}

	fmt.Println("\nMACD(12,26,9) 결과:")
	for _, result := range macdResults {
		fmt.Printf("날짜: %s, MACD: %.2f, Signal: %.2f, Histogram: %.2f\n",
			result.Timestamp.Format("2006-01-02"),
			result.MACD, result.Signal, result.Histogram)
	}
}

func TestSAR(t *testing.T) {
	prices := generateTestPrices()

	testCases := []struct {
		name string
		opt  SAROption
	}{
		{"기본설정", DefaultSAROption()},
		{"민감설정", SAROption{AccelerationInitial: 0.04, AccelerationMax: 0.4}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := SAR(prices, tc.opt)
			if err != nil {
				t.Errorf("SAR 계산 중 에러 발생: %v", err)
				return
			}

			fmt.Printf("\nParabolic SAR (%s) 결과:\n", tc.name)
			prevSAR := 0.0
			prevTrend := true
			for i, result := range results {
				fmt.Printf("날짜: %s, SAR: %.2f, 추세: %s\n",
					result.Timestamp.Format("2006-01-02"),
					result.SAR,
					map[bool]string{true: "상승", false: "하락"}[result.IsLong])

				// SAR 값 검증
				if i > 0 {
					// SAR 값이 이전 값과 동일한지 체크
					if result.SAR == prevSAR {
						t.Logf("경고: %s에 SAR 값이 변화없음: %.2f",
							result.Timestamp.Format("2006-01-02"), result.SAR)
					}
					// 추세 전환 확인
					if result.IsLong != prevTrend {
						fmt.Printf(">>> 추세 전환 발생: %s에서 %s로 전환\n",
							map[bool]string{true: "상승", false: "하락"}[prevTrend],
							map[bool]string{true: "상승", false: "하락"}[result.IsLong])
					}
				}
				prevSAR = result.SAR
				prevTrend = result.IsLong
			}
		})
	}
}

func TestAll(t *testing.T) {
	prices := generateTestPrices()

	t.Run("EMA Test", func(t *testing.T) { TestEMA(t) })
	t.Run("MACD Test", func(t *testing.T) { TestMACD(t) })
	t.Run("SAR Test", func(t *testing.T) { TestSAR(t) })

	fmt.Println("\n=== 테스트 데이터 요약 ===")
	fmt.Printf("데이터 기간: %s ~ %s\n",
		prices[0].Time.Format("2006-01-02"),
		prices[len(prices)-1].Time.Format("2006-01-02"))
	fmt.Printf("데이터 개수: %d\n", len(prices))
	fmt.Printf("시작가: %.2f, 종가: %.2f\n", prices[0].Close, prices[len(prices)-1].Close)

	// 데이터 특성 분석
	maxPrice := prices[0].High
	minPrice := prices[0].Low
	maxVolume := prices[0].Volume
	for _, p := range prices {
		if p.High > maxPrice {
			maxPrice = p.High
		}
		if p.Low < minPrice {
			minPrice = p.Low
		}
		if p.Volume > maxVolume {
			maxVolume = p.Volume
		}
	}
	fmt.Printf("가격 범위: %.2f ~ %.2f (변동폭: %.2f%%)\n",
		minPrice, maxPrice, (maxPrice-minPrice)/minPrice*100)
	fmt.Printf("최대 거래량: %.0f\n", maxVolume)
}

```
## internal/analysis/indicator/macd.go
```go
package indicator

import (
	"fmt"
	"time"
)

// MACDOption은 MACD 계산에 필요한 옵션을 정의합니다
type MACDOption struct {
	ShortPeriod  int // 단기 EMA 기간
	LongPeriod   int // 장기 EMA 기간
	SignalPeriod int // 시그널 라인 기간
}

// MACDResult는 MACD 계산 결과를 정의합니다
type MACDResult struct {
	MACD      float64   // MACD 라인
	Signal    float64   // 시그널 라인
	Histogram float64   // 히스토그램
	Timestamp time.Time // 계산 시점
}

// ValidateMACDOption은 MACD 옵션을 검증합니다
func ValidateMACDOption(opt MACDOption) error {
	if opt.ShortPeriod <= 0 {
		return &ValidationError{
			Field: "ShortPeriod",
			Err:   fmt.Errorf("단기 기간은 0보다 커야 합니다: %d", opt.ShortPeriod),
		}
	}
	if opt.LongPeriod <= opt.ShortPeriod {
		return &ValidationError{
			Field: "LongPeriod",
			Err:   fmt.Errorf("장기 기간은 단기 기간보다 커야 합니다: %d <= %d", opt.LongPeriod, opt.ShortPeriod),
		}
	}
	if opt.SignalPeriod <= 0 {
		return &ValidationError{
			Field: "SignalPeriod",
			Err:   fmt.Errorf("시그널 기간은 0보다 커야 합니다: %d", opt.SignalPeriod),
		}
	}
	return nil
}

// MACD는 MACD(Moving Average Convergence Divergence) 지표를 계산합니다
func MACD(prices []PriceData, opt MACDOption) ([]MACDResult, error) {
	if err := ValidateMACDOption(opt); err != nil {
		return nil, err
	}

	// 단기 EMA 계산
	shortEMA, err := EMA(prices, EMAOption{Period: opt.ShortPeriod})
	if err != nil {
		return nil, fmt.Errorf("단기 EMA 계산 실패: %w", err)
	}

	// 장기 EMA 계산
	longEMA, err := EMA(prices, EMAOption{Period: opt.LongPeriod})
	if err != nil {
		return nil, fmt.Errorf("장기 EMA 계산 실패: %w", err)
	}

	// MACD 라인 계산 (단기 EMA - 장기 EMA)
	macdStartIdx := opt.LongPeriod - 1
	macdLine := make([]PriceData, len(prices)-macdStartIdx)
	for i := range macdLine {
		macdLine[i] = PriceData{
			Time:  prices[i+macdStartIdx].Time,
			Close: shortEMA[i+macdStartIdx].Value - longEMA[i+macdStartIdx].Value,
		}
	}

	// 시그널 라인 계산 (MACD의 EMA)
	signalLineData, err := EMA(macdLine, EMAOption{Period: opt.SignalPeriod})
	if err != nil {
		return nil, fmt.Errorf("시그널 라인 계산 실패: %w", err)
	}

	// 최종 결과 생성
	resultStartIdx := opt.SignalPeriod - 1
	results := make([]MACDResult, len(macdLine)-resultStartIdx)
	for i := range results {
		macdIdx := i + resultStartIdx
		results[i] = MACDResult{
			MACD:      macdLine[macdIdx].Close,
			Signal:    signalLineData[i].Value,
			Histogram: macdLine[macdIdx].Close - signalLineData[i].Value,
			Timestamp: macdLine[macdIdx].Time,
		}
	}

	return results, nil
}

```
## internal/analysis/indicator/sar.go
```go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// SAROption은 Parabolic SAR 계산에 필요한 옵션을 정의합니다
type SAROption struct {
	AccelerationInitial float64 // 초기 가속도
	AccelerationMax     float64 // 최대 가속도
}

// DefaultSAROption은 기본 SAR 옵션을 반환합니다
func DefaultSAROption() SAROption {
	return SAROption{
		AccelerationInitial: 0.02,
		AccelerationMax:     0.2,
	}
}

// ValidateSAROption은 SAR 옵션을 검증합니다
func ValidateSAROption(opt SAROption) error {
	if opt.AccelerationInitial <= 0 {
		return &ValidationError{
			Field: "AccelerationInitial",
			Err:   fmt.Errorf("초기 가속도는 0보다 커야 합니다: %f", opt.AccelerationInitial),
		}
	}
	if opt.AccelerationMax <= opt.AccelerationInitial {
		return &ValidationError{
			Field: "AccelerationMax",
			Err: fmt.Errorf("최대 가속도는 초기 가속도보다 커야 합니다: %f <= %f",
				opt.AccelerationMax, opt.AccelerationInitial),
		}
	}
	return nil
}

// SARResult는 Parabolic SAR 계산 결과를 정의합니다
type SARResult struct {
	SAR       float64   // SAR 값
	IsLong    bool      // 현재 추세가 상승인지 여부
	Timestamp time.Time // 계산 시점
}

// SAR은 Parabolic SAR 지표를 계산합니다
func SAR(prices []PriceData, opt SAROption) ([]SARResult, error) {
	results := make([]SARResult, len(prices))
	// 초기값 설정 단순화
	af := opt.AccelerationInitial
	sar := prices[0].Low
	ep := prices[0].High
	isLong := true

	results[0] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[0].Time}

	// SAR 계산
	for i := 1; i < len(prices); i++ {
		if isLong {
			sar = sar + af*(ep-sar)

			// 새로운 고점 발견
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+opt.AccelerationInitial, opt.AccelerationMax)
			}

			// 추세 전환 체크
			if sar > prices[i].Low {
				isLong = false
				sar = ep
				ep = prices[i].Low
				af = opt.AccelerationInitial
			}
		} else {
			sar = sar - af*(sar-ep)

			// 새로운 저점 발견
			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+opt.AccelerationInitial, opt.AccelerationMax)
			}

			// 추세 전환 체크
			if sar < prices[i].High {
				isLong = true
				sar = ep
				ep = prices[i].High
				af = opt.AccelerationInitial
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

```
## internal/analysis/indicator/types.go
```go
package indicator

import (
	"fmt"
	"time"
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

// Result는 지표 계산 결과를 정의합니다
type Result struct {
	Value     float64   // 지표값
	Timestamp time.Time // 계산 시점
}

// ValidationError는 입력값 검증 에러를 정의합니다
type ValidationError struct {
	Field string
	Err   error
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("유효하지 않은 %s: %v", e.Field, e.Err)
}

```
## internal/analysis/signal/detector.go
```go
package signal

import (
	"fmt"
	"log"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// DetectorConfig는 시그널 감지기 설정을 정의합니다
type DetectorConfig struct {
	EMALength      int // EMA 기간 (기본값: 200)
	StopLossPct    float64
	TakeProfitPct  float64
	MinHistogram   float64 // 최소 MACD 히스토그램 값 (기본값: 0.00005)
	MaxWaitCandles int     // 최대 대기 캔들 수 (기본값: 5)
}

// NewDetector는 새로운 시그널 감지기를 생성합니다
func NewDetector(config DetectorConfig) *Detector {
	return &Detector{
		states:         make(map[string]*SymbolState),
		emaLength:      config.EMALength,
		stopLossPct:    config.StopLossPct,
		takeProfitPct:  config.TakeProfitPct,
		minHistogram:   config.MinHistogram,
		maxWaitCandles: config.MaxWaitCandles,
	}
}

// Detect는 주어진 데이터로부터 시그널을 감지합니다
func (d *Detector) Detect(symbol string, prices []indicator.PriceData) (*Signal, error) {
	if len(prices) < d.emaLength {
		return nil, fmt.Errorf("insufficient data: need at least %d prices", d.emaLength)
	}

	// 심볼별 상태 가져오기
	state := d.getSymbolState(symbol)

	// 지표 계산
	ema, err := indicator.EMA(prices, indicator.EMAOption{Period: d.emaLength})
	if err != nil {
		return nil, fmt.Errorf("calculating EMA: %w", err)
	}

	macd, err := indicator.MACD(prices, indicator.MACDOption{
		ShortPeriod:  12,
		LongPeriod:   26,
		SignalPeriod: 9,
	})
	if err != nil {
		return nil, fmt.Errorf("calculating MACD: %w", err)
	}

	sar, err := indicator.SAR(prices, indicator.DefaultSAROption())
	if err != nil {
		return nil, fmt.Errorf("calculating SAR: %w", err)
	}

	currentPrice := prices[len(prices)-1].Close
	currentMACD := macd[len(macd)-1].MACD
	currentSignal := macd[len(macd)-1].Signal
	currentHistogram := currentMACD - currentSignal
	currentEMA := ema[len(ema)-1].Value
	currentSAR := sar[len(sar)-1].SAR
	currentHigh := prices[len(prices)-1].High
	currentLow := prices[len(prices)-1].Low

	/// EMA 및 SAR 조건 확인
	isAboveEMA := currentPrice > currentEMA
	sarBelowCandle := currentSAR < currentLow
	sarAboveCandle := currentSAR > currentHigh

	// MACD 크로스 확인 - 이제 심볼별 상태 사용
	macdCross := d.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 기본 시그널 객체 생성
	signal := &Signal{
		Type:      NoSignal,
		Symbol:    symbol,
		Price:     currentPrice,
		Timestamp: prices[len(prices)-1].Time,
	}

	// 시그널 조건
	signal.Conditions = SignalConditions{
		EMALong:     isAboveEMA,
		EMAShort:    !isAboveEMA,
		MACDLong:    macdCross == 1,
		MACDShort:   macdCross == -1,
		SARLong:     sarBelowCandle,
		SARShort:    !sarBelowCandle,
		EMAValue:    currentEMA,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		SARValue:    currentSAR,
	}

	// 1. 대기 상태 확인 및 업데이트
	if state.PendingSignal != NoSignal {
		pendingSignal := d.processPendingState(state, symbol, signal, currentHistogram, sarBelowCandle, sarAboveCandle)
		if pendingSignal != nil {
			// 상태 업데이트
			state.PrevMACD = currentMACD
			state.PrevSignal = currentSignal
			state.PrevHistogram = currentHistogram
			state.LastSignal = pendingSignal
			return pendingSignal, nil
		}
	}

	// 2. 일반 시그널 조건 확인 (대기 상태가 없거나 취소된 경우)

	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= d.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signal.Type = Long
		signal.StopLoss = currentSAR                                        // SAR 기반 손절가
		signal.TakeProfit = currentPrice + (currentPrice - signal.StopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= d.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signal.Type = Short
		signal.StopLoss = currentSAR                                        // SAR 기반 손절가
		signal.TakeProfit = currentPrice - (signal.StopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// 3. 새로운 대기 상태 설정 (일반 시그널이 아닌 경우)
	if signal.Type == NoSignal {
		// MACD 상향돌파 + EMA 위 + SAR 캔들 아래가 아닌 경우 -> 롱 대기 상태
		if isAboveEMA && macdCross == 1 && !sarBelowCandle && currentHistogram > 0 {
			state.PendingSignal = PendingLong
			state.WaitedCandles = 0
			log.Printf("[%s] Long 대기 상태 시작: MACD 상향돌파, SAR 반전 대기", symbol)
		}

		// MACD 하향돌파 + EMA 아래 + SAR이 캔들 위가 아닌 경우 → 숏 대기 상태
		if !isAboveEMA && macdCross == -1 && !sarAboveCandle && currentHistogram < 0 {
			state.PendingSignal = PendingShort
			state.WaitedCandles = 0
			log.Printf("[%s] Short 대기 상태 시작: MACD 하향돌파, SAR 반전 대기", symbol)
		}
	}

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal
	state.PrevHistogram = currentHistogram

	if signal.Type != NoSignal {
		state.LastSignal = signal
	}

	state.LastSignal = signal
	return signal, nil
}

// processPendingState는 대기 상태를 처리하고 시그널을 생성합니다
func (d *Detector) processPendingState(state *SymbolState, symbol string, baseSignal *Signal, currentHistogram float64, sarBelowCandle bool, sarAboveCandle bool) *Signal {
	// 캔들 카운트 증가
	state.WaitedCandles++

	// 최대 대기 시간 초과 체크
	if state.WaitedCandles > state.MaxWaitCandles {
		log.Printf("[%s] 대기 상태 취소: 최대 대기 캔들 수 (%d) 초과", symbol, state.MaxWaitCandles)
		d.resetPendingState(state)
		return nil
	}

	resultSignal := &Signal{
		Type:       NoSignal,
		Symbol:     baseSignal.Symbol,
		Price:      baseSignal.Price,
		Timestamp:  baseSignal.Timestamp,
		Conditions: baseSignal.Conditions,
	}

	// Long 대기 상태 처리
	if state.PendingSignal == PendingLong {
		// 히스토그램이 음수로 바뀌면 취소(추세 역전)
		if currentHistogram < 0 && state.PrevHistogram > 0 {
			log.Printf("[%s] Long 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			d.resetPendingState(state)
			return nil
		}

		// SAR가 캔들 아래로 이동하면 롱 시그널 생성
		if sarBelowCandle {
			resultSignal.Type = Long
			resultSignal.StopLoss = baseSignal.Conditions.SARValue
			resultSignal.TakeProfit = baseSignal.Price + (baseSignal.Price - resultSignal.StopLoss)

			log.Printf("[%s] Long 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			d.resetPendingState(state)
			return resultSignal
		}
	}

	// Short 대기 상태 처리
	if state.PendingSignal == PendingShort {
		// 히스토그램이 양수로 바뀌면 취소 (추세 역전)
		if currentHistogram > 0 && state.PrevHistogram < 0 {
			log.Printf("[%s] Short 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			d.resetPendingState(state)
			return nil
		}

		// SAR이 캔들 위로 이동하면 숏 시그널 생성
		if sarAboveCandle {
			resultSignal.Type = Short
			resultSignal.StopLoss = baseSignal.Conditions.SARValue
			resultSignal.TakeProfit = baseSignal.Price - (resultSignal.StopLoss - baseSignal.Price)

			log.Printf("[%s] Short 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			d.resetPendingState(state)
			return resultSignal
		}
	}

	return nil
}

// checkMACDCross는 MACD 크로스를 확인합니다
// 반환값: 1 (상향돌파), -1 (하향돌파), 0 (크로스 없음)
func (d *Detector) checkMACDCross(currentMACD, currentSignal, prevMACD, prevSignal float64) int {
	if prevMACD <= prevSignal && currentMACD > currentSignal {
		return 1 // 상향돌파
	}
	if prevMACD >= prevSignal && currentMACD < currentSignal {
		return -1 // 하향돌파
	}
	return 0 // 크로스 없음
}

// isDuplicateSignal은 중복 시그널인지 확인합니다
// func (d *Detector) isDuplicateSignal(signal *Signal) bool {
// 	if d.lastSignal == nil {
// 		return false
// 	}

// 	// 동일 방향의 시그널이 이미 존재하는 경우
// 	if d.lastSignal.Type == signal.Type {
// 		return true
// 	}

// 	return false
// }

```
## internal/analysis/signal/signal_test.go
```go
package signal

import (
	"math"
	"testing"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// 롱 시그널을 위한 테스트 데이터 생성
func generateLongSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250) // EMA 200 계산을 위해 충분한 데이터

	// 초기 하락 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - float64(i)*0.1,
			High:   startPrice - float64(i)*0.1 + 0.05,
			Low:    startPrice - float64(i)*0.1 - 0.05,
			Close:  startPrice - float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 상승 추세로 전환 (100-199)
	for i := 100; i < 200; i++ {
		increment := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + increment,
			High:   startPrice + increment + 0.05,
			Low:    startPrice + increment - 0.05,
			Close:  startPrice + increment,
			Volume: 1500.0,
		}
	}

	// 마지막 구간에서 강한 상승 추세 (200-249)
	// MACD 골든크로스와 SAR 하향 전환을 만들기 위한 데이터
	for i := 200; i < 250; i++ {
		increment := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 15.0 + increment,
			High:   startPrice + 15.0 + increment + 0.1,
			Low:    startPrice + 15.0 + increment - 0.05,
			Close:  startPrice + 15.0 + increment + 0.08,
			Volume: 2000.0,
		}
	}

	return prices
}

// 숏 시그널을 위한 테스트 데이터 생성
func generateShortSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// 초기 상승 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + float64(i)*0.1,
			High:   startPrice + float64(i)*0.1 + 0.05,
			Low:    startPrice + float64(i)*0.1 - 0.05,
			Close:  startPrice + float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 하락 추세로 전환 (100-199)
	for i := 100; i < 200; i++ {
		decrement := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - decrement,
			High:   startPrice - decrement + 0.05,
			Low:    startPrice - decrement - 0.05,
			Close:  startPrice - decrement,
			Volume: 1500.0,
		}
	}

	// 마지막 구간에서 강한 하락 추세 (200-249)
	for i := 200; i < 250; i++ {
		decrement := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 15.0 - decrement,
			High:   startPrice - 15.0 - decrement + 0.05,
			Low:    startPrice - 15.0 - decrement - 0.1,
			Close:  startPrice - 15.0 - decrement - 0.08,
			Volume: 2000.0,
		}
	}

	return prices
}

// 시그널이 발생하지 않는 데이터 생성
func generateNoSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// EMA200 주변에서 횡보하는 데이터 생성
	startPrice := 100.0
	for i := 0; i < 250; i++ {
		// sin 곡선을 사용하여 EMA200 주변에서 진동하는 가격 생성
		cycle := float64(i) * 0.1
		variation := math.Sin(cycle) * 0.5

		price := startPrice + variation
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   price - 0.1,
			High:   price + 0.2,
			Low:    price - 0.2,
			Close:  price,
			Volume: 1000.0 + math.Abs(variation)*100,
		}
	}

	return prices
}

func TestDetector_Detect(t *testing.T) {
	tests := []struct {
		name           string
		prices         []indicator.PriceData
		expectedSignal SignalType
		wantErr        bool
	}{
		{
			name:           "롱 시그널 테스트",
			prices:         generateLongSignalPrices(),
			expectedSignal: Long,
			wantErr:        false,
		},
		{
			name:           "숏 시그널 테스트",
			prices:         generateShortSignalPrices(),
			expectedSignal: Short,
			wantErr:        false,
		},
		{
			name:           "무시그널 테스트",
			prices:         generateNoSignalPrices(),
			expectedSignal: NoSignal,
			wantErr:        false,
		},
		{
			name:           "데이터 부족 테스트",
			prices:         generateLongSignalPrices()[:150], // 200개 미만의 데이터
			expectedSignal: NoSignal,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector(DetectorConfig{
				EMALength:     200,
				StopLossPct:   0.02,
				TakeProfitPct: 0.04,
			})

			signal, err := detector.Detect("BTCUSDT", tt.prices)

			// 에러 검증
			if (err != nil) != tt.wantErr {
				t.Errorf("Detect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// 시그널 타입 검증
			if signal.Type != tt.expectedSignal {
				t.Errorf("Expected %v signal, got %v", tt.expectedSignal, signal.Type)
			}

			// 시그널 타입별 추가 검증
			switch signal.Type {
			case Long:
				validateLongSignal(t, signal)
			case Short:
				validateShortSignal(t, signal)
			case NoSignal:
				validateNoSignal(t, signal)
			}
		})
	}
}

func validateLongSignal(t *testing.T, signal *Signal) {
	if !signal.Conditions.EMALong {
		t.Error("Expected price to be above EMA200")
	}
	if !signal.Conditions.MACDLong {
		t.Error("Expected MACD to cross above Signal line")
	}
	if !signal.Conditions.SARLong {
		t.Error("Expected SAR to be below price")
	}
	validateRiskRewardRatio(t, signal)
}

func validateShortSignal(t *testing.T, signal *Signal) {
	if !signal.Conditions.EMAShort {
		t.Error("Expected price to be below EMA200")
	}
	if !signal.Conditions.MACDShort {
		t.Error("Expected MACD to cross below Signal line")
	}
	if !signal.Conditions.SARShort {
		t.Error("Expected SAR to be above price")
	}
	validateRiskRewardRatio(t, signal)
}

func validateNoSignal(t *testing.T, signal *Signal) {
	if signal.StopLoss != 0 || signal.TakeProfit != 0 {
		t.Error("NoSignal should have zero StopLoss and TakeProfit")
	}
}

func validateRiskRewardRatio(t *testing.T, signal *Signal) {
	if signal.StopLoss == 0 || signal.TakeProfit == 0 {
		t.Error("StopLoss and TakeProfit should be non-zero")
		return
	}

	var riskAmount, rewardAmount float64
	if signal.Type == Long {
		riskAmount = signal.Price - signal.StopLoss
		rewardAmount = signal.TakeProfit - signal.Price
	} else {
		riskAmount = signal.StopLoss - signal.Price
		rewardAmount = signal.Price - signal.TakeProfit
	}

	if !almostEqual(riskAmount, rewardAmount, 0.0001) {
		t.Errorf("Risk:Reward ratio is not 1:1 (Risk: %.2f, Reward: %.2f)",
			riskAmount, rewardAmount)
	}
}

// 부동소수점 비교를 위한 헬퍼 함수
func almostEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// generatePendingLongSignalPrices는 Long 대기 상태를 확인하기 위한 테스트 데이터를 생성합니다
func generatePendingLongSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250) // EMA 200 계산을 위해 충분한 데이터

	// 초기 하락 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - float64(i)*0.1,
			High:   startPrice - float64(i)*0.1 + 0.05,
			Low:    startPrice - float64(i)*0.1 - 0.05,
			Close:  startPrice - float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 상승 추세로 전환 시작 (100-199)
	for i := 100; i < 200; i++ {
		increment := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + increment,
			High:   startPrice + increment + 0.05,
			Low:    startPrice + increment - 0.05,
			Close:  startPrice + increment,
			Volume: 1500.0,
		}
	}

	// 중간에 강한 상승 추세 (200-240) - MACD 골든 크로스 만들기
	for i := 200; i < 240; i++ {
		increment := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 15.0 + increment,
			High:   startPrice + 15.0 + increment + 0.1,
			Low:    startPrice + 15.0 + increment - 0.05,
			Close:  startPrice + 15.0 + increment + 0.08,
			Volume: 2000.0,
		}
	}

	// 마지막 부분 (240-244)에서 SAR은 아직 캔들 위에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		increment := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice + 27.0 + increment,
			// SAR이 계속 캔들 위에 유지되도록 고가를 낮게 설정
			High:   startPrice + 27.0 + increment + 0.03,
			Low:    startPrice + 27.0 + increment - 0.05,
			Close:  startPrice + 27.0 + increment + 0.02,
			Volume: 1800.0,
		}
	}

	// 마지막 캔들 (245-249)에서만 SAR이 캔들 아래로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		increment := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice + 28.0 + increment,
			// 마지막 캔들에서 확실하게 SAR이 아래로 가도록 고가를 확 높게 설정
			High:   startPrice + 28.0 + increment + 1.5,
			Low:    startPrice + 28.0 + increment - 0.1,
			Close:  startPrice + 28.0 + increment + 1.0,
			Volume: 2500.0,
		}
	}

	return prices
}

// generatePendingShortSignalPrices는 Short 대기 상태를 확인하기 위한 테스트 데이터를 생성합니다
func generatePendingShortSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// 초기 상승 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + float64(i)*0.1,
			High:   startPrice + float64(i)*0.1 + 0.05,
			Low:    startPrice + float64(i)*0.1 - 0.05,
			Close:  startPrice + float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 하락 추세로 전환 시작 (100-199)
	for i := 100; i < 200; i++ {
		decrement := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - decrement,
			High:   startPrice - decrement + 0.05,
			Low:    startPrice - decrement - 0.05,
			Close:  startPrice - decrement,
			Volume: 1500.0,
		}
	}

	// 강한 하락 추세 (200-240) - MACD 데드 크로스 만들기
	for i := 200; i < 240; i++ {
		decrement := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 15.0 - decrement,
			High:   startPrice - 15.0 - decrement + 0.05,
			Low:    startPrice - 15.0 - decrement - 0.1,
			Close:  startPrice - 15.0 - decrement - 0.08,
			Volume: 2000.0,
		}
	}

	// 마지막 부분 (240-244)에서 SAR은 아직 캔들 아래에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		decrement := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice - 27.0 - decrement,
			High: startPrice - 27.0 - decrement + 0.03,
			// SAR이 계속 캔들 아래에 유지되도록 저가를 높게 설정
			Low:    startPrice - 27.0 - decrement - 0.03,
			Close:  startPrice - 27.0 - decrement - 0.02,
			Volume: 1800.0,
		}
	}

	// 마지막 캔들 (245-249)에서만 SAR이 캔들 위로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		decrement := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice - 28.0 - decrement,
			High: startPrice - 28.0 - decrement + 0.1,
			// 마지막 캔들에서 확실하게 SAR이 위로 가도록 저가를 확 낮게 설정
			Low:    startPrice - 28.0 - decrement - 1.5,
			Close:  startPrice - 28.0 - decrement - 1.0,
			Volume: 2500.0,
		}
	}

	return prices
}

// TestPendingSignals는 대기 상태 및 신호 감지를 테스트합니다
func TestPendingSignals(t *testing.T) {
	detector := NewDetector(DetectorConfig{
		EMALength:      200,
		StopLossPct:    0.02,
		TakeProfitPct:  0.04,
		MaxWaitCandles: 5,
		MinHistogram:   0.00005,
	})

	t.Run("롱/숏 신호 감지 테스트", func(t *testing.T) {
		// 롱 신호 테스트
		longPrices := generateLongSignalPrices()
		longSignal, err := detector.Detect("BTCUSDT", longPrices)
		if err != nil {
			t.Fatalf("롱 신호 감지 에러: %v", err)
		}

		if longSignal.Type != Long {
			t.Errorf("롱 신호 감지 실패: 예상 타입 Long, 실제 %s", longSignal.Type)
		} else {
			t.Logf("롱 신호 감지 성공: 가격=%.2f, 손절=%.2f, 익절=%.2f",
				longSignal.Price, longSignal.StopLoss, longSignal.TakeProfit)
		}

		// 숏 신호 테스트
		shortPrices := generateShortSignalPrices()
		shortSignal, err := detector.Detect("BTCUSDT", shortPrices)
		if err != nil {
			t.Fatalf("숏 신호 감지 에러: %v", err)
		}

		if shortSignal.Type != Short {
			t.Errorf("숏 신호 감지 실패: 예상 타입 Short, 실제 %s", shortSignal.Type)
		} else {
			t.Logf("숏 신호 감지 성공: 가격=%.2f, 손절=%.2f, 익절=%.2f",
				shortSignal.Price, shortSignal.StopLoss, shortSignal.TakeProfit)
		}
	})

	t.Run("대기 상태 단위 테스트", func(t *testing.T) {
		// 롱 대기 상태 테스트
		symbolState := &SymbolState{
			PendingSignal:  PendingLong,
			WaitedCandles:  2,
			MaxWaitCandles: 5,
			PrevMACD:       0.001,
			PrevSignal:     0.0005,
			PrevHistogram:  0.0005,
		}

		// SAR이 캔들 아래로 반전된 상황 시뮬레이션
		baseSignal := &Signal{
			Type:      NoSignal,
			Symbol:    "BTCUSDT",
			Price:     100.0,
			Timestamp: time.Now(),
			Conditions: SignalConditions{
				SARValue: 98.0, // SAR이 캔들 아래로 이동
			},
		}

		// 롱 대기 상태에서 SAR 반전 시 롱 신호가 생성되는지 확인
		result := detector.processPendingState(symbolState, "BTCUSDT", baseSignal, 0.001, true, false)
		if result == nil {
			t.Errorf("롱 대기 상태에서 SAR 반전 시 신호가 생성되지 않음")
		} else if result.Type != Long {
			t.Errorf("롱 대기 상태에서 SAR 반전 시 잘못된 신호 타입: %s", result.Type)
		} else {
			t.Logf("롱 대기 상태 테스트 성공")
		}

		// 숏 대기 상태 테스트
		symbolState = &SymbolState{
			PendingSignal:  PendingShort,
			WaitedCandles:  2,
			MaxWaitCandles: 5,
			PrevMACD:       -0.001,
			PrevSignal:     -0.0005,
			PrevHistogram:  -0.0005,
		}

		// SAR이 캔들 위로 반전된 상황 시뮬레이션
		baseSignal = &Signal{
			Type:      NoSignal,
			Symbol:    "BTCUSDT",
			Price:     100.0,
			Timestamp: time.Now(),
			Conditions: SignalConditions{
				SARValue: 102.0, // SAR이 캔들 위로 이동
			},
		}

		// 숏 대기 상태에서 SAR 반전 시 숏 신호가 생성되는지 확인
		result = detector.processPendingState(symbolState, "BTCUSDT", baseSignal, -0.001, false, true)
		if result == nil {
			t.Errorf("숏 대기 상태에서 SAR 반전 시 신호가 생성되지 않음")
		} else if result.Type != Short {
			t.Errorf("숏 대기 상태에서 SAR 반전 시 잘못된 신호 타입: %s", result.Type)
		} else {
			t.Logf("숏 대기 상태 테스트 성공")
		}
	})
}

```
## internal/analysis/signal/state.go
```go
package signal

// getSymbolState는 심볼별 상태를 가져옵니다
func (d *Detector) getSymbolState(symbol string) *SymbolState {
	d.mu.RLock()
	state, exists := d.states[symbol]
	d.mu.RUnlock()

	if !exists {
		d.mu.Lock()
		state = &SymbolState{
			PendingSignal:  NoSignal,
			WaitedCandles:  0,
			MaxWaitCandles: d.maxWaitCandles,
		}
		d.states[symbol] = state
		d.mu.Unlock()
	}

	return state
}

// resetPendingState는 심볼의 대기 상태를 초기화합니다
func (d *Detector) resetPendingState(state *SymbolState) {
	state.PendingSignal = NoSignal
	state.WaitedCandles = 0
}

```
## internal/analysis/signal/types.go
```go
package signal

import (
	"sync"
	"time"
)

// SignalType은 시그널 유형을 정의합니다
type SignalType int

const (
	NoSignal SignalType = iota
	Long
	Short
	PendingLong  // MACD 상향 돌파 후 SAR 반전 대기 상태
	PendingShort // MACD 하향돌파 후 SAR 반전 대기 상태
)

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
	Type       SignalType
	Symbol     string
	Price      float64
	Timestamp  time.Time
	Conditions SignalConditions
	StopLoss   float64
	TakeProfit float64
}

// SymbolState는 각 심볼별 상태를 관리합니다
type SymbolState struct {
	PrevMACD       float64    // 이전 MACD 값
	PrevSignal     float64    // 이전 Signal 값
	PrevHistogram  float64    // 이전 히스토그램 값
	LastSignal     *Signal    // 마지막 발생 시그널
	PendingSignal  SignalType // 대기중인 시그널 타입
	WaitedCandles  int        // 대기한 캔들 수
	MaxWaitCandles int        // 최대 대기 캔들 수
}

// Detector는 시그널 감지기를 정의합니다
type Detector struct {
	states         map[string]*SymbolState
	emaLength      int     // EMA 기간
	stopLossPct    float64 // 손절 비율
	takeProfitPct  float64 // 익절 비율
	minHistogram   float64 // MACD 히스토그램 최소값
	maxWaitCandles int     // 기본 최대 대기 캔들 수
	mu             sync.RWMutex
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
			NotionalCap      float64 `json:"notionalCap,string"`
			NotionalFloor    float64 `json:"notionalFloor,string"`
			MaintMarginRatio float64 `json:"maintMarginRatio,string"`
			Cum              float64 `json:"cum,string"`
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
## internal/market/client.go
```go
package market

import (
	"strings"
)

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
## internal/market/collector.go
```go
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
	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/exchange"
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
	exchange exchange.Exchange
	discord  *discord.Client
	detector *signal.Detector
	config   *config.Config

	retry RetryConfig
	mu    sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(exchange exchange.Exchange, discord *discord.Client, detector *signal.Detector, config *config.Config, opts ...CollectorOption) *Collector {
	c := &Collector{
		exchange: exchange,
		discord:  discord,
		detector: detector,
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
func (c *Collector) checkEntryAvailable(ctx context.Context, coinSignal *signal.Signal) (bool, error) {
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
	newSignalIsLong := coinSignal.Type == signal.Long

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
func (c *Collector) ExecuteSignalTrade(ctx context.Context, s *signal.Signal) error {
	if s.Type == signal.NoSignal {
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
	if s.Type == signal.Short {
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
	if s.Type == signal.Short {
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
	if s.Type == signal.Long {
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
	if s.Type == signal.Short {
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
	if s.Type == signal.Short {
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
## internal/notification/discord/signal.go
```go
package discord

import (
	"fmt"

	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s *signal.Signal) error {
	var title, emoji string
	var color int

	switch s.Type {
	case signal.Long:
		emoji = "🚀"
		title = "LONG"
		color = notification.ColorSuccess
	case signal.Short:
		emoji = "🔻"
		title = "SHORT"
		color = notification.ColorError
	case signal.PendingLong:
		emoji = "⏳"
		title = "PENDING LONG"
		color = notification.ColorWarning
	case signal.PendingShort:
		emoji = "⏳"
		title = "PENDING SHORT"
		color = notification.ColorWarning
	default:
		emoji = "⚠️"
		title = "NO SIGNAL"
		color = notification.ColorInfo
	}

	// 시그널 조건 상태 표시
	longConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 위)
%s MACD (시그널 상향돌파)
%s SAR (SAR이 가격 아래)`,
		getCheckMark(s.Conditions.EMALong),
		getCheckMark(s.Conditions.MACDLong),
		getCheckMark(s.Conditions.SARLong))

	shortConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 아래)
		%s MACD (시그널 하향돌파)
		%s SAR (SAR이 가격 위)`,
		getCheckMark(s.Conditions.EMAShort),
		getCheckMark(s.Conditions.MACDShort),
		getCheckMark(s.Conditions.SARShort))

	// 기술적 지표 값
	technicalValues := fmt.Sprintf("```\n[EMA200]: %.5f\n[MACD Line]: %.5f\n[Signal Line]: %.5f\n[Histogram]: %.5f\n[SAR]: %.5f```",
		s.Conditions.EMAValue,
		s.Conditions.MACDValue,
		s.Conditions.SignalValue,
		s.Conditions.MACDValue-s.Conditions.SignalValue,
		s.Conditions.SARValue)

	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s %s/USDT", emoji, title, s.Symbol)).
		SetColor(color)

	if s.Type != signal.NoSignal {
		// 손익률 계산 및 표시
		var slPct, tpPct float64
		switch s.Type {
		case signal.Long:
			// Long: 실제 수치 그대로 표시
			slPct = (s.StopLoss - s.Price) / s.Price * 100
			tpPct = (s.TakeProfit - s.Price) / s.Price * 100
		case signal.Short:
			// Short: 부호 반대로 표시
			slPct = (s.Price - s.StopLoss) / s.Price * 100
			tpPct = (s.Price - s.TakeProfit) / s.Price * 100
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			s.StopLoss,
			slPct,
			s.TakeProfit,
			tpPct,
		))
	} else if s.Type == signal.PendingLong || s.Type == signal.PendingShort {
		// 대기 상태 정보 표시
		var waitingFor string
		if s.Type == signal.PendingLong {
			waitingFor = "SAR가 캔들 아래로 이동 대기 중"
		} else {
			waitingFor = "SAR가 캔들 위로 이동 대기 중"
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
**현재가**: $%.2f
**대기 상태**: %s
**조건**: MACD 크로스 발생, SAR 위치 부적절`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			waitingFor,
		))
	} else {
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
		))
	}

	embed.AddField("LONG 조건", longConditions, true)
	embed.AddField("SHORT 조건", shortConditions, true)
	embed.AddField("기술적 지표", technicalValues, false)

	return c.sendToWebhook(c.signalWebhook, WebhookMessage{
		Embeds: []Embed{*embed},
	})
}

func getCheckMark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}

```
## internal/notification/discord/webhook.go
```go
package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 전송합니다
// func (c *Client) SendSignal(signal notification.Signal) error {
// 	embed := NewEmbed().
// 		SetTitle(fmt.Sprintf("트레이딩 시그널: %s", signal.Symbol)).
// 		SetDescription(fmt.Sprintf("**타입**: %s\n**가격**: $%.2f\n**이유**: %s",
// 			signal.Type, signal.Price, signal.Reason)).
// 		SetColor(getColorForSignal(signal.Type)).
// 		SetFooter("Assist by Trading Bot 🤖").
// 		SetTimestamp(signal.Timestamp)

// 	msg := WebhookMessage{
// 		Embeds: []Embed{*embed},
// 	}

// 	return c.sendToWebhook(c.signalWebhook, msg)
// }

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

import "time"

// SignalType은 트레이딩 시그널 종류를 정의합니다
type SignalType string

const (
	SignalLong         SignalType = "LONG"
	SignalShort        SignalType = "SHORT"
	SignalClose        SignalType = "CLOSE"
	SignalPendingLong  SignalType = "PENDINGLONG"  // 롱 대기 상태
	SignalPendingShort SignalType = "PENDINGSHORT" // 숏 대기 상태
	ColorSuccess                  = 0x00FF00
	ColorError                    = 0xFF0000
	ColorInfo                     = 0x0000FF
	ColorWarning                  = 0xFFA500 // 대기 상태를 위한 주황색 추가
)

// Signal은 트레이딩 시그널 정보를 담는 구조체입니다
type Signal struct {
	Type      SignalType
	Symbol    string
	Price     float64
	Timestamp time.Time
	Reason    string
}

// Notifier는 알림 전송 인터페이스를 정의합니다
type Notifier interface {
	// SendSignal은 트레이딩 시그널 알림을 전송합니다
	SendSignal(signal Signal) error

	// SendError는 에러 알림을 전송합니다
	SendError(err error) error

	// SendInfo는 일반 정보 알림을 전송합니다
	SendInfo(message string) error
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

// getColorForPosition은 포지션 타입에 따른 색상을 반환합니다
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
