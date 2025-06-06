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
	Period    int     // EMA 기간
	Alpha     float64 // 명시적 알파 값 (설정 시 Period 무시)
	UseWilder bool    // Wilder의 방식 사용 여부 (alpha=1/period)
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
		Period:    period,
		UseWilder: false, // 기본값은 기존 방식 유지
	}
}

// NewWilderEMA는 Wilder 방식의 EMA 인스턴스를 생성합니다 (RSI용)
func NewWilderEMA(period int) *EMA {
	return &EMA{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("WilderEMA(%d)", period),
			Config: map[string]interface{}{
				"Period":    period,
				"UseWilder": true,
			},
		},
		Period:    period,
		UseWilder: true,
	}
}

// NewCustomEMA는 사용자 정의 알파 값을 갖는 EMA 인스턴스를 생성합니다
func NewCustomEMA(period int, alpha float64) *EMA {
	return &EMA{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("CustomEMA(alpha=%.4f)", alpha),
			Config: map[string]interface{}{
				"Period": period,
				"Alpha":  alpha,
			},
		},
		Period: period,
		Alpha:  alpha,
	}
}

// Calculate는 주어진 가격 데이터에 대해 EMA를 계산합니다
func (e *EMA) Calculate(prices []PriceData) ([]Result, error) {
	if err := e.validateInput(prices); err != nil {
		return nil, err
	}

	p := e.Period
	var alpha float64

	// 알파 값 결정
	switch {
	case e.Alpha > 0: // 명시적 알파 값이 설정된 경우
		alpha = e.Alpha
	case e.UseWilder: // Wilder 방식 (RSI용)
		alpha = 1.0 / float64(p)
	default: // 기본 span 방식 (일반 EMA, MACD용)
		alpha = 2.0 / float64(p+1)
	}

	results := make([]Result, len(prices))

	// 첫 값을 시작값으로 사용
	ema := prices[0].Close
	results[0] = EMAResult{Value: ema, Timestamp: prices[0].Time}

	// EMA 계산
	for i := 1; i < len(prices); i++ {
		ema = alpha*prices[i].Close + (1-alpha)*ema
		results[i] = EMAResult{Value: ema, Timestamp: prices[i].Time}
	}

	// 항상 최소 기간 이전의 값들을 NaN으로 설정
	for i := 0; i < p; i++ {
		results[i] = EMAResult{Value: math.NaN(), Timestamp: prices[i].Time}
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
