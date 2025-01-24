package signal

import (
	"sync"
	"time"
)

// SignalType은 시그널 유형을 정의합니다
type SignalType int

const (
	NoSignal SignalType = iota
	Long
	Short
)

// SignalConditions는 시그널 발생 조건들의 상세 정보를 저장합니다
type SignalConditions struct {
	EMA         bool    // EMA 조건 충족 여부
	MACD        bool    // MACD 조건 충족 여부
	SAR         bool    // SAR 조건 충족 여부
	EMAValue    float64 // EMA 값
	MACDValue   float64 // MACD 값
	SignalValue float64 // Signal Line 값
	SARValue    float64 // SAR 값
}

// Signal은 생성된 시그널 정보를 담습니다
type Signal struct {
	Type       SignalType
	Symbol     string
	Price      float64
	Timestamp  time.Time
	Conditions SignalConditions
	StopLoss   float64
	TakeProfit float64
}

// SymbolState는 각 심볼별 상태를 관리합니다
type SymbolState struct {
	PrevMACD   float64
	PrevSignal float64
	LastSignal *Signal
}

// Detector는 시그널 감지기를 정의합니다
type Detector struct {
	states        map[string]*SymbolState
	emaLength     int     // EMA 기간
	stopLossPct   float64 // 손절 비율
	takeProfitPct float64 // 익절 비율
	mu            sync.RWMutex
}
