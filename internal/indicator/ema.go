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
