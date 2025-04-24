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
