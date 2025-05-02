package backtest

import (
	"fmt"
	"log"
	"sync"

	"github.com/assist-by/phoenix/internal/indicator"
)

// IndicatorCache는 다양한 지표를 캐싱하는 범용 저장소입니다
type IndicatorCache struct {
	indicators map[string][]indicator.Result // 지표 이름을 키로 하는 결과 맵
	mutex      sync.RWMutex                  // 동시성 제어
}

// NewIndicatorCache는 새로운 지표 캐시를 생성합니다
func NewIndicatorCache() *IndicatorCache {
	return &IndicatorCache{
		indicators: make(map[string][]indicator.Result),
	}
}

// CacheIndicator는 특정 지표를 계산하고 캐싱합니다
func (cache *IndicatorCache) CacheIndicator(name string, ind indicator.Indicator, prices []indicator.PriceData) error {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	log.Printf("지표 '%s' 계산 중...", name)
	results, err := ind.Calculate(prices)
	if err != nil {
		return fmt.Errorf("지표 '%s' 계산 실패: %w", name, err)
	}

	cache.indicators[name] = results
	log.Printf("지표 '%s' 계산 완료: %d개 결과", name, len(results))
	return nil
}

// GetIndicator는 특정 이름과 인덱스의 지표 결과를 반환합니다
func (cache *IndicatorCache) GetIndicator(name string, index int) (indicator.Result, error) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	results, exists := cache.indicators[name]
	if !exists {
		return nil, fmt.Errorf("캐시에 '%s' 지표가 없습니다", name)
	}

	if index < 0 || index >= len(results) {
		return nil, fmt.Errorf("유효하지 않은 인덱스: %d", index)
	}

	return results[index], nil
}

// HasIndicator는 특정 지표가 캐시에 있는지 확인합니다
func (cache *IndicatorCache) HasIndicator(name string) bool {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	_, exists := cache.indicators[name]
	return exists
}

// GetIndicators는 지표명 목록을 반환합니다
func (cache *IndicatorCache) GetIndicators() []string {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	var names []string
	for name := range cache.indicators {
		names = append(names, name)
	}
	return names
}

// IndicatorSpec은 지표 명세를 나타냅니다
type IndicatorSpec struct {
	Type       string                 // 지표 유형 (EMA, MACD, SAR 등)
	Parameters map[string]interface{} // 지표 파라미터
}

// CreateIndicator는 지표 명세에 따라 지표 인스턴스를 생성합니다
func CreateIndicator(spec IndicatorSpec) (indicator.Indicator, error) {
	switch spec.Type {
	case "EMA":
		period, ok := spec.Parameters["period"].(int)
		if !ok {
			return nil, fmt.Errorf("EMA에는 'period' 파라미터가 필요합니다")
		}
		return indicator.NewEMA(period), nil

	case "MACD":
		shortPeriod, shortOk := spec.Parameters["shortPeriod"].(int)
		longPeriod, longOk := spec.Parameters["longPeriod"].(int)
		signalPeriod, signalOk := spec.Parameters["signalPeriod"].(int)
		if !shortOk || !longOk || !signalOk {
			return nil, fmt.Errorf("MACD에는 'shortPeriod', 'longPeriod', 'signalPeriod' 파라미터가 필요합니다")
		}
		return indicator.NewMACD(shortPeriod, longPeriod, signalPeriod), nil

	case "SAR":
		accelInitial, initialOk := spec.Parameters["accelerationInitial"].(float64)
		accelMax, maxOk := spec.Parameters["accelerationMax"].(float64)
		if !initialOk || !maxOk {
			// 기본값 사용
			return indicator.NewDefaultSAR(), nil
		}
		return indicator.NewSAR(accelInitial, accelMax), nil

	default:
		return nil, fmt.Errorf("지원하지 않는 지표 유형: %s", spec.Type)
	}
}

// CacheIndicators는 여러 지표를 한 번에 캐싱합니다
func (cache *IndicatorCache) CacheIndicators(specs []IndicatorSpec, prices []indicator.PriceData) error {
	for _, spec := range specs {
		// 지표 이름 생성 (유형 + 파라미터)
		name := spec.Type
		switch spec.Type {
		case "EMA":
			if period, ok := spec.Parameters["period"].(int); ok {
				name = fmt.Sprintf("%s(%d)", spec.Type, period)
			}
		case "MACD":
			if short, shortOk := spec.Parameters["shortPeriod"].(int); shortOk {
				if long, longOk := spec.Parameters["longPeriod"].(int); longOk {
					if signal, signalOk := spec.Parameters["signalPeriod"].(int); signalOk {
						name = fmt.Sprintf("%s(%d,%d,%d)", spec.Type, short, long, signal)
					}
				}
			}
		case "SAR":
			if initial, initialOk := spec.Parameters["accelerationInitial"].(float64); initialOk {
				if max, maxOk := spec.Parameters["accelerationMax"].(float64); maxOk {
					name = fmt.Sprintf("%s(%.2f,%.2f)", spec.Type, initial, max)
				}
			}
		}

		// 이미 캐시에 있는지 확인
		if cache.HasIndicator(name) {
			log.Printf("지표 '%s'가 이미 캐시에 있습니다", name)
			continue
		}

		// 지표 생성 및 캐싱
		ind, err := CreateIndicator(spec)
		if err != nil {
			return fmt.Errorf("지표 생성 실패 '%s': %w", name, err)
		}

		if err := cache.CacheIndicator(name, ind, prices); err != nil {
			return err
		}
	}

	return nil
}

// GetDefaultIndicators는 MACD+SAR+EMA 전략에 필요한 기본 지표 명세를 반환합니다
func GetDefaultIndicators() []IndicatorSpec {
	return []IndicatorSpec{
		{
			Type: "EMA",
			Parameters: map[string]interface{}{
				"period": 200,
			},
		},
		{
			Type: "MACD",
			Parameters: map[string]interface{}{
				"shortPeriod":  12,
				"longPeriod":   26,
				"signalPeriod": 9,
			},
		},
		{
			Type: "SAR",
			Parameters: map[string]interface{}{
				"accelerationInitial": 0.02,
				"accelerationMax":     0.2,
			},
		},
	}
}
