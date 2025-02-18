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
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assist-by/phoenix/internal/config"
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
	// 로그 설정
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("트레이딩 봇 시작...")

	// 설정 로드
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

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

	// 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 바이낸스 클라이언트 생성
	binanceClient := market.NewClient(
		cfg.Binance.APIKey,
		cfg.Binance.SecretKey,
		market.WithTimeout(10*time.Second),
	)
	// 바이낸스 서버와 시간 동기화
	if err := binanceClient.SyncTime(ctx); err != nil {
		log.Printf("바이낸스 서버 시간 동기화 실패: %v", err)
		if err := discordClient.SendError(fmt.Errorf("바이낸스 서버 시간 동기화 실패: %w", err)); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		os.Exit(1)
	}

	// 데이터 수집기 생성
	collector := market.NewCollector(
		binanceClient,
		discordClient,
		cfg.App.FetchInterval,
		cfg.App.CandleLimit,
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
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

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

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// NewDetector는 새로운 시그널 감지기를 생성합니다
func NewDetector(config DetectorConfig) *Detector {
	return &Detector{
		states:        make(map[string]*SymbolState),
		emaLength:     config.EMALength,
		stopLossPct:   config.StopLossPct,
		takeProfitPct: config.TakeProfitPct,
		minHistogram:  config.MinHistogram,
	}
}

// DetectorConfig는 시그널 감지기 설정을 정의합니다
type DetectorConfig struct {
	EMALength     int // EMA 기간 (기본값: 200)
	StopLossPct   float64
	TakeProfitPct float64
	MinHistogram  float64 // 최소 MACD 히스토그램 값 (기본값: 0.00005)
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

	// MACD 크로스 확인 - 이제 심볼별 상태 사용
	macdCross := d.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal

	signal := &Signal{
		Type:      NoSignal,
		Symbol:    symbol,
		Price:     currentPrice,
		Timestamp: prices[len(prices)-1].Time,
	}

	// MACD 히스토그램 계산
	histogram := currentMACD - currentSignal

	// Long 시그널
	if currentPrice > ema[len(ema)-1].Value && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		histogram >= d.minHistogram && // MACD 히스토그램이 최소값 이상
		sar[len(sar)-1].SAR < prices[len(prices)-1].Low { // SAR이 현재 봉의 저가보다 낮음

		signal.Type = Long
		signal.StopLoss = sar[len(sar)-1].SAR                               // SAR 기반 손절가
		signal.TakeProfit = currentPrice + (currentPrice - signal.StopLoss) // 1:1 비율
	}

	// Short 시그널
	if currentPrice < ema[len(ema)-1].Value && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-histogram >= d.minHistogram && // 음수 히스토그램에 대한 조건
		sar[len(sar)-1].SAR > prices[len(prices)-1].High { // SAR이 현재 봉의 고가보다 높음

		signal.Type = Short
		signal.StopLoss = sar[len(sar)-1].SAR                               // SAR 기반 손절가
		signal.TakeProfit = currentPrice - (signal.StopLoss - currentPrice) // 1:1 비율
	}

	emaCondition := currentPrice > ema[len(ema)-1].Value
	sarCondition := sar[len(sar)-1].SAR < prices[len(prices)-1].Low

	// 시그널 조건
	signal.Conditions = SignalConditions{
		EMALong:     emaCondition,
		EMAShort:    !emaCondition,
		MACDLong:    macdCross == 1,
		MACDShort:   macdCross == -1,
		SARLong:     sarCondition,
		SARShort:    !sarCondition,
		EMAValue:    ema[len(ema)-1].Value,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		SARValue:    sar[len(sar)-1].SAR,
	}

	state.LastSignal = signal
	return signal, nil
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
		state = &SymbolState{}
		d.states[symbol] = state
		d.mu.Unlock()
	}

	return state
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
)

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
	PrevMACD   float64
	PrevSignal float64
	LastSignal *Signal
}

// Detector는 시그널 감지기를 정의합니다
type Detector struct {
	states        map[string]*SymbolState
	emaLength     int     // EMA 기간
	stopLossPct   float64 // 손절 비율
	takeProfitPct float64 // 익절 비율
	minHistogram  float64 // MACD 히스토그램 최소값
	mu            sync.RWMutex
}

```
## internal/config/config.go
```go
package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// 바이낸스 API 설정
	Binance struct {
		APIKey    string `envconfig:"BINANCE_API_KEY" required:"true"`
		SecretKey string `envconfig:"BINANCE_SECRET_KEY" required:"true"`
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
		FetchInterval time.Duration `envconfig:"FETCH_INTERVAL" default:"15m"`
		CandleLimit   int           `envconfig:"CANDLE_LIMIT" default:"100"`
	}

	// 거래 설정
	Trading struct {
		Leverage int `envconfig:"LEVERAGE" default:"5" validate:"min=1,max=100"`
	}
}

// ValidateConfig는 설정이 유효한지 확인합니다.
func ValidateConfig(cfg *Config) error {
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

	// 설정값 검증
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("설정값 검증 실패: %w", err)
	}

	return &cfg, nil
}

```
## internal/market/client.go
```go
package market

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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

// sign은 요청에 대한 서명을 생성합니다
func (c *Client) sign(payload string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// doRequest는 HTTP 요청을 실행하고 결과를 반환합니다
func (c *Client) doRequest(ctx context.Context, method, endpoint string, params url.Values, needSign bool) ([]byte, error) {
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
		// 서버 시간으로 타임스탬프 설정
		timestamp := strconv.FormatInt(c.getServerTime(), 10)
		params.Set("timestamp", timestamp)
		// recvWindow 설정 (선택적)
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

// GetServerTime은 서버 시간을 조회합니다
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
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

// GetKlines는 캔들 데이터를 조회합니다
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]CandleData, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("interval", interval)
	params.Add("limit", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var rawCandles [][]interface{}
	if err := json.Unmarshal(resp, &rawCandles); err != nil {
		return nil, fmt.Errorf("캔들 데이터 파싱 실패: %w", err)
	}

	candles := make([]CandleData, len(rawCandles))
	for i, raw := range rawCandles {
		candles[i] = CandleData{
			OpenTime:  int64(raw[0].(float64)),
			CloseTime: int64(raw[6].(float64)),
		}
		// 숫자 문자열을 float64로 변환
		if open, err := strconv.ParseFloat(raw[1].(string), 64); err == nil {
			candles[i].Open = open
		}
		if high, err := strconv.ParseFloat(raw[2].(string), 64); err == nil {
			candles[i].High = high
		}
		if low, err := strconv.ParseFloat(raw[3].(string), 64); err == nil {
			candles[i].Low = low
		}
		if close, err := strconv.ParseFloat(raw[4].(string), 64); err == nil {
			candles[i].Close = close
		}
		if volume, err := strconv.ParseFloat(raw[5].(string), 64); err == nil {
			candles[i].Volume = volume
		}
	}

	return candles, nil
}

// GetBalance는 계정의 잔고를 조회합니다
func (c *Client) GetBalance(ctx context.Context) (map[string]Balance, error) {
	params := url.Values{}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/account", params, true)
	if err != nil {
		return nil, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	var result struct {
		Assets []struct {
			Asset            string  `json:"asset"`
			WalletBalance    float64 `json:"walletBalance,string"`
			UnrealizedProfit float64 `json:"unrealizedProfit,string"`
			MarginBalance    float64 `json:"marginBalance,string"`
			AvailableBalance float64 `json:"availableBalance,string"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	balances := make(map[string]Balance)
	for _, asset := range result.Assets {
		if asset.WalletBalance > 0 {
			balances[asset.Asset] = Balance{
				Asset:              asset.Asset,
				Available:          asset.AvailableBalance,
				Locked:             asset.WalletBalance - asset.AvailableBalance,
				CrossWalletBalance: asset.WalletBalance,
			}
		}
	}

	return balances, nil
}

// PlaceOrder는 새로운 주문을 생성합니다
func (c *Client) PlaceOrder(ctx context.Context, order OrderRequest) (*OrderResponse, error) {
	params := url.Values{}
	params.Add("symbol", order.Symbol)
	params.Add("side", string(order.Side))
	params.Add("type", string(order.Type))
	params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
	params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
	params.Add("stopLimitPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
	params.Add("price", strconv.FormatFloat(order.TakeProfit, 'f', -1, 64))

	if order.PositionSide != "" {
		params.Add("positionSide", string(order.PositionSide))
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/order/oco", params, true)
	if err != nil {
		return nil, fmt.Errorf("OCO 주문 실행 실패: %w", err)
	}

	var result OrderResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("주문 응답 파싱 실패: %w", err)
	}

	return &result, nil
}

// // PlaceTPSLOrder는 손절/익절 주문을 생성합니다
// func (c *Client) PlaceTPSLOrder(ctx context.Context, mainOrder *OrderResponse, stopLoss, takeProfit float64) error {
// 	if stopLoss > 0 {
// 		slOrder := OrderRequest{
// 			Symbol:       mainOrder.Symbol,
// 			Side:         getOppositeOrderSide(OrderSide(mainOrder.Side)),
// 			Type:         StopMarket,
// 			Quantity:     mainOrder.ExecutedQuantity,
// 			StopPrice:    stopLoss,
// 			PositionSide: mainOrder.PositionSide,
// 		}
// 		if _, err := c.PlaceOrder(ctx, slOrder); err != nil {
// 			return fmt.Errorf("손절 주문 실패: %w", err)
// 		}
// 	}

// 	if takeProfit > 0 {
// 		tpOrder := OrderRequest{
// 			Symbol:       mainOrder.Symbol,
// 			Side:         getOppositeOrderSide(OrderSide(mainOrder.Side)),
// 			Type:         TakeProfitMarket,
// 			Quantity:     mainOrder.ExecutedQuantity,
// 			StopPrice:    takeProfit,
// 			PositionSide: mainOrder.PositionSide,
// 		}
// 		if _, err := c.PlaceOrder(ctx, tpOrder); err != nil {
// 			return fmt.Errorf("익절 주문 실패: %w", err)
// 		}
// 	}

// 	return nil
// }

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
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == ErrPositionModeNoChange {
			return nil // 이미 원하는 모드로 설정된 경우
		}
		return fmt.Errorf("포지션 모드 설정 실패: %w", err)
	}

	return nil
}

// getOppositeOrderSide는 주문의 반대 방향을 반환합니다
func getOppositeOrderSide(side OrderSide) OrderSide {
	if side == Buy {
		return Sell
	}
	return Buy
}

// GetTopVolumeSymbols는 거래량 기준 상위 n개 심볼을 조회합니다
func (c *Client) GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("거래량 데이터 조회 실패: %w", err)
	}

	var tickers []SymbolVolume
	if err := json.Unmarshal(resp, &tickers); err != nil {
		return nil, fmt.Errorf("거래량 데이터 파싱 실패: %w", err)
	}

	// USDT 마진 선물만 필터링
	var filteredTickers []SymbolVolume
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

	// 거래량 로깅 (시각화)
	if len(filteredTickers) > 0 {
		maxVolume := filteredTickers[0].QuoteVolume
		log.Println("\n=== 상위 거래량 심볼 ===")
		for i := 0; i < resultCount; i++ {
			ticker := filteredTickers[i]
			barLength := int((ticker.QuoteVolume / maxVolume) * 50) // 최대 50칸
			bar := strings.Repeat("=", barLength)
			log.Printf("%-12s %15.2f USDT ||%s\n",
				ticker.Symbol, ticker.QuoteVolume, bar)
		}
		log.Println("========================")
	}

	return symbols, nil
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

// getServerTime은 현재 서버 시간을 반환합니다
func (c *Client) getServerTime() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UnixMilli() + c.serverTimeOffset
}

```
## internal/market/collector.go
```go
// internal/market/collector.go

package market

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
	"github.com/assist-by/phoenix/internal/analysis/signal"
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
	fetchInterval time.Duration
	candleLimit   int
	retry         RetryConfig
	detector      *signal.Detector
	mu            sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(client *Client, discord *discord.Client, fetchInterval time.Duration, candleLimit int, opts ...CollectorOption) *Collector {
	c := &Collector{
		client:        client,
		discord:       discord,
		fetchInterval: fetchInterval,
		candleLimit:   candleLimit,
		detector: signal.NewDetector(signal.DetectorConfig{
			EMALength:     200,
			StopLossPct:   0.02,
			TakeProfitPct: 0.04,
		}),
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

			// 시그널이 있으면 Discord로 전송
			if s != nil {
				if err := c.discord.SendSignal(s); err != nil {
					log.Printf("시그널 알림 전송 실패 (%s): %v", symbol, err)
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

// 에러 코드 상수 정의
const (
	ErrPositionModeNoChange = -4059 // 포지션 모드 변경 불필요 에러
)

// CandleData는 캔들 데이터를 표현합니다
type CandleData struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open,string"`
	High                float64 `json:"high,string"`
	Low                 float64 `json:"low,string"`
	Close               float64 `json:"close,string"`
	Volume              float64 `json:"volume,string"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume,string"`
	NumberOfTrades      int     `json:"numberOfTrades"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume,string"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume,string"`
}

// Balance는 계정 잔고 정보를 표현합니다
type Balance struct {
	Asset              string  `json:"asset"`
	Available          float64 `json:"available"`
	Locked             float64 `json:"locked"`
	CrossWalletBalance float64 `json:"crossWalletBalance"`
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

	Market  OrderType = "MARKET"
	Limit   OrderType = "LIMIT"
	StopOCO OrderType = "STOP_OCO"
)

// OrderRequest는 주문 요청 정보를 표현합니다

type OrderRequest struct {
	Symbol       string
	Side         OrderSide
	PositionSide PositionSide
	Type         OrderType
	Quantity     float64
	Price        float64 // 진입가격
	StopPrice    float64 // 손절가격
	TakeProfit   float64 // 익절가격
}

// OrderResponse는 주문 응답을 표현합니다
type OrderResponse struct {
	OrderID          int64        `json:"orderId"`
	Symbol           string       `json:"symbol"`
	Status           string       `json:"status"`
	ClientOrderID    string       `json:"clientOrderId"`
	Price            float64      `json:"price,string"`
	AvgPrice         float64      `json:"avgPrice,string"`
	OrigQuantity     float64      `json:"origQty,string"`
	ExecutedQuantity float64      `json:"executedQty,string"`
	Side             string       `json:"side"`
	PositionSide     PositionSide `json:"positionSide"`
	Type             string       `json:"type"`
	CreateTime       int64        `json:"time"`
}

// SymbolVolume은 심볼의 거래량 정보를 표현합니다
type SymbolVolume struct {
	Symbol      string  `json:"symbol"`
	QuoteVolume float64 `json:"quoteVolume,string"`
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

// 임베드 색상 상수
const (
	ColorSuccess = 0x00FF00 // 초록색
	ColorError   = 0xFF0000 // 빨간색
	ColorInfo    = 0x0099FF // 파란색
)

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
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s *signal.Signal) error {
	var title, emoji string
	var color int

	switch s.Type {
	case signal.Long:
		emoji = "🚀"
		title = "LONG"
		color = ColorSuccess
	case signal.Short:
		emoji = "🔻"
		title = "SHORT"
		color = ColorError
	default:
		emoji = "⚠️"
		title = "NO SIGNAL"
		color = ColorInfo
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
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			s.StopLoss,
			(s.StopLoss-s.Price)/s.Price*100,
			s.TakeProfit,
			(s.TakeProfit-s.Price)/s.Price*100,
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
		SetColor(ColorError).
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
		SetColor(ColorInfo).
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
			"**포지션**: %s\n**수량**: %.8f\n**가격**: $%.2f\n**손절가**: $%.2f\n**목표가**: $%.2f",
			info.PositionType, info.Quantity, info.EntryPrice, info.StopLoss, info.TakeProfit,
		)).
		SetColor(notification.GetColorForPosition(info.PositionType)).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.tradeWebhook, msg)
}

// getColorForSignal은 시그널 타입에 따른 색상을 반환합니다
func getColorForSignal(signalType notification.SignalType) int {
	switch signalType {
	case notification.SignalLong:
		return ColorSuccess
	case notification.SignalShort:
		return ColorError
	default:
		return ColorInfo
	}
}

```
## internal/notification/types.go
```go
package notification

import "time"

// SignalType은 트레이딩 시그널 종류를 정의합니다
type SignalType string

const (
	SignalLong   SignalType = "LONG"
	SignalShort  SignalType = "SHORT"
	SignalClose  SignalType = "CLOSE"
	ColorSuccess            = 0x00FF00
	ColorError              = 0xFF0000
	ColorInfo               = 0x0000FF
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
	Symbol       string
	PositionType string // "LONG" or "SHORT"
	Quantity     float64
	EntryPrice   float64
	StopLoss     float64
	TakeProfit   float64
}

// getColorForPosition은 포지션 타입에 따른 색상을 반환합니다
func GetColorForPosition(positionType string) int {
	switch positionType {
	case "LONG":
		return ColorSuccess
	case "SHORT":
		return ColorError
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
