package macdsarema

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/strategy"
)

// MACDSAREMASignal은 MACD+SAR+EMA 전략에 특화된 시그널 구현체입니다
type MACDSAREMASignal struct {
	domain.BaseSignal // 기본 시그널 필드와 메서드 상속

	// MACD+SAR+EMA 전략 특화 필드
	EMAValue    float64 // 200 EMA 값
	EMAAbove    bool    // 가격이 EMA 위에 있는지 여부
	MACDValue   float64 // MACD 라인 값
	SignalValue float64 // 시그널 라인 값
	Histogram   float64 // 히스토그램 값
	MACDCross   int     // MACD 크로스 상태 (1: 상향돌파, -1: 하향돌파, 0: 크로스 없음)
	SARValue    float64 // SAR 값
	SARBelow    bool    // SAR이 캔들 아래에 있는지 여부
}

// NewMACDSAREMASignal은 기본 필드로 새 MACDSAREMASignal을 생성합니다
func NewMACDSAREMASignal(
	signalType domain.SignalType,
	symbol string,
	price float64,
	timestamp time.Time,
	stopLoss float64,
	takeProfit float64,
) *MACDSAREMASignal {
	return &MACDSAREMASignal{
		BaseSignal: domain.BaseSignal{
			Type:       signalType,
			Symbol:     symbol,
			Price:      price,
			Timestamp:  timestamp,
			StopLoss:   stopLoss,
			TakeProfit: takeProfit,
		},
	}
}

// CreateFromStrategySignal은 기존 strategy.Signal에서 MACDSAREMASignal을 생성합니다
func CreateFromStrategySignal(s *domain.Signal) *MACDSAREMASignal {
	if s == nil {
		return nil
	}

	macdSignal := NewMACDSAREMASignal(
		s.Type,
		s.Symbol,
		s.Price,
		s.Timestamp,
		s.StopLoss,
		s.TakeProfit,
	)

	// 기존 Conditions 맵에서 필요한 데이터 추출
	if s.Conditions.EMAValue != 0 {
		macdSignal.EMAValue = s.Conditions.EMAValue
	}
	macdSignal.EMAAbove = s.Conditions.EMALong

	if s.Conditions.MACDValue != 0 {
		macdSignal.MACDValue = s.Conditions.MACDValue
	}
	if s.Conditions.SignalValue != 0 {
		macdSignal.SignalValue = s.Conditions.SignalValue
	}
	if s.Conditions.SARValue != 0 {
		macdSignal.SARValue = s.Conditions.SARValue
	}

	macdSignal.SARBelow = s.Conditions.SARLong
	macdSignal.Histogram = macdSignal.MACDValue - macdSignal.SignalValue

	// MACD 크로스 상태 결정
	if s.Conditions.MACDLong {
		macdSignal.MACDCross = 1 // 상향돌파
	} else if s.Conditions.MACDShort {
		macdSignal.MACDCross = -1 // 하향돌파
	} else {
		macdSignal.MACDCross = 0 // 크로스 없음
	}

	return macdSignal
}

// CreateFromConditions은 전략 분석 시 생성된 조건 맵에서 MACDSAREMASignal을 생성합니다
func CreateFromConditions(
	signalType domain.SignalType,
	symbol string,
	price float64,
	timestamp time.Time,
	stopLoss float64,
	takeProfit float64,
	conditions map[string]interface{},
) *MACDSAREMASignal {
	macdSignal := NewMACDSAREMASignal(
		signalType,
		symbol,
		price,
		timestamp,
		stopLoss,
		takeProfit,
	)

	// conditions 맵에서 값 추출
	if val, ok := conditions["EMAValue"].(float64); ok {
		macdSignal.EMAValue = val
	}
	if val, ok := conditions["EMALong"].(bool); ok {
		macdSignal.EMAAbove = val
	}
	if val, ok := conditions["MACDValue"].(float64); ok {
		macdSignal.MACDValue = val
	}
	if val, ok := conditions["SignalValue"].(float64); ok {
		macdSignal.SignalValue = val
	}
	if val, ok := conditions["SARValue"].(float64); ok {
		macdSignal.SARValue = val
	}
	if val, ok := conditions["SARLong"].(bool); ok {
		macdSignal.SARBelow = val
	}

	// 히스토그램 계산
	macdSignal.Histogram = macdSignal.MACDValue - macdSignal.SignalValue

	// MACD 크로스 상태 결정
	if val, ok := conditions["MACDLong"].(bool); ok && val {
		macdSignal.MACDCross = 1 // 상향돌파
	} else if val, ok := conditions["MACDShort"].(bool); ok && val {
		macdSignal.MACDCross = -1 // 하향돌파
	} else {
		macdSignal.MACDCross = 0 // 크로스 없음
	}

	return macdSignal
}

// ToStrategySignal은 MACDSAREMASignal을 기존 strategy.Signal로 변환합니다
// 이 메서드는 마이그레이션 기간 동안 호환성을 위해 사용됩니다
func (s *MACDSAREMASignal) ToStrategySignal() *strategy.Signal {
	conditions := map[string]interface{}{
		"EMALong":     s.EMAAbove,
		"EMAShort":    !s.EMAAbove,
		"MACDLong":    s.MACDCross > 0,
		"MACDShort":   s.MACDCross < 0,
		"SARLong":     s.SARBelow,
		"SARShort":    !s.SARBelow,
		"EMAValue":    s.EMAValue,
		"MACDValue":   s.MACDValue,
		"SignalValue": s.SignalValue,
		"SARValue":    s.SARValue,
	}

	return &strategy.Signal{
		Type:       s.Type,
		Symbol:     s.Symbol,
		Price:      s.Price,
		Timestamp:  s.Timestamp,
		StopLoss:   s.StopLoss,
		TakeProfit: s.TakeProfit,
		Conditions: conditions,
	}
}

// ToNotificationData는 MACD+SAR+EMA 전략에 특화된 알림 데이터를 반환합니다
func (s *MACDSAREMASignal) ToNotificationData() map[string]interface{} {
	data := s.BaseSignal.ToNotificationData() // 기본 필드 가져오기

	// MACD+SAR+EMA 특화 필드 추가
	data["EMA 값"] = fmt.Sprintf("%.5f", s.EMAValue)
	data["EMA 상태"] = getAboveBelowText(s.EMAAbove)
	data["MACD 값"] = fmt.Sprintf("%.5f", s.MACDValue)
	data["시그널 값"] = fmt.Sprintf("%.5f", s.SignalValue)
	data["히스토그램"] = fmt.Sprintf("%.5f", s.Histogram)
	data["SAR 값"] = fmt.Sprintf("%.5f", s.SARValue)
	data["SAR 상태"] = getSARText(s.SARBelow)
	data["MACD 크로스"] = getMACDCrossText(s.MACDCross)

	return data
}

// 표시용 헬퍼 함수들
func getAboveBelowText(above bool) string {
	if above {
		return "가격이 EMA 위"
	}
	return "가격이 EMA 아래"
}

func getSARText(below bool) string {
	if below {
		return "SAR이 캔들 아래"
	}
	return "SAR이 캔들 위"
}

func getMACDCrossText(cross int) string {
	switch cross {
	case 1:
		return "상향돌파"
	case -1:
		return "하향돌파"
	default:
		return "크로스 없음"
	}
}
