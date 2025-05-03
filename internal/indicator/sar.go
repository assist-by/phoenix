// internal/indicator/sar.go
package indicator

import (
	"fmt"
	"math"
	"time"
)

/* -------------------- 결과 타입 -------------------- */

// SARResult 는 Parabolic SAR 한 개 지점의 계산 결과
type SARResult struct {
	SAR       float64   // SAR 값
	IsLong    bool      // 현재 상승 추세 여부
	Timestamp time.Time // 캔들 시각
}

func (r SARResult) GetTimestamp() time.Time { return r.Timestamp }

/* -------------------- 본체 -------------------- */

// SAR 은 Parabolic SAR 지표를 구현
type SAR struct {
	BaseIndicator
	AccelerationInitial float64 // AF(가속도) 시작값
	AccelerationMax     float64 // AF 최대값
}

// 새 인스턴스
func NewSAR(step, max float64) *SAR {
	return &SAR{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("SAR(%.2f,%.2f)", step, max),
			Config: map[string]interface{}{
				"AccelerationInitial": step,
				"AccelerationMax":     max,
			},
		},
		AccelerationInitial: step,
		AccelerationMax:     max,
	}
}

// 기본 파라미터(0.02, 0.2)
func NewDefaultSAR() *SAR { return NewSAR(0.02, 0.2) }

/* -------------------- 핵심 계산 -------------------- */

func (s *SAR) Calculate(prices []PriceData) ([]Result, error) {
	if err := s.validateInput(prices); err != nil {
		return nil, err
	}

	out := make([]Result, len(prices))

	// ---------- ① 초기 추세 및 SAR 결정 ----------
	isLong := prices[1].Close >= prices[0].Close // 첫 두 캔들로 방향 판정
	var sar, ep float64
	if isLong {
		// TV 방식: 직전 Low 하나만 사용
		sar = prices[0].Low
		ep = prices[1].High
	} else {
		sar = prices[0].High
		ep = prices[1].Low
	}
	af := s.AccelerationInitial

	// 첫 캔들은 NaN, 두 번째는 계산값
	out[0] = SARResult{SAR: math.NaN(), IsLong: isLong, Timestamp: prices[0].Time}
	out[1] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[1].Time}

	// ---------- ② 메인 루프 ----------
	for i := 2; i < len(prices); i++ {
		prevSAR := sar
		// 기본 파라볼릭 공식
		sar = prevSAR + af*(ep-prevSAR)

		if isLong {
			// 상승일 때: 직전 2개 Low 이하로 clamp
			sar = s.Min(sar, prices[i-1].Low, prices[i-2].Low)

			// 새로운 고점 나오면 EP·AF 갱신
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			// ★ 추세 전환 조건(포함 비교) ★
			if sar >= prices[i].Low {
				// 반전 → 하락
				isLong = false
				sar = ep           // 반전 시 SAR = 직전 EP
				ep = prices[i].Low // 새 EP
				af = s.AccelerationInitial

				// TV와 동일하게 즉시 clamp
				sar = s.Max(sar, prices[i-1].High, prices[i-2].High)
			}
		} else { // 하락 추세
			// 하락일 때: 직전 2개 High 이상으로 clamp
			sar = s.Max(sar, prices[i-1].High, prices[i-2].High)

			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+s.AccelerationInitial, s.AccelerationMax)
			}

			if sar <= prices[i].High { // ★ 포함 비교 ★
				// 반전 → 상승
				isLong = true
				sar = ep
				ep = prices[i].High
				af = s.AccelerationInitial

				// 즉시 clamp
				sar = s.Min(sar, prices[i-1].Low, prices[i-2].Low)
			}
		}

		out[i] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[i].Time}
	}

	// /* ---------- ③ (선택) TV와 동일하게 한 캔들 우측으로 밀기 ----------
	//    TradingView 는 t-1 시점에 계산한 SAR 을 t 캔들 밑에 찍는다.
	//    차트에 그대로 맞추고 싶다면 주석 해제.

	for i := len(out) - 1; i > 0; i-- {
		out[i] = out[i-1]
	}
	out[0] = SARResult{SAR: math.NaN(), IsLong: out[1].(SARResult).IsLong, Timestamp: prices[0].Time}
	// ------------------------------------------------------------------ */

	return out, nil
}

/* -------------------- 입력 검증 -------------------- */

func (s *SAR) validateInput(prices []PriceData) error {
	switch {
	case len(prices) < 3:
		return &ValidationError{Field: "prices", Err: fmt.Errorf("가격 데이터가 최소 3개 필요합니다")}
	case s.AccelerationInitial <= 0 || s.AccelerationMax <= 0:
		return &ValidationError{Field: "acceleration", Err: fmt.Errorf("가속도(step/max)는 0보다 커야 합니다")}
	case s.AccelerationInitial > s.AccelerationMax:
		return &ValidationError{Field: "acceleration", Err: fmt.Errorf("AccelerationMax는 AccelerationInitial 이상이어야 합니다")}
	}
	return nil
}

/* -------------------- 보조: 다중 min/max -------------------- */

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
