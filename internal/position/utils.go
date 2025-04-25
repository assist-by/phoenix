package position

import (
	"github.com/assist-by/phoenix/internal/domain"
)

// GetPositionSideFromSignal은 시그널 타입에 따른 포지션 사이드를 반환합니다
func GetPositionSideFromSignal(signalType domain.SignalType) domain.PositionSide {
	if signalType == domain.Long || signalType == domain.PendingLong {
		return domain.LongPosition
	}
	return domain.ShortPosition
}

// GetOrderSideForEntry는 포지션 진입을 위한 주문 사이드를 반환합니다
func GetOrderSideForEntry(positionSide domain.PositionSide) domain.OrderSide {
	if positionSide == domain.LongPosition {
		return domain.Buy
	}
	return domain.Sell
}

// GetOrderSideForExit는 포지션 청산을 위한 주문 사이드를 반환합니다
func GetOrderSideForExit(positionSide domain.PositionSide) domain.OrderSide {
	if positionSide == domain.LongPosition {
		return domain.Sell
	}
	return domain.Buy
}
