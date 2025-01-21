package signal

import (
	"time"
)

// SignalType은 시그널 유형을 정의합니다
type SignalType int

const (
	NoSignal SignalType = iota
	Long
	Short
)

// Signal은 생성된 시그널 정보를 담습니다
type Signal struct {
	Type      SignalType
	Symbol    string
	Price     float64
	Timestamp time.Time

	// 진입 조건 상세
	Conditions struct {
		EMA  bool // EMA 조건 충족 여부
		MACD bool // MACD 조건 충족 여부
		SAR  bool // SAR 조건 충족 여부

		// 지표 상세값
		EMAValue    float64
		MACDValue   float64
		SignalValue float64
		SARValue    float64
	}

	// 리스크 관리
	StopLoss   float64
	TakeProfit float64
}

// Detector는 시그널 감지기를 정의합니다
type Detector struct {
	// 설정값
	emaLength     int     // EMA 기간
	stopLossPct   float64 // 손절 비율
	takeProfitPct float64 // 익절 비율

	// 이전 상태 저장
	prevMACD   float64
	prevSignal float64
	lastSignal *Signal
}
