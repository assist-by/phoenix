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
	PendingLong  // MACD 상향 돌파 후 SAR 반전 대기 상태
	PendingShort // MACD 하향돌파 후 SAR 반전 대기 상태
)

func (s SignalType) String() string {
	switch s {
	case NoSignal:
		return "NoSignal"
	case Long:
		return "Long"
	case Short:
		return "Short"
	case PendingLong:
		return "PendingLong"
	case PendingShort:
		return "PendingShort"
	default:
		return "Unknown"
	}
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
	PrevMACD       float64    // 이전 MACD 값
	PrevSignal     float64    // 이전 Signal 값
	PrevHistogram  float64    // 이전 히스토그램 값
	LastSignal     *Signal    // 마지막 발생 시그널
	PendingSignal  SignalType // 대기중인 시그널 타입
	WaitedCandles  int        // 대기한 캔들 수
	MaxWaitCandles int        // 최대 대기 캔들 수
}

// Detector는 시그널 감지기를 정의합니다
type Detector struct {
	states         map[string]*SymbolState
	emaLength      int     // EMA 기간
	stopLossPct    float64 // 손절 비율
	takeProfitPct  float64 // 익절 비율
	minHistogram   float64 // MACD 히스토그램 최소값
	maxWaitCandles int     // 기본 최대 대기 캔들 수
	mu             sync.RWMutex
}
