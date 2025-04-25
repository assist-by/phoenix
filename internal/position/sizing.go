package position

import (
	"fmt"
	"math"
)

// SizingConfig는 포지션 사이즈 계산을 위한 설정을 정의합니다
type SizingConfig struct {
	AccountBalance   float64 // 계정 총 잔고 (USDT)
	AvailableBalance float64 // 사용 가능한 잔고 (USDT)
	Leverage         int     // 사용할 레버리지
	MaxAllocation    float64 // 최대 할당 비율 (기본값: 0.9 = 90%)
	StepSize         float64 // 수량 최소 단위
	TickSize         float64 // 가격 최소 단위
	MinNotional      float64 // 최소 주문 가치
	MaintMarginRate  float64 // 유지증거금률
}

// PositionSizeResult는 포지션 계산 결과를 담는 구조체입니다
type PositionSizeResult struct {
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매 수량 (코인)
}

// CalculatePositionSize는 적절한 포지션 크기를 계산합니다
func CalculatePositionSize(price float64, config SizingConfig) (PositionSizeResult, error) {
	// 기본값 설정
	if config.MaxAllocation <= 0 {
		config.MaxAllocation = 0.9 // 기본값 90%
	}

	// 1. 사용 가능한 잔고에서 MaxAllocation만큼만 사용
	allocatedBalance := config.AccountBalance * config.MaxAllocation

	// 가용 잔고가 필요한 할당 금액보다 작은 경우 에러 반환
	if config.AvailableBalance < allocatedBalance {
		return PositionSizeResult{}, fmt.Errorf("가용 잔고가 부족합니다: 필요 %.2f USDT, 현재 %.2f USDT",
			allocatedBalance, config.AvailableBalance)
	}

	// 2. 레버리지 적용 및 수수료 고려
	totalFeeRate := 0.002 // 0.2% (진입 + 청산 수수료 + 여유분)
	effectiveMargin := config.MaintMarginRate + totalFeeRate

	// 안전하게 사용 가능한 최대 포지션 가치 계산
	maxSafePositionValue := (allocatedBalance * float64(config.Leverage)) / (1 + effectiveMargin)

	// 3. 최대 안전 수량 계산
	maxSafeQuantity := maxSafePositionValue / price

	// 4. 최소 주문 단위로 수량 조정
	// stepSize가 0.001이면 소수점 3자리
	precision := 0
	temp := config.StepSize
	for temp < 1.0 {
		temp *= 10
		precision++
	}

	// 소수점 자릿수에 맞춰 내림 계산
	scale := math.Pow(10, float64(precision))
	steps := math.Floor(maxSafeQuantity / config.StepSize)
	adjustedQuantity := steps * config.StepSize

	// 소수점 자릿수 정밀도 보장
	adjustedQuantity = math.Floor(adjustedQuantity*scale) / scale

	// 5. 최종 포지션 가치 계산
	finalPositionValue := adjustedQuantity * price

	// 최소 주문 가치 체크
	if finalPositionValue < config.MinNotional {
		return PositionSizeResult{}, fmt.Errorf("계산된 포지션 가치(%.2f)가 최소 주문 가치(%.2f)보다 작습니다",
			finalPositionValue, config.MinNotional)
	}

	// 소수점 2자리까지 내림 (USDT 기준)
	return PositionSizeResult{
		PositionValue: math.Floor(finalPositionValue*100) / 100,
		Quantity:      adjustedQuantity,
	}, nil
}
