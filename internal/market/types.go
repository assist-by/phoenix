package market

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

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"
)

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

const (
	Long  PositionSide = "LONG"
	Short PositionSide = "SHORT"
)

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Market           OrderType = "MARKET"
	Limit            OrderType = "LIMIT"
	StopMarket       OrderType = "STOP_MARKET"
	TakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"
)

// OrderRequest는 주문 요청 정보를 표현합니다
type OrderRequest struct {
	Symbol       string
	Side         OrderSide
	PositionSide PositionSide
	Type         OrderType
	Quantity     float64
	Price        float64
	StopPrice    float64
	TimeInForce  string
}
