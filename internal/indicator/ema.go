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
