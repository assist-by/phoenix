package market

import "fmt"

// APIError는 바이낸스 API 에러를 표현합니다
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("바이낸스 API 에러(코드: %d): %s", e.Code, e.Message)
}

// OrderSide는 주문 방향을 정의합니다
type OrderSide string

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"

	Long  PositionSide = "LONG"
	Short PositionSide = "SHORT"
)

// PositionSizeResult는 포지션 계산 결과를 담는 구조체입니다
type PositionSizeResult struct {
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매 수량 (코인)
}
