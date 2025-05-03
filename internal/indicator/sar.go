package indicator

import (
	"fmt"
	"math"
	"time"
)

// ---------------- 결과 --------------------------------------------------

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

// ---------------- 본체 --------------------------------------------------
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

	out := make([]Result, len(prices))

	// ---------- ① 초기 추세 결정 -------------------------------------
	isLong := prices[1].Close >= prices[0].Close
	var sar, ep float64
	if isLong {
		sar = math.Min(prices[0].Low, prices[1].Low)  // 초기 SAR: 이전 두 Low 중 작은 값
		ep = math.Max(prices[0].High, prices[1].High) // EP: 이전 두 High 중 큰 값
	} else {
		sar = math.Max(prices[0].High, prices[1].High) // 초기 SAR: 이전 두 High 중 큰 값
		ep = math.Min(prices[0].Low, prices[1].Low)    // EP: 이전 두 Low 중 작은 값
	}
	af := s.AccelerationInitial

	// 첫 두 캔들은 “계산 불가” → NaN
	out[0] = SARResult{SAR: math.NaN(), IsLong: isLong, Timestamp: prices[0].Time}
	out[1] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[1].Time}

	// ---------- ② 메인 루프 ------------------------------------------
	for i := 2; i < len(prices); i++ {
		prevSAR := sar
		// 기본 파라볼릭 식
		sar = prevSAR + af*(ep-prevSAR)

		if isLong {
			// SAR은 직전 2개 Low 이하로 제한
			sar = s.Min(sar, prices[i-1].Low, prices[i-2].Low)

			// EP·AF 업데이트
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// 추세 전환 체크
			if sar > prices[i].Low {
				isLong = false
				sar = ep                   // 반전 시 SAR은 직전 EP
				ep = prices[i].Low         // 새 EP
				af = s.AccelerationInitial // AF 리셋
			}
		} else { // 현재 하락 추세
			// SAR은 직전 2개 High 이상으로 제한
			sar = s.Max(sar, prices[i-1].High, prices[i-2].High)

			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			if sar < prices[i].High {
				isLong = true
				sar = ep
				ep = prices[i].High
				af = s.AccelerationInitial
			}
		}

		out[i] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[i].Time}
	}
	return out, nil
}

// validateInput은 입력 데이터가 유효한지 검증합니다
func (s *SAR) validateInput(prices []PriceData) error {
	switch {
	case len(prices) < 3:
		return &ValidationError{Field: "prices", Err: fmt.Errorf("가격 데이터가 최소 3개 필요합니다")}
	case s.AccelerationInitial <= 0 || s.AccelerationMax <= 0:
		return &ValidationError{Field: "acceleration", Err: fmt.Errorf("가속도 값은 0보다 커야 합니다")}
	case s.AccelerationInitial > s.AccelerationMax:
		return &ValidationError{Field: "acceleration", Err: fmt.Errorf("AccelerationMax는 AccelerationInitial 이상이어야 합니다")}
	}
	return nil
}

// ---------- 보조: 다중 min/max -----------------------------------------

func (SAR) Min(vals ...float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
func (SAR) Max(vals ...float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
