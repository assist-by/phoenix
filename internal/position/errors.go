package position

import "fmt"

// Error 타입들은 포지션 관리 중 발생할 수 있는 다양한 에러를 정의합니다
var (
	ErrInsufficientBalance   = fmt.Errorf("잔고가 부족합니다")
	ErrPositionExists        = fmt.Errorf("이미 해당 심볼에 포지션이 존재합니다")
	ErrPositionNotFound      = fmt.Errorf("해당 심볼에 포지션이 존재하지 않습니다")
	ErrInvalidTPSLConfig     = fmt.Errorf("잘못된 TP/SL 설정입니다")
	ErrOrderCancellationFail = fmt.Errorf("주문 취소에 실패했습니다")
	ErrOrderPlacementFail    = fmt.Errorf("주문 생성에 실패했습니다")
)

// PositionError는 포지션 관리 에러를 확장한 구조체입니다
type PositionError struct {
	Symbol string
	Op     string
	Err    error
}

// Error는 error 인터페이스를 구현합니다
func (e *PositionError) Error() string {
	if e.Symbol != "" {
		return fmt.Sprintf("포지션 에러 [%s, 작업: %s]: %v", e.Symbol, e.Op, e.Err)
	}
	return fmt.Sprintf("포지션 에러 [작업: %s]: %v", e.Op, e.Err)
}

// Unwrap은 내부 에러를 반환합니다 (errors.Is/As 지원을 위함)
func (e *PositionError) Unwrap() error {
	return e.Err
}

// NewPositionError는 새로운 PositionError를 생성합니다
func NewPositionError(symbol, op string, err error) *PositionError {
	return &PositionError{
		Symbol: symbol,
		Op:     op,
		Err:    err,
	}
}
