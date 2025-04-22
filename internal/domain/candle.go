package domain

import "time"

// Candle은 캔들 데이터를 표현합니다
type Candle struct {
	OpenTime  time.Time    // 캔들 시작 시간
	CloseTime time.Time    // 캔들 종료 시간
	Open      float64      // 시가
	High      float64      // 고가
	Low       float64      // 저가
	Close     float64      // 종가
	Volume    float64      // 거래량
	Symbol    string       // 심볼 (예: BTCUSDT)
	Interval  TimeInterval // 시간 간격 (예: 15m, 1h)
}

// CandleList는 캔들 데이터 목록입니다
type CandleList []Candle

// GetLastCandle은 가장 최근 캔들을 반환합니다
func (cl CandleList) GetLastCandle() (Candle, bool) {
	if len(cl) == 0 {
		return Candle{}, false
	}
	return cl[len(cl)-1], true
}

// GetPriceAtIndex는 특정 인덱스의 가격을 반환합니다
func (cl CandleList) GetPriceAtIndex(index int) (float64, bool) {
	if index < 0 || index >= len(cl) {
		return 0, false
	}
	return cl[index].Close, true
}

// GetSubList는 지정된 범위의 부분 리스트를 반환합니다
func (cl CandleList) GetSubList(start, end int) (CandleList, bool) {
	if start < 0 || end > len(cl) || start >= end {
		return nil, false
	}
	return cl[start:end], true
}
