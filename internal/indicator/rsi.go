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
	Period  int  // RSI 계산 기간
	FillNaN bool // NaN 값을 채울지 여부
}

// NewRSI는 새로운 RSI 지표 인스턴스를 생성
func NewRSI(period int, fillNaN bool) *RSI {
	return &RSI{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("RSI(%d)", period),
			Config: map[string]interface{}{
				"Period":  period,
				"FillNaN": fillNaN,
			},
		},
		Period:  period,
		FillNaN: fillNaN,
	}
}

// Calculate는 주어진 가격 데이터에 대해 RSI를 계산
func (r *RSI) Calculate(prices []PriceData) ([]Result, error) {
	if err := r.validateInput(prices); err != nil {
		return nil, err
	}

	results := make([]Result, len(prices))

	// 첫 요소에 대한 NaN 설정 (차분을 구할 수 없음)
	results[0] = RSIResult{
		Value:     math.NaN(),
		AvgGain:   math.NaN(),
		AvgLoss:   math.NaN(),
		Timestamp: prices[0].Time,
	}

	// 상승/하락 분리를 위한 PriceData 생성
	gainData := make([]PriceData, len(prices)-1)
	lossData := make([]PriceData, len(prices)-1)

	for i := 1; i < len(prices); i++ {
		diff := prices[i].Close - prices[i-1].Close
		idx := i - 1

		// 상승/하락 분리
		if diff > 0 {
			gainData[idx] = PriceData{
				Time:  prices[i].Time,
				Close: diff,
			}
			lossData[idx] = PriceData{
				Time:  prices[i].Time,
				Close: 0,
			}
		} else {
			gainData[idx] = PriceData{
				Time:  prices[i].Time,
				Close: 0,
			}
			lossData[idx] = PriceData{
				Time:  prices[i].Time,
				Close: -diff,
			}
		}
	}

	// Wilder 방식의 EMA 계산 (alpha=1/period)
	gainEMA := NewWilderEMA(r.Period)
	lossEMA := NewWilderEMA(r.Period)

	// fillna 설정
	minPeriods := 0
	if !r.FillNaN {
		minPeriods = r.Period
	}

	// 설정 업데이트
	gainEMA.Config["min_periods"] = minPeriods
	lossEMA.Config["min_periods"] = minPeriods

	// EMA 계산 수행
	gainResults, _ := gainEMA.Calculate(gainData)
	lossResults, _ := lossEMA.Calculate(lossData)

	// RSI 계산
	for i := 0; i < len(gainResults); i++ {
		// 결과 인덱스
		resultIdx := i + 1

		// EMA 결과에서 값 추출
		avgGain := gainResults[i].(EMAResult).Value
		avgLoss := lossResults[i].(EMAResult).Value

		// RSI 값 계산 - Python 방식과 일치
		var rsi float64
		switch {
		case math.IsNaN(avgGain) || math.IsNaN(avgLoss):
			rsi = math.NaN()
		case avgLoss == 0:
			rsi = 100
		default:
			rs := avgGain / avgLoss
			rsi = 100 - 100/(1+rs)
		}

		// NaN 채우기
		if r.FillNaN && math.IsNaN(rsi) {
			rsi = 50
		}

		results[resultIdx] = RSIResult{
			Value:     rsi,
			AvgGain:   avgGain,
			AvgLoss:   avgLoss,
			Timestamp: prices[resultIdx].Time,
		}
	}

	return results, nil
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
