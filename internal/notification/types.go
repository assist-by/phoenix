package notification

import "time"

// SignalType은 트레이딩 시그널 종류를 정의합니다
type SignalType string

const (
	SignalLong   SignalType = "LONG"
	SignalShort  SignalType = "SHORT"
	SignalClose  SignalType = "CLOSE"
	ColorSuccess            = 0x00FF00
	ColorError              = 0xFF0000
	ColorInfo               = 0x0000FF
)

// Signal은 트레이딩 시그널 정보를 담는 구조체입니다
type Signal struct {
	Type      SignalType
	Symbol    string
	Price     float64
	Timestamp time.Time
	Reason    string
}

// Notifier는 알림 전송 인터페이스를 정의합니다
type Notifier interface {
	// SendSignal은 트레이딩 시그널 알림을 전송합니다
	SendSignal(signal Signal) error

	// SendError는 에러 알림을 전송합니다
	SendError(err error) error

	// SendInfo는 일반 정보 알림을 전송합니다
	SendInfo(message string) error
}

// TradeInfo는 거래 실행 정보를 정의합니다
type TradeInfo struct {
	Symbol        string
	PositionType  string // "LONG" or "SHORT"
	PositionValue float64
	EntryPrice    float64
	StopLoss      float64
	TakeProfit    float64
	Balance       float64 // 현재 USDT 잔고
	Leverage      int     // 사용 레버리지
}

// getColorForPosition은 포지션 타입에 따른 색상을 반환합니다
func GetColorForPosition(positionType string) int {
	switch positionType {
	case "LONG":
		return ColorSuccess
	case "SHORT":
		return ColorError
	default:
		return ColorInfo
	}
}
