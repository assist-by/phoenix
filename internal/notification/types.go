package notification

import "github.com/assist-by/phoenix/internal/domain"

const (
	ColorSuccess = 0x00FF00 // 녹색
	ColorError   = 0xFF0000 // 빨간색
	ColorInfo    = 0x0000FF // 파란색
	ColorWarning = 0xFFA500 // 주황색
)

// Notifier는 알림 전송 인터페이스를 정의합니다
type Notifier interface {
	// SendSignal은 트레이딩 시그널 알림을 전송합니다
	// 기존: SendSignal(signal *strategy.Signal) error
	SendSignal(signal domain.SignalInterface) error

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

// GetColorForPosition은 포지션 타입에 따른 색상을 반환합니다
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
