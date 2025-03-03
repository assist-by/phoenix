package market

import "fmt"

// APIError는 바이낸스 API 에러를 표현합니다
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("바이낸스 API 에러(코드: %d): %s", e.Code, e.Message)
}

// 에러 코드 상수 정의
const (
	ErrPositionModeNoChange = -4059 // 포지션 모드 변경 불필요 에러
)

// CandleData는 캔들 데이터를 표현합니다
type CandleData struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open,string"`
	High                float64 `json:"high,string"`
	Low                 float64 `json:"low,string"`
	Close               float64 `json:"close,string"`
	Volume              float64 `json:"volume,string"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume,string"`
	NumberOfTrades      int     `json:"numberOfTrades"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume,string"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume,string"`
}

// Balance는 계정 잔고 정보를 표현합니다
type Balance struct {
	Asset              string  `json:"asset"`
	Available          float64 `json:"available"`
	Locked             float64 `json:"locked"`
	CrossWalletBalance float64 `json:"crossWalletBalance"`
}

// OrderSide는 주문 방향을 정의합니다
type OrderSide string

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"

	Long  PositionSide = "LONG"
	Short PositionSide = "SHORT"

	Market  OrderType = "MARKET"
	Limit   OrderType = "LIMIT"
	StopOCO OrderType = "STOP_OCO"

	StopMarket       OrderType = "STOP_MARKET"
	TakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"
)

// OrderRequest는 주문 요청 정보를 표현합니다
type OrderRequest struct {
	Symbol        string
	Side          OrderSide
	PositionSide  PositionSide
	Type          OrderType
	Quantity      float64 // 코인 개수
	QuoteOrderQty float64 // USDT 가치 (추가됨)
	Price         float64 // 진입가격 (리밋 주문)
	StopPrice     float64 // 손절가격
	TakeProfit    float64 // 익절가격
}

// OrderResponse는 주문 응답을 표현합니다
type OrderResponse struct {
	OrderID          int64        `json:"orderId"`
	Symbol           string       `json:"symbol"`
	Status           string       `json:"status"`
	ClientOrderID    string       `json:"clientOrderId"`
	Price            float64      `json:"price,string"`
	AvgPrice         float64      `json:"avgPrice,string"`
	OrigQuantity     float64      `json:"origQty,string"`
	ExecutedQuantity float64      `json:"executedQty,string"`
	Side             string       `json:"side"`
	PositionSide     PositionSide `json:"positionSide"`
	Type             string       `json:"type"`
	CreateTime       int64        `json:"time"`
}

// SymbolVolume은 심볼의 거래량 정보를 표현합니다
type SymbolVolume struct {
	Symbol      string  `json:"symbol"`
	QuoteVolume float64 `json:"quoteVolume,string"`
}

type PositionInfo struct {
	Symbol       string  `json:"symbol"`
	PositionSide string  `json:"positionSide"`
	Quantity     float64 `json:"positionAmt,string"`
	EntryPrice   float64 `json:"entryPrice,string"`
}

// LeverageBracket은 레버리지 구간 정보를 나타냅니다
type LeverageBracket struct {
	Bracket          int     `json:"bracket"`          // 구간 번호
	InitialLeverage  int     `json:"initialLeverage"`  // 최대 레버리지
	MaintMarginRatio float64 `json:"maintMarginRatio"` // 유지증거금 비율
	Notional         float64 `json:"notional"`         // 명목가치 상한
}

// SymbolBrackets는 심볼별 레버리지 구간 정보를 나타냅니다
type SymbolBrackets struct {
	Symbol   string            `json:"symbol"`
	Brackets []LeverageBracket `json:"brackets"`
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
