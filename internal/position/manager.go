package position

import (
	"context"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/strategy"
)

// PositionRequest는 포지션 생성/관리 요청 정보를 담습니다
type PositionRequest struct {
	Signal     *strategy.Signal // 전략에서 생성된 시그널
	Leverage   int              // 사용할 레버리지
	RiskFactor float64          // 리스크 팩터 (계정 잔고의 몇 %를 리스크로 설정할지)
}

// PositionResult는 포지션 생성/관리 결과 정보를 담습니다
type PositionResult struct {
	Symbol        string              // 심볼 (예: BTCUSDT)
	PositionSide  domain.PositionSide // 롱/숏 포지션
	EntryPrice    float64             // 진입가
	Quantity      float64             // 수량
	PositionValue float64             // 포지션 가치 (USDT)
	Leverage      int                 // 레버리지
	StopLoss      float64             // 손절가
	TakeProfit    float64             // 익절가
	OrderIDs      map[string]int64    // 주문 ID (key: "entry", "tp", "sl")
	RealizedPnL   *float64            // 실현 손익 (청산 시에만 설정)
}

// Manager는 포지션 관리를 담당하는 인터페이스입니다
type Manager interface {
	// OpenPosition은 시그널에 따라 새 포지션을 생성합니다
	OpenPosition(ctx context.Context, req *PositionRequest) (*PositionResult, error)

	// ClosePosition은 특정 심볼의 포지션을 청산합니다
	ClosePosition(ctx context.Context, symbol string, positionSide domain.PositionSide) (*PositionResult, error)

	// GetActivePositions는 현재 활성화된 포지션 목록을 반환합니다
	GetActivePositions(ctx context.Context) ([]domain.Position, error)

	// IsEntryAvailable은 새 포지션 진입이 가능한지 확인합니다
	IsEntryAvailable(ctx context.Context, symbol string, signalType domain.SignalType) (bool, error)

	// CancelAllOrders는 특정 심볼의 모든 열린 주문을 취소합니다
	CancelAllOrders(ctx context.Context, symbol string) error
}
