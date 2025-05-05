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

	// 미리 모든 결과를 NaN으로 초기화
	out := make([]Result, len(prices))
	for i := 0; i < len(prices); i++ {
		out[i] = MACDResult{
			MACD:      math.NaN(),
			Signal:    math.NaN(),
			Histogram: math.NaN(),
			Timestamp: prices[i].Time,
		}
	}

	// 첫 번째 유효한 MACD 값의 인덱스 (장기 EMA가 유효해지는 시점)
	firstValidIdx := m.LongPeriod - 1

	// Signal 계산용 MACD 값을 저장할 배열
	macdValues := make([]float64, len(prices)-firstValidIdx)
	macdTimes := make([]time.Time, len(prices)-firstValidIdx)

	// MACD 값 계산 (EMA 유효한 부분부터)
	for i := firstValidIdx; i < len(prices); i++ {
		short := shortEMARes[i].(EMAResult).Value
		long := longEMARes[i].(EMAResult).Value

		// 둘 다 유효한 값인지 확인
		if !math.IsNaN(short) && !math.IsNaN(long) {
			macd := short - long
			macdIdx := i - firstValidIdx

			// MACD 값 저장
			macdValues[macdIdx] = macd
			macdTimes[macdIdx] = prices[i].Time

			// MACD 라인 값 설정
			macdResult := out[i].(MACDResult)
			macdResult.MACD = macd
			out[i] = macdResult
		}
	}

	// Signal 계산 - 단순히 MACD 값의 EMA
	// 여기서 MACD 값을 PriceData로 변환
	macdPrices := make([]PriceData, len(macdValues))
	for i, value := range macdValues {
		macdPrices[i] = PriceData{
			Close: value,
			Time:  macdTimes[i],
		}
	}

	// Signal EMA 계산
	signalEMA := NewEMA(m.SignalPeriod)
	signalResults, _ := signalEMA.Calculate(macdPrices)

	// Signal 값과 Histogram 설정
	signalStartIdx := m.SignalPeriod - 1
	if signalStartIdx < 0 {
		signalStartIdx = 0
	}

	for i := signalStartIdx; i < len(signalResults); i++ {
		signalValue := signalResults[i].(EMAResult).Value
		if !math.IsNaN(signalValue) {
			realIdx := i + firstValidIdx
			if realIdx < len(out) {
				macdResult := out[realIdx].(MACDResult)
				macdResult.Signal = signalValue
				macdResult.Histogram = macdResult.MACD - signalValue
				out[realIdx] = macdResult
			}
		}
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
