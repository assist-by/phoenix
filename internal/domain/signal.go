package domain

import "time"

// SignalConditions는 시그널 발생 조건들의 상세 정보를 저장합니다
type SignalConditions struct {
	EMALong     bool    // 가격이 EMA 위
	EMAShort    bool    // 가격이 EMA 아래
	MACDLong    bool    // MACD 상향돌파
	MACDShort   bool    // MACD 하향돌파
	SARLong     bool    // SAR이 가격 아래
	SARShort    bool    // SAR이 가격 위
	EMAValue    float64 // EMA 값
	MACDValue   float64 // MACD 값
	SignalValue float64 // MACD Signal 값
	SARValue    float64 // SAR 값
}

// Signal은 생성된 시그널 정보를 담습니다
type Signal struct {
	Type       SignalType       // 시그널 유형 (Long, Short 등)
	Symbol     string           // 심볼 (예: BTCUSDT)
	Price      float64          // 현재 가격
	Timestamp  time.Time        // 시그널 생성 시간
	Conditions SignalConditions // 시그널 발생 조건 상세
	StopLoss   float64          // 손절가
	TakeProfit float64          // 익절가
}

// IsValid는 시그널이 유효한지 확인합니다
func (s *Signal) IsValid() bool {
	return s.Type != NoSignal && s.Symbol != "" && s.Price > 0
}

// IsLong은 시그널이 롱 포지션인지 확인합니다
func (s *Signal) IsLong() bool {
	return s.Type == Long
}

// IsShort은 시그널이 숏 포지션인지 확인합니다
func (s *Signal) IsShort() bool {
	return s.Type == Short
}

// IsPending은 시그널이 대기 상태인지 확인합니다
func (s *Signal) IsPending() bool {
	return s.Type == PendingLong || s.Type == PendingShort
}
