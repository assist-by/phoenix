// internal/exchange/exchange.go
package exchange

import (
	"context"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// Exchange는 거래소와의 상호작용을 위한 인터페이스입니다.
type Exchange interface {
	// 시장 데이터 조회
	GetServerTime(ctx context.Context) (time.Time, error)
	GetKlines(ctx context.Context, symbol string, interval domain.TimeInterval, limit int) (domain.CandleList, error)
	GetSymbolInfo(ctx context.Context, symbol string) (*domain.SymbolInfo, error)
	GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error)

	// 계정 데이터 조회
	GetBalance(ctx context.Context) (map[string]domain.Balance, error)
	GetPositions(ctx context.Context) ([]domain.Position, error)
	GetOpenOrders(ctx context.Context, symbol string) ([]domain.OrderResponse, error)
	GetLeverageBrackets(ctx context.Context, symbol string) ([]domain.LeverageBracket, error)

	// 거래 기능
	PlaceOrder(ctx context.Context, order domain.OrderRequest) (*domain.OrderResponse, error)
	CancelOrder(ctx context.Context, symbol string, orderID int64) error

	// 설정 기능
	SetLeverage(ctx context.Context, symbol string, leverage int) error
	SetPositionMode(ctx context.Context, hedgeMode bool) error

	// 시간 동기화
	SyncTime(ctx context.Context) error
}
