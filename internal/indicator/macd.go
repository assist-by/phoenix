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
