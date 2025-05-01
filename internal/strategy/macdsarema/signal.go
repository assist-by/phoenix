package macdsarema

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

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
		BaseSignal: domain.NewBaseSignal(
			signalType,
			symbol,
			price,
			timestamp,
			stopLoss,
			takeProfit,
		),
	}
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

// ToNotificationData는 MACD+SAR+EMA 전략에 특화된 알림 데이터를 반환합니다
func (s *MACDSAREMASignal) ToNotificationData() map[string]interface{} {
	data := s.BaseSignal.ToNotificationData() // 기본 필드 가져오기

	// 롱 조건 메시지
	longConditionValue := fmt.Sprintf(
		"%s EMA200 (가격이 EMA 위)\n%s MACD (시그널 상향돌파)\n%s SAR (SAR이 가격 아래)",
		getCheckMark(s.EMAAbove),
		getCheckMark(s.MACDCross > 0),
		getCheckMark(s.SARBelow),
	)

	// 숏 조건 메시지
	shortConditionValue := fmt.Sprintf(
		"%s EMA200 (가격이 EMA 아래)\n%s MACD (시그널 하향돌파)\n%s SAR (SAR이 가격 위)",
		getCheckMark(!s.EMAAbove),
		getCheckMark(s.MACDCross < 0),
		getCheckMark(!s.SARBelow),
	)

	// 기술적 지표 값 메시지 - 코드 블록으로 감싸기
	technicalValue := fmt.Sprintf(
		"```\n[EMA200]: %.5f\n[MACD Line]: %.5f\n[Signal Line]: %.5f\n[Histogram]: %.5f\n[SAR]: %.5f\n```",
		s.EMAValue,
		s.MACDValue,
		s.SignalValue,
		s.Histogram,
		s.SARValue,
	)

	// Discord의 embed 필드로 구성 - inline 속성 true로 설정
	fields := []map[string]interface{}{
		{
			"name":   "LONG 조건",
			"value":  longConditionValue,
			"inline": true, // 인라인으로 설정
		},
		{
			"name":   "SHORT 조건",
			"value":  shortConditionValue,
			"inline": true, // 인라인으로 설정
		},
		{
			"name":   "기술적 지표",
			"value":  technicalValue,
			"inline": false, // 이건 전체 폭 사용
		},
	}

	// 필드 배열을 데이터에 추가
	data["field"] = fields

	return data
}

// getCheckMark는 조건에 따라 체크마크나 X를 반환합니다
func getCheckMark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}
