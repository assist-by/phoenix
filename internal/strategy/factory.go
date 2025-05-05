package strategy

import "github.com/assist-by/phoenix/internal/config"

// CreateStrategyFromConfig는 설정에 따라 적절한 전략을 생성합니다
func CreateStrategyFromConfig(registry *Registry, cfg *config.Config) (Strategy, error) {
	// 사용할 전략 이름 (기본값은 MACD+SAR+EMA)
	strategyName := cfg.Trading.Strategy

	// 전략별 설정 맵 생성
	strategyConfigs := map[string]map[string]interface{}{
		"MACD+SAR+EMA": {
			"emaLength":      200,
			"stopLossPct":    0.02,
			"takeProfitPct":  0.04,
			"minHistogram":   0.00005,
			"maxWaitCandles": 3,
		},
		"DoubleRSI": {
			"dailyRSIPeriod":     7,
			"hourlyRSIPeriod":    7,
			"dailyRSIUpperBand":  60.0,
			"dailyRSILowerBand":  40.0,
			"hourlyRSIUpperBand": 60.0,
			"hourlyRSILowerBand": 40.0,
			"tpRatio":            1.5,
			"lookbackPeriod":     5,
		},
	}

	// 선택한 전략의 설정 가져오기
	config, exists := strategyConfigs[strategyName]
	if !exists {
		// 등록된 전략이 아니면 기본 전략 사용
		strategyName = "MACD+SAR+EMA"
		config = strategyConfigs[strategyName]
	}

	// 전략 생성
	strategy, err := registry.Create(strategyName, config)
	if err != nil {
		return nil, err
	}

	return strategy, nil
}
