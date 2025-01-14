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
	macdLine := make([]PriceData, len(prices)-opt.LongPeriod+1)
	for i := 0; i < len(macdLine); i++ {
		idx := i + opt.LongPeriod - 1
		macdLine[i] = PriceData{
			Time:  prices[idx].Time,
			Close: shortEMA[idx].Value - longEMA[idx].Value,
		}
	}

	// 시그널 라인 계산 (MACD의 EMA)
	signalLineData, err := EMA(macdLine, EMAOption{Period: opt.SignalPeriod})
	if err != nil {
		return nil, fmt.Errorf("시그널 라인 계산 실패: %w", err)
	}

	// 최종 결과 생성
	results := make([]MACDResult, len(signalLineData))
	startIdx := opt.LongPeriod - 1
	for i := range results {
		macd := macdLine[i+opt.SignalPeriod-1].Close
		signal := signalLineData[i].Value
		results[i] = MACDResult{
			MACD:      macd,
			Signal:    signal,
			Histogram: macd - signal,
			Timestamp: prices[startIdx+i].Time,
		}
	}

	return results, nil
}
