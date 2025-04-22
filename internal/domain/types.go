package domain

// SignalType은 트레이딩 시그널 유형을 정의합니다
type SignalType int

const (
	NoSignal SignalType = iota
	Long
	Short
	PendingLong  // MACD 상향 돌파 후 SAR 반전 대기 상태
	PendingShort // MACD 하향돌파 후 SAR 반전 대기 상태
)

// String은 SignalType의 문자열 표현을 반환합니다
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

// OrderSide는 주문 방향을 정의합니다
type OrderSide string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"
)

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

const (
	LongPosition  PositionSide = "LONG"
	ShortPosition PositionSide = "SHORT"
	BothPosition  PositionSide = "BOTH" // 헤지 모드가 아닌 경우
)

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Market           OrderType = "MARKET"
	Limit            OrderType = "LIMIT"
	StopMarket       OrderType = "STOP_MARKET"
	TakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"
)

// TimeInterval은 캔들 차트의 시간 간격을 정의합니다
type TimeInterval string

const (
	Interval1m  TimeInterval = "1m"
	Interval3m  TimeInterval = "3m"
	Interval5m  TimeInterval = "5m"
	Interval15m TimeInterval = "15m"
	Interval30m TimeInterval = "30m"
	Interval1h  TimeInterval = "1h"
	Interval2h  TimeInterval = "2h"
	Interval4h  TimeInterval = "4h"
	Interval6h  TimeInterval = "6h"
	Interval8h  TimeInterval = "8h"
	Interval12h TimeInterval = "12h"
	Interval1d  TimeInterval = "1d"
)

// NotificationColor는 알림 색상 코드를 정의합니다
const (
	ColorSuccess = 0x00FF00 // 녹색
	ColorError   = 0xFF0000 // 빨간색
	ColorInfo    = 0x0000FF // 파란색
	ColorWarning = 0xFFA500 // 주황색
)

// ErrorCode는 API 에러 코드를 정의합니다
const (
	ErrPositionModeNoChange = -4059 // 포지션 모드 변경 불필요 에러
)
