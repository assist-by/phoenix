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

	n := len(prices)
	high := make([]float64, n)
	low := make([]float64, n)
	close := make([]float64, n)

	// PriceData에서 배열 추출
	for i, price := range prices {
		high[i] = price.High
		low[i] = price.Low
		close[i] = price.Close
	}

	// 결과 배열 초기화
	result := make([]Result, n)

	// 계산 로직 시작
	psar := make([]float64, n)
	psarUp := make([]float64, n)
	psarDown := make([]float64, n)

	// 초기값 설정 (첫 두 값은 NaN으로 설정)
	for i := 0; i < 2; i++ {
		result[i] = SARResult{
			SAR:       math.NaN(),
			IsLong:    false, // 첫 포인트는 추세를 결정할 수 없음
			Timestamp: prices[i].Time,
		}
		psar[i] = close[i]
		psarUp[i] = math.NaN()
		psarDown[i] = math.NaN()
	}

	// 초기 추세 설정 - 두 번째 캔들이 첫 번째보다 높으면 상승 추세
	upTrend := close[1] > close[0]

	accelerationFactor := s.AccelerationInitial
	upTrendHigh := high[0]
	downTrendLow := low[0]

	// 메인 계산 루프
	for i := 2; i < n; i++ {
		reversal := false
		maxHigh := high[i]
		minLow := low[i]

		if upTrend {
			// 상승 추세 계산
			psar[i] = psar[i-1] + (accelerationFactor * (upTrendHigh - psar[i-1]))

			// 반전 체크
			if minLow < psar[i] {
				reversal = true
				psar[i] = upTrendHigh
				downTrendLow = minLow
				accelerationFactor = s.AccelerationInitial
			} else {
				// 새로운 고점 갱신 시 가속 계수 증가
				if maxHigh > upTrendHigh {
					upTrendHigh = maxHigh
					accelerationFactor = math.Min(accelerationFactor+s.AccelerationInitial, s.AccelerationMax)
				}

				// 이전 저점으로 SAR 조정 (캔들 두 개)
				low1 := low[i-1]
				low2 := low[i-2]
				if low2 < psar[i] {
					psar[i] = low2
				} else if low1 < psar[i] {
					psar[i] = low1
				}
			}
		} else {
			// 하락 추세 계산
			psar[i] = psar[i-1] - (accelerationFactor * (psar[i-1] - downTrendLow))

			// 반전 체크
			if maxHigh > psar[i] {
				reversal = true
				psar[i] = downTrendLow
				upTrendHigh = maxHigh
				accelerationFactor = s.AccelerationInitial
			} else {
				// 새로운 저점 갱신 시 가속 계수 증가
				if minLow < downTrendLow {
					downTrendLow = minLow
					accelerationFactor = math.Min(accelerationFactor+s.AccelerationInitial, s.AccelerationMax)
				}

				// 이전 고점으로 SAR 조정 (캔들 두 개)
				high1 := high[i-1]
				high2 := high[i-2]
				if high2 > psar[i] {
					psar[i] = high2
				} else if high1 > psar[i] {
					psar[i] = high1
				}
			}
		}

		// XOR 연산으로 추세 방향 업데이트
		upTrend = upTrend != reversal

		// SAR 값 추세 방향에 따라 저장
		if upTrend {
			psarUp[i] = psar[i]
			psarDown[i] = math.NaN()
		} else {
			psarDown[i] = psar[i]
			psarUp[i] = math.NaN()
		}

		// 결과 설정
		result[i] = SARResult{
			SAR:       psar[i],
			IsLong:    upTrend,
			Timestamp: prices[i].Time,
		}
	}

	return result, nil
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
