// internal/indicator/rsi.go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// RSIResult는 RSI 지표 계산 결과
type RSIResult struct {
	Value     float64   // RSI 값 (0–100, 계산 불가 구간은 math.NaN())
	AvgGain   float64   // 평균 이득
	AvgLoss   float64   // 평균 손실
	Timestamp time.Time // 계산 시점
}

// GetTimestamp는 결과의 타임스탬프를 반환
func (r RSIResult) GetTimestamp() time.Time { return r.Timestamp }

// RSI는 Relative Strength Index 지표를 구현
type RSI struct {
	BaseIndicator
	Period int // RSI 계산 기간
}

// NewRSI는 새로운 RSI 지표 인스턴스를 생성
func NewRSI(period int) *RSI {
	return &RSI{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("RSI(%d)", period),
			Config: map[string]interface{}{
				"Period": period,
			},
		},
		Period: period,
	}
}

// Calculate는 주어진 가격 데이터에 대해 RSI를 계산
func (r *RSI) Calculate(prices []PriceData) ([]Result, error) {
	if err := r.validateInput(prices); err != nil {
		return nil, err
	}

	p := r.Period
	results := make([]Result, len(prices))

	// ---------- 1. 첫 p 개의 변동 Δ 합산 (SMA) ----------------------------
	sumGain, sumLoss := 0.0, 0.0
	for i := 1; i <= p; i++ { // i <= p  ⬅ off-by-one 수정
		delta := prices[i].Close - prices[i-1].Close
		if delta > 0 {
			sumGain += delta
		} else {
			sumLoss += -delta
		}
	}
	avgGain, avgLoss := sumGain/float64(p), sumLoss/float64(p)
	results[p] = toRSI(avgGain, avgLoss, prices[p].Time)

	// ---------- 2. 이후 구간 Wilder EMA 방식 ----------------------------
	for i := p + 1; i < len(prices); i++ {
		delta := prices[i].Close - prices[i-1].Close
		gain, loss := 0.0, 0.0
		if delta > 0 {
			gain = delta
		} else {
			loss = -delta
		}

		avgGain = (avgGain*float64(p-1) + gain) / float64(p)
		avgLoss = (avgLoss*float64(p-1) + loss) / float64(p)
		results[i] = toRSI(avgGain, avgLoss, prices[i].Time)
	}

	// ---------- 3. 앞 구간(NaN) 표시 ------------------------------------
	for i := 0; i < p; i++ {
		results[i] = RSIResult{
			Value:     math.NaN(),
			AvgGain:   math.NaN(),
			AvgLoss:   math.NaN(),
			Timestamp: prices[i].Time,
		}
	}
	return results, nil
}

// --- 유틸 ---------------------------------------------------------------

func toRSI(avgGain, avgLoss float64, ts time.Time) RSIResult {
	var rsi float64
	switch {
	case avgGain == 0 && avgLoss == 0:
		rsi = 50 // 완전 횡보
	case avgLoss == 0:
		rsi = 100
	default:
		rs := avgGain / avgLoss
		rsi = 100 - 100/(1+rs)
	}
	return RSIResult{Value: rsi, AvgGain: avgGain, AvgLoss: avgLoss, Timestamp: ts}
}

func (r *RSI) validateInput(prices []PriceData) error {
	if r.Period <= 0 {
		return &ValidationError{Field: "period", Err: fmt.Errorf("period must be > 0")}
	}
	if len(prices) == 0 {
		return &ValidationError{Field: "prices", Err: fmt.Errorf("가격 데이터가 비어있습니다")}
	}
	if len(prices) <= r.Period {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d+1, 현재: %d", r.Period, len(prices)),
		}
	}
	return nil
}
