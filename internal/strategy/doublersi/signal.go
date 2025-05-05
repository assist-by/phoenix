package doublersi

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// DoubleRSISignal은 더블 RSI 전략 시그널을 표현합니다
type DoubleRSISignal struct {
	domain.BaseSignal // 기본 시그널 필드와 메서드 상속

	// 더블 RSI 전략 특화 필드
	DailyRSIValue  float64 // 일봉 RSI 값 (고차)
	HourlyRSIValue float64 // 시간봉 RSI 값 (저차)
	HourlyRSIPrev  float64 // 이전 시간봉 RSI 값
	CrossUp        bool    // HourlyRSI가 상향 돌파했는지
	CrossDown      bool    // HourlyRSI가 하향 돌파했는지
	PrevLow        float64 // 직전 저점 (손절 계산용)
	PrevHigh       float64 // 직전 고점 (손절 계산용)
}

// NewDoubleRSISignal은 기본 필드로 새 DoubleRSISignal을 생성합니다
func NewDoubleRSISignal(
	signalType domain.SignalType,
	symbol string,
	price float64,
	timestamp time.Time,
	stopLoss float64,
	takeProfit float64,
) *DoubleRSISignal {
	return &DoubleRSISignal{
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

// CreateFromConditions은 전략 분석 시 생성된 조건 맵에서 DoubleRSISignal을 생성합니다
func CreateFromConditions(
	signalType domain.SignalType,
	symbol string,
	price float64,
	timestamp time.Time,
	stopLoss float64,
	takeProfit float64,
	conditions map[string]interface{},
) *DoubleRSISignal {
	rsiSignal := NewDoubleRSISignal(
		signalType,
		symbol,
		price,
		timestamp,
		stopLoss,
		takeProfit,
	)

	// conditions 맵에서 값 추출
	if val, ok := conditions["DailyRSIValue"].(float64); ok {
		rsiSignal.DailyRSIValue = val
	}
	if val, ok := conditions["HourlyRSIValue"].(float64); ok {
		rsiSignal.HourlyRSIValue = val
	}
	if val, ok := conditions["HourlyRSIPrev"].(float64); ok {
		rsiSignal.HourlyRSIPrev = val
	}
	if val, ok := conditions["CrossUp"].(bool); ok {
		rsiSignal.CrossUp = val
	}
	if val, ok := conditions["CrossDown"].(bool); ok {
		rsiSignal.CrossDown = val
	}
	if val, ok := conditions["PrevLow"].(float64); ok {
		rsiSignal.PrevLow = val
	}
	if val, ok := conditions["PrevHigh"].(float64); ok {
		rsiSignal.PrevHigh = val
	}

	return rsiSignal
}

// ToNotificationData는 더블 RSI 전략에 특화된 알림 데이터를 반환합니다
func (s *DoubleRSISignal) ToNotificationData() map[string]interface{} {
	data := s.BaseSignal.ToNotificationData() // 기본 필드 가져오기

	// 롱 조건 메시지
	longConditionValue := fmt.Sprintf(
		"%s 일봉 RSI > 60 (%.2f)\n%s 시간봉 RSI 상향돌파 %.2f → %.2f",
		getCheckMark(s.DailyRSIValue > 60),
		s.DailyRSIValue,
		getCheckMark(s.CrossUp),
		s.HourlyRSIPrev,
		s.HourlyRSIValue,
	)

	// 숏 조건 메시지
	shortConditionValue := fmt.Sprintf(
		"%s 일봉 RSI < 40 (%.2f)\n%s 시간봉 RSI 하향돌파 %.2f → %.2f",
		getCheckMark(s.DailyRSIValue < 40),
		s.DailyRSIValue,
		getCheckMark(s.CrossDown),
		s.HourlyRSIPrev,
		s.HourlyRSIValue,
	)

	// 기술적 지표 값 메시지 - 코드 블록으로 감싸기
	technicalValue := fmt.Sprintf(
		"```\n[일봉 RSI]: %.2f\n[시간봉 RSI]: %.2f\n[이전 RSI]: %.2f\n```",
		s.DailyRSIValue,
		s.HourlyRSIValue,
		s.HourlyRSIPrev,
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
