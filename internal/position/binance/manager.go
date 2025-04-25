package binance

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/exchange"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/position"
	"github.com/assist-by/phoenix/internal/strategy"
)

// BinancePositionManager는 바이낸스에서 포지션 관리를 담당합니다
type BinancePositionManager struct {
	exchange   exchange.Exchange
	notifier   notification.Notifier
	strategy   strategy.Strategy
	maxRetries int
	retryDelay time.Duration
}

// NewManager는 새로운 바이낸스 포지션 매니저를 생성합니다
func NewManager(exchange exchange.Exchange, notifier notification.Notifier, strategy strategy.Strategy) position.Manager {
	return &BinancePositionManager{
		exchange:   exchange,
		notifier:   notifier,
		strategy:   strategy,
		maxRetries: 5,
		retryDelay: 1 * time.Second,
	}
}

// OpenPosition은 시그널에 따라 새 포지션을 생성합니다
func (m *BinancePositionManager) OpenPosition(ctx context.Context, req *position.PositionRequest) (*position.PositionResult, error) {
	symbol := req.Signal.Symbol
	signalType := req.Signal.Type

	// 1. 진입 가능 여부 확인
	available, err := m.IsEntryAvailable(ctx, symbol, signalType)
	if err != nil {
		return nil, position.NewPositionError(symbol, "check_availability", err)
	}

	if !available {
		return nil, position.NewPositionError(symbol, "check_availability", position.ErrPositionExists)
	}

	// 2. 현재 가격 확인
	candles, err := m.exchange.GetKlines(ctx, symbol, "1m", 1)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_price", err)
	}
	if len(candles) == 0 {
		return nil, position.NewPositionError(symbol, "get_price", fmt.Errorf("캔들 데이터를 가져오지 못했습니다"))
	}
	currentPrice := candles[0].Close

	// 3. 심볼 정보 조회
	symbolInfo, err := m.exchange.GetSymbolInfo(ctx, symbol)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_symbol_info", err)
	}

	// 4. HEDGE 모드 설정
	if err := m.exchange.SetPositionMode(ctx, true); err != nil {
		return nil, position.NewPositionError(symbol, "set_hedge_mode", err)
	}

	// 5. 레버리지 설정
	leverage := req.Leverage
	if err := m.exchange.SetLeverage(ctx, symbol, leverage); err != nil {
		return nil, position.NewPositionError(symbol, "set_leverage", err)
	}

	// 6. 잔고 확인
	balances, err := m.exchange.GetBalance(ctx)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_balance", err)
	}

	usdtBalance, exists := balances["USDT"]
	if !exists || usdtBalance.Available <= 0 {
		return nil, position.NewPositionError(symbol, "check_balance", position.ErrInsufficientBalance)
	}

	// 7. 레버리지 브라켓 정보 조회
	brackets, err := m.exchange.GetLeverageBrackets(ctx, symbol)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_leverage_brackets", err)
	}

	if len(brackets) == 0 {
		return nil, position.NewPositionError(symbol, "get_leverage_brackets", fmt.Errorf("레버리지 브라켓 정보가 없습니다"))
	}

	// 적절한 브라켓 찾기
	var maintMarginRate float64 = 0.01 // 기본값
	for _, bracket := range brackets {
		if bracket.InitialLeverage >= leverage {
			maintMarginRate = bracket.MaintMarginRatio
			break
		}
	}

	// 8. 포지션 크기 계산
	sizingConfig := position.SizingConfig{
		AccountBalance:   usdtBalance.CrossWalletBalance,
		AvailableBalance: usdtBalance.Available,
		Leverage:         leverage,
		MaxAllocation:    0.9, // 90% 사용
		StepSize:         symbolInfo.StepSize,
		TickSize:         symbolInfo.TickSize,
		MinNotional:      symbolInfo.MinNotional,
		MaintMarginRate:  maintMarginRate,
	}

	posResult, err := position.CalculatePositionSize(currentPrice, sizingConfig)
	if err != nil {
		return nil, position.NewPositionError(symbol, "calculate_position", err)
	}

	// 9. 수량 정밀도 조정
	adjustedQuantity := domain.AdjustQuantity(posResult.Quantity, symbolInfo.StepSize, symbolInfo.QuantityPrecision)

	// 10. 포지션 방향 결정
	positionSide := position.GetPositionSideFromSignal(req.Signal.Type)
	orderSide := position.GetOrderSideForEntry(positionSide)

	// 11. 진입 주문 생성
	entryOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         orderSide,
		PositionSide: positionSide,
		Type:         domain.Market,
		Quantity:     adjustedQuantity,
	}

	// 12. 진입 주문 실행
	orderResponse, err := m.exchange.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return nil, position.NewPositionError(symbol, "place_entry_order", err)
	}

	log.Printf("포지션 진입 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		symbol, adjustedQuantity, orderResponse.OrderID)

	// 13. 포지션 확인
	var actualPosition *domain.Position
	for i := 0; i < m.maxRetries; i++ {
		positions, err := m.exchange.GetPositions(ctx)
		if err != nil {
			time.Sleep(m.retryDelay)
			continue
		}

		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionSide == positionSide && math.Abs(pos.Quantity) > 0 {
				actualPosition = &pos
				break
			}
		}

		if actualPosition != nil {
			break
		}

		time.Sleep(m.retryDelay)
	}

	if actualPosition == nil {
		return nil, position.NewPositionError(symbol, "confirm_position", fmt.Errorf("포지션 확인 실패"))
	}

	// 14. TP/SL 설정
	// 시그널에서 직접 TP/SL 값 사용
	stopLoss := domain.AdjustPrice(req.Signal.StopLoss, symbolInfo.TickSize, symbolInfo.PricePrecision)
	takeProfit := domain.AdjustPrice(req.Signal.TakeProfit, symbolInfo.TickSize, symbolInfo.PricePrecision)

	// 15. TP/SL 주문 생성
	exitSide := position.GetOrderSideForExit(positionSide)

	// 손절 주문
	slOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         exitSide,
		PositionSide: positionSide,
		Type:         domain.StopMarket,
		Quantity:     math.Abs(actualPosition.Quantity),
		StopPrice:    stopLoss,
	}

	// 익절 주문
	tpOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         exitSide,
		PositionSide: positionSide,
		Type:         domain.TakeProfitMarket,
		Quantity:     math.Abs(actualPosition.Quantity),
		StopPrice:    takeProfit,
	}

	// 16. TP/SL 주문 실행
	slResponse, err := m.exchange.PlaceOrder(ctx, slOrder)
	if err != nil {
		log.Printf("손절 주문 실패: %v", err)
		// 진입은 성공했으므로 에러는 기록만 하고 계속 진행
	}

	tpResponse, err := m.exchange.PlaceOrder(ctx, tpOrder)
	if err != nil {
		log.Printf("익절 주문 실패: %v", err)
		// 진입은 성공했으므로 에러는 기록만 하고 계속 진행
	}

	// 17. 결과 생성
	result := &position.PositionResult{
		Symbol:        symbol,
		PositionSide:  positionSide,
		EntryPrice:    actualPosition.EntryPrice,
		Quantity:      math.Abs(actualPosition.Quantity),
		PositionValue: posResult.PositionValue,
		Leverage:      leverage,
		StopLoss:      stopLoss,
		TakeProfit:    takeProfit,
		OrderIDs: map[string]int64{
			"entry": orderResponse.OrderID,
		},
	}

	// TP/SL 주문 ID 추가 (성공한 경우만)
	if slResponse != nil {
		result.OrderIDs["sl"] = slResponse.OrderID
	}
	if tpResponse != nil {
		result.OrderIDs["tp"] = tpResponse.OrderID
	}

	// 18. 알림 전송
	if m.notifier != nil {
		tradeInfo := notification.TradeInfo{
			Symbol:        symbol,
			PositionType:  string(positionSide),
			PositionValue: posResult.PositionValue,
			Quantity:      result.Quantity,
			EntryPrice:    result.EntryPrice,
			StopLoss:      stopLoss,
			TakeProfit:    takeProfit,
			Balance:       usdtBalance.Available - posResult.PositionValue,
			Leverage:      leverage,
		}

		if err := m.notifier.SendTradeInfo(tradeInfo); err != nil {
			log.Printf("거래 정보 알림 전송 실패: %v", err)
		}
	}

	return result, nil
}

// IsEntryAvailable은 새 포지션 진입이 가능한지 확인합니다
func (m *BinancePositionManager) IsEntryAvailable(ctx context.Context, symbol string, signalType domain.SignalType) (bool, error) {
	// 1. 현재 포지션 조회
	positions, err := m.exchange.GetPositions(ctx)
	if err != nil {
		return false, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 결정할 포지션 사이드
	targetSide := position.GetPositionSideFromSignal(signalType)

	// 기존 포지션 확인
	for _, pos := range positions {
		if pos.Symbol == symbol && math.Abs(pos.Quantity) > 0 {
			// 같은 방향의 포지션이 있으면 진입 불가
			if (pos.PositionSide == targetSide) ||
				(pos.PositionSide == domain.BothPosition &&
					((targetSide == domain.LongPosition && pos.Quantity > 0) ||
						(targetSide == domain.ShortPosition && pos.Quantity < 0))) {
				return false, nil
			}

			// 반대 방향의 포지션이 있으면 청산 필요
			log.Printf("반대 방향 포지션 감지: %s, 수량: %.8f, 방향: %s",
				symbol, math.Abs(pos.Quantity), pos.PositionSide)

			// 기존 주문 취소
			if err := m.CancelAllOrders(ctx, symbol); err != nil {
				return false, fmt.Errorf("기존 주문 취소 실패: %w", err)
			}

			// 포지션 청산
			closeOrder := domain.OrderRequest{
				Symbol:       symbol,
				Side:         position.GetOrderSideForExit(pos.PositionSide),
				PositionSide: pos.PositionSide,
				Type:         domain.Market,
				Quantity:     math.Abs(pos.Quantity),
			}

			_, err := m.exchange.PlaceOrder(ctx, closeOrder)
			if err != nil {
				return false, fmt.Errorf("포지션 청산 실패: %w", err)
			}

			// 포지션 청산 확인
			for i := 0; i < m.maxRetries; i++ {
				cleared := true
				positions, err := m.exchange.GetPositions(ctx)
				if err != nil {
					time.Sleep(m.retryDelay)
					continue
				}

				for _, pos := range positions {
					if pos.Symbol == symbol && math.Abs(pos.Quantity) > 0 {
						cleared = false
						break
					}
				}

				if cleared {
					log.Printf("%s 포지션 청산 확인 완료", symbol)
					return true, nil
				}

				time.Sleep(m.retryDelay)
			}

			return false, fmt.Errorf("최대 재시도 횟수 초과: 포지션이 청산되지 않음")
		}
	}

	// 2. 열린 주문 확인 및 취소
	openOrders, err := m.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return false, fmt.Errorf("주문 조회 실패: %w", err)
	}

	if len(openOrders) > 0 {
		log.Printf("%s의 기존 주문 %d개를 취소합니다.", symbol, len(openOrders))

		for _, order := range openOrders {
			if err := m.exchange.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				return false, fmt.Errorf("주문 취소 실패 (ID: %d): %w", order.OrderID, err)
			}
			log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
		}
	}

	return true, nil
}

// CancelAllOrders는 특정 심볼의 모든 열린 주문을 취소합니다
func (m *BinancePositionManager) CancelAllOrders(ctx context.Context, symbol string) error {
	openOrders, err := m.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("주문 조회 실패: %w", err)
	}

	if len(openOrders) > 0 {
		log.Printf("%s의 기존 주문 %d개를 취소합니다.", symbol, len(openOrders))

		for _, order := range openOrders {
			if err := m.exchange.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				return fmt.Errorf("주문 취소 실패 (ID: %d): %w", order.OrderID, err)
			}
			log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
		}
	}

	return nil
}

// ClosePosition은 특정 심볼의 포지션을 청산합니다
func (m *BinancePositionManager) ClosePosition(ctx context.Context, symbol string, positionSide domain.PositionSide) (*position.PositionResult, error) {
	// 1. 포지션 조회
	positions, err := m.exchange.GetPositions(ctx)
	if err != nil {
		return nil, position.NewPositionError(symbol, "get_positions", err)
	}

	// 2. 해당 심볼 포지션 찾기
	var targetPosition *domain.Position
	for _, pos := range positions {
		if pos.Symbol == symbol && pos.PositionSide == positionSide && math.Abs(pos.Quantity) > 0 {
			targetPosition = &pos
			break
		}
	}

	if targetPosition == nil {
		return nil, position.NewPositionError(symbol, "find_position", position.ErrPositionNotFound)
	}

	// 3. 기존 주문 취소
	if err := m.CancelAllOrders(ctx, symbol); err != nil {
		return nil, position.NewPositionError(symbol, "cancel_orders", err)
	}

	// 4. 청산 주문 생성
	exitSide := position.GetOrderSideForExit(positionSide)

	closeOrder := domain.OrderRequest{
		Symbol:       symbol,
		Side:         exitSide,
		PositionSide: positionSide,
		Type:         domain.Market,
		Quantity:     math.Abs(targetPosition.Quantity),
	}

	// 5. 청산 주문 실행
	orderResponse, err := m.exchange.PlaceOrder(ctx, closeOrder)
	if err != nil {
		return nil, position.NewPositionError(symbol, "place_close_order", err)
	}

	log.Printf("포지션 청산 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		symbol, math.Abs(targetPosition.Quantity), orderResponse.OrderID)

	// 6. 포지션 청산 확인
	cleared := false
	for i := 0; i < m.maxRetries; i++ {
		positions, err := m.exchange.GetPositions(ctx)
		if err != nil {
			time.Sleep(m.retryDelay)
			continue
		}

		found := false
		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionSide == positionSide && math.Abs(pos.Quantity) > 0 {
				found = true
				break
			}
		}

		if !found {
			cleared = true
			break
		}

		time.Sleep(m.retryDelay)
	}

	if !cleared {
		return nil, position.NewPositionError(symbol, "confirm_close", fmt.Errorf("최대 재시도 횟수 초과: 포지션이 청산되지 않음"))
	}

	// 7. 결과 생성
	realizedPnL := targetPosition.UnrealizedPnL

	result := &position.PositionResult{
		Symbol:       symbol,
		PositionSide: positionSide,
		EntryPrice:   targetPosition.EntryPrice,
		Quantity:     math.Abs(targetPosition.Quantity),
		Leverage:     targetPosition.Leverage,
		OrderIDs: map[string]int64{
			"close": orderResponse.OrderID,
		},
		RealizedPnL: &realizedPnL,
	}

	return result, nil
}

// GetActivePositions는 현재 활성화된 포지션 목록을 반환합니다
func (m *BinancePositionManager) GetActivePositions(ctx context.Context) ([]domain.Position, error) {
	positions, err := m.exchange.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 활성 포지션만 필터링
	var activePositions []domain.Position
	for _, pos := range positions {
		if math.Abs(pos.Quantity) > 0 {
			activePositions = append(activePositions, pos)
		}
	}

	return activePositions, nil
}
