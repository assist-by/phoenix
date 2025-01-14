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
	if err := ValidateSAROption(opt); err != nil {
		return nil, err
	}

	if len(prices) < 2 {
		return nil, &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("최소 2개의 가격 데이터가 필요합니다"),
		}
	}

	results := make([]SARResult, len(prices))

	// 초기값 설정
	isLong := prices[1].Close > prices[0].Close
	extremePoint := 0.0
	af := opt.AccelerationInitial
	sar := 0.0

	if isLong {
		sar = prices[0].Low
		extremePoint = prices[1].High
	} else {
		sar = prices[0].High
		extremePoint = prices[1].Low
	}

	// 첫 번째 결과 저장
	results[0] = SARResult{
		SAR:       sar,
		IsLong:    isLong,
		Timestamp: prices[0].Time,
	}

	// SAR 계산
	for i := 1; i < len(prices); i++ {
		prevSAR := sar

		// SAR 값 계산
		sar = prevSAR + af*(extremePoint-prevSAR)

		// 현재 추세가 상승일 때
		if isLong {
			// SAR는 이전 두 봉의 저점보다 높을 수 없음
			if i > 1 {
				minLow := math.Min(prices[i-1].Low, prices[i].Low)
				if sar > minLow {
					sar = minLow
				}
			}

			// 새로운 고점 발견 시 가속도 증가
			if prices[i].High > extremePoint {
				extremePoint = prices[i].High
				af = math.Min(af+opt.AccelerationInitial, opt.AccelerationMax)
			}

			// 추세 전환 확인
			if prices[i].Low < sar {
				isLong = false
				sar = extremePoint
				extremePoint = prices[i].Low
				af = opt.AccelerationInitial
			}
		} else {
			// SAR는 이전 두 봉의 고점보다 낮을 수 없음
			if i > 1 {
				maxHigh := math.Max(prices[i-1].High, prices[i].High)
				if sar < maxHigh {
					sar = maxHigh
				}
			}

			// 새로운 저점 발견 시 가속도 증가
			if prices[i].Low < extremePoint {
				extremePoint = prices[i].Low
				af = math.Min(af+opt.AccelerationInitial, opt.AccelerationMax)
			}

			// 추세 전환 확인
			if prices[i].High > sar {
				isLong = true
				sar = extremePoint
				extremePoint = prices[i].High
				af = opt.AccelerationInitial
			}
		}

		// 결과 저장
		results[i] = SARResult{
			SAR:       sar,
			IsLong:    isLong,
			Timestamp: prices[i].Time,
		}
	}

	return results, nil
}
