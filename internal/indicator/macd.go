package indicator

import (
	"fmt"
	"time"
)

// MACDResult는 MACD 지표 계산 결과입니다
type MACDResult struct {
	MACD      float64   // MACD 라인
	Signal    float64   // 시그널 라인
	Histogram float64   // 히스토그램
	Timestamp time.Time // 계산 시점
}

// GetTimestamp는 결과의 타임스탬프를 반환합니다 (Result 인터페이스 구현)
func (r MACDResult) GetTimestamp() time.Time {
	return r.Timestamp
}

// MACD는 Moving Average Convergence Divergence 지표를 구현합니다
type MACD struct {
	BaseIndicator
	ShortPeriod  int // 단기 EMA 기간
	LongPeriod   int // 장기 EMA 기간
	SignalPeriod int // 시그널 라인 기간
}

// NewMACD는 새로운 MACD 지표 인스턴스를 생성합니다
func NewMACD(shortPeriod, longPeriod, signalPeriod int) *MACD {
	return &MACD{
		BaseIndicator: BaseIndicator{
			Name: fmt.Sprintf("MACD(%d,%d,%d)", shortPeriod, longPeriod, signalPeriod),
			Config: map[string]interface{}{
				"ShortPeriod":  shortPeriod,
				"LongPeriod":   longPeriod,
				"SignalPeriod": signalPeriod,
			},
		},
		ShortPeriod:  shortPeriod,
		LongPeriod:   longPeriod,
		SignalPeriod: signalPeriod,
	}
}

// Calculate는 주어진 가격 데이터에 대해 MACD를 계산합니다
func (m *MACD) Calculate(prices []PriceData) ([]Result, error) {
	if err := m.validateInput(prices); err != nil {
		return nil, err
	}

	// 단기 EMA 계산
	shortEMA := NewEMA(m.ShortPeriod)
	shortEMAResults, err := shortEMA.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("단기 EMA 계산 실패: %w", err)
	}

	// 장기 EMA 계산
	longEMA := NewEMA(m.LongPeriod)
	longEMAResults, err := longEMA.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("장기 EMA 계산 실패: %w", err)
	}

	// MACD 라인 계산 (단기 EMA - 장기 EMA)
	macdStartIdx := m.LongPeriod - 1
	macdLine := make([]PriceData, len(prices)-macdStartIdx)

	// 이 부분에 타입 안전성 확보를 위한 체크 추가
	for i := range macdLine {
		idx := i + macdStartIdx
		// nil 체크 추가
		if shortEMAResults[idx] == nil || longEMAResults[idx] == nil {
			continue // nil인 경우 건너뛰기
		}

		// 안전한 타입 변환
		shortVal, ok1 := shortEMAResults[idx].(EMAResult)
		longVal, ok2 := longEMAResults[idx].(EMAResult)

		if !ok1 || !ok2 {
			return nil, fmt.Errorf("EMA 결과 타입 변환 실패 (인덱스: %d)", idx)
		}

		macdLine[i] = PriceData{
			Time:  prices[idx].Time,
			Close: shortVal.Value - longVal.Value,
		}
	}

	// 시그널 라인 계산 (MACD의 EMA)
	signalEMA := NewEMA(m.SignalPeriod)
	signalEMAResults, err := signalEMA.Calculate(macdLine)
	if err != nil {
		return nil, fmt.Errorf("시그널 라인 계산 실패: %w", err)
	}

	// 최종 결과 생성
	resultStartIdx := m.SignalPeriod - 1
	results := make([]Result, len(prices))

	// 결과가 없는 초기 인덱스는 nil로 설정
	for i := 0; i < macdStartIdx+resultStartIdx; i++ {
		results[i] = nil
	}

	// 실제 MACD 결과 설정
	for i := 0; i < len(macdLine)-resultStartIdx; i++ {
		resultIdx := i + macdStartIdx + resultStartIdx

		// nil 체크 추가
		if i+resultStartIdx >= len(macdLine) || i >= len(signalEMAResults) || signalEMAResults[i] == nil {
			results[resultIdx] = nil
			continue
		}

		macdValue := macdLine[i+resultStartIdx].Close

		// 안전한 타입 변환
		signalEMAResult, ok := signalEMAResults[i].(EMAResult)
		if !ok {
			results[resultIdx] = nil
			continue
		}

		signalValue := signalEMAResult.Value

		results[resultIdx] = MACDResult{
			MACD:      macdValue,
			Signal:    signalValue,
			Histogram: macdValue - signalValue,
			Timestamp: macdLine[i+resultStartIdx].Time,
		}
	}

	return results, nil
}

// validateInput은 입력 데이터가 유효한지 검증합니다
func (m *MACD) validateInput(prices []PriceData) error {
	if len(prices) == 0 {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 비어있습니다"),
		}
	}

	minPeriod := m.LongPeriod + m.SignalPeriod - 1
	if len(prices) < minPeriod {
		return &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d, 현재: %d", minPeriod, len(prices)),
		}
	}

	return nil
}
