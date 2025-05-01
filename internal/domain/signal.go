package domain

import "time"

// SignalInterface는 모든 시그널 타입이 구현해야 하는 인터페이스입니다
type SignalInterface interface {
	// 기본 정보 조회 메서드
	GetType() SignalType
	GetSymbol() string
	GetPrice() float64
	GetTimestamp() time.Time
	GetStopLoss() float64
	GetTakeProfit() float64

	// 유효성 검사
	IsValid() bool

	// 알림 데이터 변환 - 각 전략별 구현체에서 구체적으로 구현
	ToNotificationData() map[string]interface{}

	GetCondition(key string) (interface{}, bool)
	SetCondition(key string, value interface{})
	GetAllConditions() map[string]interface{}
}

// BaseSignal은 모든 시그널 구현체가 공유하는 기본 필드와 메서드를 제공합니다
type BaseSignal struct {
	Type       SignalType
	Symbol     string
	Price      float64
	Timestamp  time.Time
	StopLoss   float64
	TakeProfit float64
	Conditions map[string]interface{}
}

/// 생성자
func NewBaseSignal(signalType SignalType, symbol string, price float64, timestamp time.Time, stopLoss, takeProfit float64) BaseSignal {
	return BaseSignal{
		Type:       signalType,
		Symbol:     symbol,
		Price:      price,
		Timestamp:  timestamp,
		StopLoss:   stopLoss,
		TakeProfit: takeProfit,
		Conditions: make(map[string]interface{}),
	}
}

// GetType은 시그널 타입을 반환합니다
func (s *BaseSignal) GetType() SignalType {
	return s.Type
}

// GetSymbol은 시그널의 심볼을 반환합니다
func (s *BaseSignal) GetSymbol() string {
	return s.Symbol
}

// GetPrice는 시그널의 가격을 반환합니다
func (s *BaseSignal) GetPrice() float64 {
	return s.Price
}

// GetTimestamp는 시그널의 생성 시간을 반환합니다
func (s *BaseSignal) GetTimestamp() time.Time {
	return s.Timestamp
}

// GetStopLoss는 시그널의 손절가를 반환합니다
func (s *BaseSignal) GetStopLoss() float64 {
	return s.StopLoss
}

// GetTakeProfit는 시그널의 익절가를 반환합니다
func (s *BaseSignal) GetTakeProfit() float64 {
	return s.TakeProfit
}

// IsValid는 시그널이 유효한지 확인합니다
func (s *BaseSignal) IsValid() bool {
	return s.Type != NoSignal && s.Symbol != "" && s.Price > 0
}

// ToNotificationData는 알림 시스템에서 사용할 기본 데이터를 반환합니다
// 구체적인 시그널 구현체에서 오버라이딩해야 합니다
func (s *BaseSignal) ToNotificationData() map[string]interface{} {
	data := map[string]interface{}{
		"Type":       s.Type.String(),
		"Symbol":     s.Symbol,
		"Price":      s.Price,
		"Timestamp":  s.Timestamp.Format("2006-01-02 15:04:05"),
		"StopLoss":   s.StopLoss,
		"TakeProfit": s.TakeProfit,
	}

	// 조건 정보 추가
	if s.Conditions != nil {
		for k, v := range s.Conditions {
			data[k] = v
		}
	}

	return data
}

// GetCondition는 특정 키의 조건 값을 반환합니다
func (s *BaseSignal) GetCondition(key string) (interface{}, bool) {
	if s.Conditions == nil {
		return nil, false
	}
	value, exists := s.Conditions[key]
	return value, exists
}

// SetCondition는 특정 키에 조건 값을 설정합니다
func (s *BaseSignal) SetCondition(key string, value interface{}) {
	if s.Conditions == nil {
		s.Conditions = make(map[string]interface{})
	}
	s.Conditions[key] = value
}

// GetAllConditions는 모든 조건을 맵으로 반환합니다
func (s *BaseSignal) GetAllConditions() map[string]interface{} {
	// 원본 맵의 복사본 반환
	if s.Conditions == nil {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	for k, v := range s.Conditions {
		result[k] = v
	}
	return result
}

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
