package strategy

import (
	"context"
	"fmt"

	"github.com/assist-by/phoenix/internal/domain"
)

// Strategy는 트레이딩 전략의 인터페이스를 정의합니다
type Strategy interface {
	// Initialize는 전략을 초기화합니다
	Initialize(ctx context.Context) error

	// Analyze는 주어진 데이터를 분석하여 매매 신호를 생성합니다
	Analyze(ctx context.Context, symbol string, candles domain.CandleList) (domain.SignalInterface, error)

	// GetName은 전략의 이름을 반환합니다
	GetName() string

	// GetDescription은 전략의 설명을 반환합니다
	GetDescription() string

	// GetConfig는 전략의 현재 설정을 반환합니다
	GetConfig() map[string]interface{}

	// UpdateConfig는 전략 설정을 업데이트합니다
	UpdateConfig(config map[string]interface{}) error
}

// BaseStrategy는 모든 전략 구현체에서 공통적으로 사용할 수 있는 기본 구현을 제공합니다
type BaseStrategy struct {
	Name        string
	Description string
	Config      map[string]interface{}
}

// GetName은 전략의 이름을 반환합니다
func (b *BaseStrategy) GetName() string {
	return b.Name
}

// GetDescription은 전략의 설명을 반환합니다
func (b *BaseStrategy) GetDescription() string {
	return b.Description
}

// GetConfig는 전략의 현재 설정을 반환합니다
func (b *BaseStrategy) GetConfig() map[string]interface{} {
	// 설정의 복사본 반환
	configCopy := make(map[string]interface{})
	for k, v := range b.Config {
		configCopy[k] = v
	}
	return configCopy
}

// UpdateConfig는 전략 설정을 업데이트합니다
func (b *BaseStrategy) UpdateConfig(config map[string]interface{}) error {
	// 설정 업데이트
	for k, v := range config {
		b.Config[k] = v
	}
	return nil
}

// TPSLOptions는 TP/SL 계산에 필요한 추가 옵션들을 포함합니다
type TPSLOptions struct {
	// 전략별 특화 필드
	SAR           *float64          // MACD+SAR+EMA 전략용 SAR 값 (nil 가능)
	RecentCandles domain.CandleList // 고점/저점 계산용 최근 캔들 (더블 RSI 전략)
}

// Factory는 전략 인스턴스를 생성하는 함수 타입입니다
type Factory func(config map[string]interface{}) (Strategy, error)

// Registry는 사용 가능한 모든 전략을 등록하고 관리합니다
type Registry struct {
	strategies map[string]Factory
}

// NewRegistry는 새로운 전략 레지스트리를 생성합니다
func NewRegistry() *Registry {
	return &Registry{
		strategies: make(map[string]Factory),
	}
}

// Register는 새로운 전략 팩토리를 레지스트리에 등록합니다
func (r *Registry) Register(name string, factory Factory) {
	r.strategies[name] = factory
}

// Create는 주어진 이름과 설정으로 전략 인스턴스를 생성합니다
func (r *Registry) Create(name string, config map[string]interface{}) (Strategy, error) {
	factory, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("존재하지 않는 전략: %s", name)
	}
	return factory(config)
}

// ListStrategies는 사용 가능한 모든 전략 이름을 반환합니다
func (r *Registry) ListStrategies() []string {
	var names []string
	for name := range r.strategies {
		names = append(names, name)
	}
	return names
}
