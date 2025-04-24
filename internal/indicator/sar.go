package indicator

import (
	"fmt"
	"math"
	"time"
)

// SARResult는 Parabolic SAR 지표 계산 결과입니다
type SARResult struct {
	SAR       float64   // SAR 값
	IsLong    bool      // 현재 추세가 상승인지 여부
	Timestamp time.Time // 계산 시점
}

// GetTimestamp는 결과의 타임스탬프를 반환합니다 (Result 인터페이스 구현)
func (r SARResult) GetTimestamp() time.Time {
	return r.Timestamp
}

// SAR은 Parabolic SAR 지표를 구현합니다
type SAR struct {
	BaseIndicator
	AccelerationInitial float64 // 초기 가속도
	AccelerationMax     float64 // 최대 가속도
}

// NewSAR는 새로운 Parabolic SAR 지표 인스턴스를 생성합니다
func NewSAR(accelerationInitial, accelerationMax float64) *SAR {
	return &SAR{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("SAR(%.2f,%.2f)", accelerationInitial, accelerationMax),
			Config: map[string]interface{}{
				"AccelerationInitial": accelerationInitial,
				"AccelerationMax":     accelerationMax,
			},
		},
		AccelerationInitial: accelerationInitial,
		AccelerationMax:     accelerationMax,
	}
}

// NewDefaultSAR는 기본 설정으로 SAR 인스턴스를 생성합니다
func NewDefaultSAR() *SAR {
	return NewSAR(0.02, 0.2)
}

// Calculate는 주어진 가격 데이터에 대해 Parabolic SAR을 계산합니다
func (s *SAR) Calculate(prices []PriceData) ([]Result, error) {
	if err := s.validateInput(prices); err != nil {
		return nil, err
	}

	results := make([]Result, len(prices))
	// 초기값 설정
	af := s.AccelerationInitial
	sar := prices[0].Low
	ep := prices[0].High
	isLong := true

	results[0] = SARResult{
		SAR:       sar,
		IsLong:    isLong,
		Timestamp: prices[0].Time,
	}

	// SAR 계산
	for i := 1; i < len(prices); i++ {
		if isLong {
			sar = sar + af*(ep-sar)

			// 새로운 고점 발견
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// 추세 전환 체크
			if sar > prices[i].Low {
				isLong = false
				sar = ep
				ep = prices[i].Low
				af = s.AccelerationInitial
			}
		} else {
			sar = sar - af*(sar-ep)

			// 새로운 저점 발견
			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// 추세 전환 체크
			if sar < prices[i].High {
				isLong = true
				sar = ep
				ep = prices[i].High
				af = s.AccelerationInitial
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

// validateInput은 입력 데이터가 유효한지 검증합니다
func (s *SAR) validateInput(prices []PriceData) error {
	if len(prices) < 2 {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("SAR 계산에는 최소 2개의 가격 데이터가 필요합니다"),
		}
	}

	return nil
}
