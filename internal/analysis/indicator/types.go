package indicator

import (
	"fmt"
	"time"
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

// Result는 지표 계산 결과를 정의합니다
type Result struct {
	Value     float64   // 지표값
	Timestamp time.Time // 계산 시점
}

// ValidationError는 입력값 검증 에러를 정의합니다
type ValidationError struct {
	Field string
	Err   error
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("유효하지 않은 %s: %v", e.Field, e.Err)
}
