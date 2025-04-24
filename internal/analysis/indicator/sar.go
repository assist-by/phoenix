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
