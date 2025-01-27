package trading

import (
	"context"

	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/market"
	"github.com/assist-by/phoenix/internal/notification/discord"
)

// ExcuteConfig는 거래를 실행하는데 필요한 설정을 담고 있는 구조체이다.
type ExecuteConfig struct {
	Leverage int // 레버리지
}

// Executor는 매매 실행기 인터페이스를 정의합니다
type Executor interface {
	// Execute는 시그널에 따라 실제 매매를 실행합니다
	Execute(ctx context.Context, signal *signal.Signal) error
}

// excutor는 거래를 실행하는데 필요한 정보를 담고 있는 구조체이다.
type excutor struct {
	client  *market.Client
	discord *discord.Client
	config  ExecuteConfig
}

// OrderSide는 시그널 타입에 따라 거래 주문을 어떻게 할지 정의하는 함수 타입입니다.
type OrderSide func(signal.SignalType) market.OrderSide

// PositionSide는 시그널 타입에 따라 포지션을 어떻게 열지 정의하는 함수 타입입니다.
type PositionSide func(signal.SignalType) market.PositionSide

// ValidationError는 거래 실행 중 발생한 유효성 검사 오류를 나타내는 구조체입니다.
type ValidationError struct {
	Field string
	Err   error
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Err.Error()
}

// ExecutionError는 거래 실행 중 발생한 오류를 나타내는 구조체입니다.
type ExecutionError struct {
	Phase string
	Err   error
}

func (e *ExecutionError) Error() string {
	return "매매 실행 실패 (" + e.Phase + "): " + e.Err.Error()
}
