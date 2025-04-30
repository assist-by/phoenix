package notification

import (
	"time"

	"github.com/assist-by/phoenix/internal/strategy"
)

// SignalType은 트레이딩 시그널 종류를 정의합니다
type SignalType string

const (
	SignalLong         SignalType = "LONG"
	SignalShort        SignalType = "SHORT"
	SignalClose        SignalType = "CLOSE"
	SignalPendingLong  SignalType = "PENDINGLONG"  // 롱 대기 상태
	SignalPendingShort SignalType = "PENDINGSHORT" // 숏 대기 상태
	ColorSuccess                  = 0x00FF00
	ColorError                    = 0xFF0000
	ColorInfo                     = 0x0000FF
	ColorWarning                  = 0xFFA500 // 대기 상태를 위한 주황색 추가
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
	SendSignal(signal *strategy.Signal) error

	// SendError는 에러 알림을 전송합니다
	SendError(err error) error

	// SendInfo는 일반 정보 알림을 전송합니다
	SendInfo(message string) error

	// SendTradeInfo는 거래 실행 정보를 전송합니다
	SendTradeInfo(info TradeInfo) error
}

// TradeInfo는 거래 실행 정보를 정의합니다
type TradeInfo struct {
	Symbol        string  // 심볼 (예: BTCUSDT)
	PositionType  string  // "LONG" or "SHORT"
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매/판매 수량 (코인)
	EntryPrice    float64 // 진입가
	StopLoss      float64 // 손절가
	TakeProfit    float64 // 익절가
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
	case "PENDINGLONG", "PENDINGSHORT":
		return ColorWarning
	default:
		return ColorInfo
	}
}
