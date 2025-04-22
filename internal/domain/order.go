package domain

import "time"

// OrderRequest는 주문 요청 정보를 표현합니다
type OrderRequest struct {
	Symbol        string       // 심볼 (예: BTCUSDT)
	Side          OrderSide    // 매수/매도
	PositionSide  PositionSide // 롱/숏 포지션
	Type          OrderType    // 주문 유형 (시장가, 지정가 등)
	Quantity      float64      // 수량
	QuoteQuantity float64      // 명목 가치 (USDT 기준)
	Price         float64      // 지정가 (Limit 주문 시)
	StopPrice     float64      // 스탑 가격 (Stop 주문 시)
	TimeInForce   string       // 주문 유효 기간 (GTC, IOC 등)
	ClientOrderID string       // 클라이언트 측 주문 ID
}

// OrderResponse는 주문 응답을 표현합니다
type OrderResponse struct {
	OrderID          int64        // 주문 ID
	Symbol           string       // 심볼
	Status           string       // 주문 상태
	ClientOrderID    string       // 클라이언트 측 주문 ID
	Price            float64      // 주문 가격
	AvgPrice         float64      // 평균 체결 가격
	OrigQuantity     float64      // 원래 주문 수량
	ExecutedQuantity float64      // 체결된 수량
	Side             OrderSide    // 매수/매도
	PositionSide     PositionSide // 롱/숏 포지션
	Type             OrderType    // 주문 유형
	CreateTime       time.Time    // 주문 생성 시간
}

// Position은 포지션 정보를 표현합니다
type Position struct {
	Symbol        string       // 심볼 (예: BTCUSDT)
	PositionSide  PositionSide // 롱/숏 포지션
	Quantity      float64      // 포지션 수량 (양수: 롱, 음수: 숏)
	EntryPrice    float64      // 평균 진입가
	Leverage      int          // 레버리지
	MarkPrice     float64      // 마크 가격
	UnrealizedPnL float64      // 미실현 손익
	InitialMargin float64      // 초기 마진
	MaintMargin   float64      // 유지 마진
}

// SymbolInfo는 심볼의 거래 정보를 나타냅니다
type SymbolInfo struct {
	Symbol            string  // 심볼 이름 (예: BTCUSDT)
	StepSize          float64 // 수량 최소 단위 (예: 0.001 BTC)
	TickSize          float64 // 가격 최소 단위 (예: 0.01 USDT)
	MinNotional       float64 // 최소 주문 가치 (예: 10 USDT)
	PricePrecision    int     // 가격 소수점 자릿수
	QuantityPrecision int     // 수량 소수점 자릿수
}

// LeverageBracket은 레버리지 구간 정보를 나타냅니다
type LeverageBracket struct {
	Bracket          int     // 구간 번호
	InitialLeverage  int     // 최대 레버리지
	MaintMarginRatio float64 // 유지증거금 비율
	Notional         float64 // 명목가치 상한
}
