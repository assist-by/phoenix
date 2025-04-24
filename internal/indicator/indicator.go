package indicator

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// PriceData는 지표 계산에 필요한 가격 정보를 정의합니다
type PriceData struct {
	Time   time.Time // 타임스탬프
	Open   float64   // 시가
	High   float64   // 고가
	Low    float64   // 저가
	Close  float64   // 종가
	Volume float64   // 거래량
}

// Result는 지표 계산의 기본 결과 구조체입니다
type Result interface {
	GetTimestamp() time.Time
}

// ValidationError는 입력값 검증 에러를 정의합니다
type ValidationError struct {
	Field string
	Err   error
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("유효하지 않은 %s: %v", e.Field, e.Err)
}

// Indicator는 모든 기술적 지표가 구현해야 하는 인터페이스입니다
type Indicator interface {
	// Calculate는 가격 데이터를 기반으로 지표를 계산합니다
	Calculate(data []PriceData) ([]Result, error)

	// GetName은 지표의 이름을 반환합니다
	GetName() string

	// GetConfig는 지표의 현재 설정을 반환합니다
	GetConfig() map[string]interface{}

	// UpdateConfig는 지표 설정을 업데이트합니다
	UpdateConfig(config map[string]interface{}) error
}

// BaseIndicator는 모든 지표 구현체에서 공통적으로 사용할 수 있는 기본 구현을 제공합니다
type BaseIndicator struct {
	Name   string
	Config map[string]interface{}
}

// GetName은 지표의 이름을 반환합니다
func (b *BaseIndicator) GetName() string {
	return b.Name
}

// GetConfig는 지표의 현재 설정을 반환합니다
func (b *BaseIndicator) GetConfig() map[string]interface{} {
	// 설정의 복사본 반환
	configCopy := make(map[string]interface{})
	for k, v := range b.Config {
		configCopy[k] = v
	}
	return configCopy
}

// UpdateConfig는 지표 설정을 업데이트합니다
func (b *BaseIndicator) UpdateConfig(config map[string]interface{}) error {
	// 설정 업데이트
	for k, v := range config {
		b.Config[k] = v
	}
	return nil
}

// ConvertCandlesToPriceData는 캔들 데이터를 지표 계산용 PriceData로 변환합니다
func ConvertCandlesToPriceData(candles domain.CandleList) []PriceData {
	priceData := make([]PriceData, len(candles))
	for i, candle := range candles {
		priceData[i] = PriceData{
			Time:   candle.OpenTime,
			Open:   candle.Open,
			High:   candle.High,
			Low:    candle.Low,
			Close:  candle.Close,
			Volume: candle.Volume,
		}
	}
	return priceData
}
