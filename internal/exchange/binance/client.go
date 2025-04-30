// internal/exchange/binance/client.go
package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// Client는 바이낸스 API 클라이언트를 구현합니다
type Client struct {
	apiKey           string
	secretKey        string
	baseURL          string
	httpClient       *http.Client
	serverTimeOffset int64 // 서버 시간과의 차이를 저장
	mu               sync.RWMutex
}

// ClientOption은 클라이언트 생성 옵션을 정의합니다
type ClientOption func(*Client)

// WithTimeout은 HTTP 클라이언트의 타임아웃을 설정합니다
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithBaseURL은 기본 URL을 설정합니다
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithTestnet은 테스트넷 사용 여부를 설정합니다
func WithTestnet(useTestnet bool) ClientOption {
	return func(c *Client) {
		if useTestnet {
			c.baseURL = "https://testnet.binancefuture.com"
		} else {
			c.baseURL = "https://fapi.binance.com"
		}
	}
}

// NewClient는 새로운 바이낸스 API 클라이언트를 생성합니다
func NewClient(apiKey, secretKey string, opts ...ClientOption) *Client {
	c := &Client{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    "https://fapi.binance.com", // 기본값은 선물 거래소
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	// 옵션 적용
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// GetServerTime은 서버 시간을 조회합니다
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
	// 기존 도메인 모델로 변환하는 부분만 변경
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/time", nil, false)
	if err != nil {
		return time.Time{}, err
	}

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return time.Time{}, fmt.Errorf("서버 시간 파싱 실패: %w", err)
	}

	return time.Unix(0, result.ServerTime*int64(time.Millisecond)), nil
}

// doRequest는 HTTP 요청을 실행하고 결과를 반환합니다
func (c *Client) doRequest(ctx context.Context, method, endpoint string, params url.Values, needSign bool) ([]byte, error) {
	// 기존 코드와 동일
	if params == nil {
		params = url.Values{}
	}

	// URL 생성
	reqURL, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, fmt.Errorf("URL 파싱 실패: %w", err)
	}

	// 타임스탬프 추가
	if needSign {
		timestamp := strconv.FormatInt(c.getServerTime(), 10)
		params.Set("timestamp", timestamp)
		params.Set("recvWindow", "5000")
	}

	// 파라미터 설정
	reqURL.RawQuery = params.Encode()

	// 서명 추가
	if needSign {
		signature := c.sign(params.Encode())
		reqURL.RawQuery = reqURL.RawQuery + "&signature=" + signature
	}

	// 요청 생성
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("요청 생성 실패: %w", err)
	}

	// 헤더 설정
	req.Header.Set("Content-Type", "application/json")
	if needSign {
		req.Header.Set("X-MBX-APIKEY", c.apiKey)
	}

	// 요청 실행
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	// 응답 읽기
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %w", err)
	}

	// 상태 코드 확인
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code    int    `json:"code"`
			Message string `json:"msg"`
		}
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return nil, fmt.Errorf("HTTP 에러(%d): %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("API 에러(코드: %d): %s", apiErr.Code, apiErr.Message)
	}

	return body, nil
}

// sign은 요청에 대한 서명을 생성합니다
func (c *Client) sign(payload string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// getServerTime은 현재 서버 시간을 반환합니다
func (c *Client) getServerTime() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UnixMilli() + c.serverTimeOffset
}

// GetKlines는 캔들 데이터를 조회합니다
func (c *Client) GetKlines(ctx context.Context, symbol string, interval domain.TimeInterval, limit int) (domain.CandleList, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("interval", string(interval))
	params.Add("limit", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var rawCandles [][]interface{}
	if err := json.Unmarshal(resp, &rawCandles); err != nil {
		return nil, fmt.Errorf("캔들 데이터 파싱 실패: %w", err)
	}

	// 기존: market.CandleData 배열 -> 새로운: domain.CandleList
	candles := make(domain.CandleList, len(rawCandles))
	for i, raw := range rawCandles {
		// 시간 변환
		openTime := int64(raw[0].(float64))
		closeTime := int64(raw[6].(float64))

		// 가격 문자열 변환
		open, _ := strconv.ParseFloat(raw[1].(string), 64)
		high, _ := strconv.ParseFloat(raw[2].(string), 64)
		low, _ := strconv.ParseFloat(raw[3].(string), 64)
		close, _ := strconv.ParseFloat(raw[4].(string), 64)
		volume, _ := strconv.ParseFloat(raw[5].(string), 64)

		candles[i] = domain.Candle{
			OpenTime:  time.Unix(openTime/1000, 0),
			CloseTime: time.Unix(closeTime/1000, 0),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Symbol:    symbol,
			Interval:  interval,
		}
	}

	return candles, nil
}

// GetSymbolInfo는 특정 심볼의 거래 정보만 조회합니다
func (c *Client) GetSymbolInfo(ctx context.Context, symbol string) (*domain.SymbolInfo, error) {
	// 요청 파라미터에 심볼 추가
	params := url.Values{}
	params.Add("symbol", symbol)

	// 특정 심볼에 대한 exchangeInfo 호출
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/exchangeInfo", params, false)
	if err != nil {
		return nil, fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// exchangeInfo 응답 구조체 정의
	var exchangeInfo struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			PricePrecision    int    `json:"pricePrecision"`
			QuantityPrecision int    `json:"quantityPrecision"`
			Filters           []struct {
				FilterType  string `json:"filterType"`
				StepSize    string `json:"stepSize,omitempty"`
				TickSize    string `json:"tickSize,omitempty"`
				MinNotional string `json:"notional,omitempty"`
			} `json:"filters"`
		} `json:"symbols"`
	}

	// JSON 응답 파싱
	if err := json.Unmarshal(resp, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("심볼 정보 파싱 실패: %w", err)
	}

	// 응답에 심볼 정보가 없는 경우
	if len(exchangeInfo.Symbols) == 0 {
		return nil, fmt.Errorf("심볼 정보를 찾을 수 없음: %s", symbol)
	}

	// 첫 번째(유일한) 심볼 정보 사용
	s := exchangeInfo.Symbols[0]

	info := &domain.SymbolInfo{
		Symbol:            symbol,
		PricePrecision:    s.PricePrecision,
		QuantityPrecision: s.QuantityPrecision,
	}

	// 필터 정보 추출
	for _, filter := range s.Filters {
		switch filter.FilterType {
		case "LOT_SIZE": // 수량 단위 필터
			if filter.StepSize != "" {
				stepSize, err := strconv.ParseFloat(filter.StepSize, 64)
				if err != nil {
					continue
				}
				info.StepSize = stepSize
			}
		case "PRICE_FILTER": // 가격 단위 필터
			if filter.TickSize != "" {
				tickSize, err := strconv.ParseFloat(filter.TickSize, 64)
				if err != nil {
					continue
				}
				info.TickSize = tickSize
			}
		case "MIN_NOTIONAL": // 최소 주문 가치 필터
			if filter.MinNotional != "" {
				minNotional, err := strconv.ParseFloat(filter.MinNotional, 64)
				if err != nil {
					continue
				}
				info.MinNotional = minNotional
			}
		}
	}

	return info, nil
}

// GetTopVolumeSymbols는 거래량 기준 상위 n개 심볼을 조회합니다
func (c *Client) GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("거래량 데이터 조회 실패: %w", err)
	}

	type symbolVolume struct {
		Symbol      string  `json:"symbol"`
		QuoteVolume float64 `json:"quoteVolume,string"`
	}

	var tickers []symbolVolume
	if err := json.Unmarshal(resp, &tickers); err != nil {
		return nil, fmt.Errorf("거래량 데이터 파싱 실패: %w", err)
	}

	// USDT 마진 선물만 필터링
	var filteredTickers []symbolVolume
	for _, ticker := range tickers {
		if strings.HasSuffix(ticker.Symbol, "USDT") {
			filteredTickers = append(filteredTickers, ticker)
		}
	}

	// 거래량 기준 내림차순 정렬
	sort.Slice(filteredTickers, func(i, j int) bool {
		return filteredTickers[i].QuoteVolume > filteredTickers[j].QuoteVolume
	})

	// 상위 n개 심볼 선택
	resultCount := min(n, len(filteredTickers))
	symbols := make([]string, resultCount)
	for i := 0; i < resultCount; i++ {
		symbols[i] = filteredTickers[i].Symbol
	}

	return symbols, nil
}

// GetBalance는 계정의 잔고를 조회합니다
func (c *Client) GetBalance(ctx context.Context) (map[string]domain.Balance, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/account", nil, true)
	if err != nil {
		return nil, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	var result struct {
		Assets []struct {
			Asset              string  `json:"asset"`
			WalletBalance      float64 `json:"walletBalance,string"`
			UnrealizedProfit   float64 `json:"unrealizedProfit,string"`
			MarginBalance      float64 `json:"marginBalance,string"`
			AvailableBalance   float64 `json:"availableBalance,string"`
			InitialMargin      float64 `json:"initialMargin,string"`
			MaintMargin        float64 `json:"maintMargin,string"`
			CrossWalletBalance float64 `json:"crossWalletBalance,string"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	balances := make(map[string]domain.Balance)
	for _, asset := range result.Assets {
		// 잔고가 있는 자산만 포함 (0보다 큰 값)
		if asset.WalletBalance > 0 {
			balances[asset.Asset] = domain.Balance{
				Asset:              asset.Asset,
				Available:          asset.AvailableBalance,
				Locked:             asset.WalletBalance - asset.AvailableBalance,
				CrossWalletBalance: asset.CrossWalletBalance,
			}
		}
	}

	return balances, nil
}

// GetPositions는 현재 열린 포지션을 조회합니다
func (c *Client) GetPositions(ctx context.Context) ([]domain.Position, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/positionRisk", nil, true)
	if err != nil {
		return nil, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	var positionsRaw []struct {
		Symbol           string  `json:"symbol"`
		PositionAmt      float64 `json:"positionAmt,string"`
		EntryPrice       float64 `json:"entryPrice,string"`
		MarkPrice        float64 `json:"markPrice,string"`
		UnrealizedProfit float64 `json:"unRealizedProfit,string"`
		LiquidationPrice float64 `json:"liquidationPrice,string"`
		Leverage         float64 `json:"leverage,string"`
		MaxNotionalValue float64 `json:"maxNotionalValue,string"`
		MarginType       string  `json:"marginType"`
		IsolatedMargin   float64 `json:"isolatedMargin,string"`
		IsAutoAddMargin  string  `json:"isAutoAddMargin"`
		PositionSide     string  `json:"positionSide"`
		Notional         float64 `json:"notional,string"`
		IsolatedWallet   float64 `json:"isolatedWallet,string"`
		UpdateTime       int64   `json:"updateTime"`
		InitialMargin    float64 `json:"initialMargin,string"`
		MaintMargin      float64 `json:"maintMargin,string"`
	}

	if err := json.Unmarshal(resp, &positionsRaw); err != nil {
		return nil, fmt.Errorf("포지션 데이터 파싱 실패: %w", err)
	}

	// 활성 포지션만 필터링 (수량이 0이 아닌 포지션)
	var positions []domain.Position
	for _, p := range positionsRaw {
		if p.PositionAmt != 0 {
			leverage := int(p.Leverage)
			position := domain.Position{
				Symbol:        p.Symbol,
				PositionSide:  domain.PositionSide(p.PositionSide),
				Quantity:      p.PositionAmt,
				EntryPrice:    p.EntryPrice,
				MarkPrice:     p.MarkPrice,
				UnrealizedPnL: p.UnrealizedProfit,
				InitialMargin: p.InitialMargin,
				MaintMargin:   p.MaintMargin,
				Leverage:      leverage,
			}
			positions = append(positions, position)
		}
	}

	return positions, nil
}

// GetOpenOrders는 현재 열린 주문 목록을 조회합니다
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]domain.OrderResponse, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/openOrders", params, true)
	if err != nil {
		return nil, fmt.Errorf("열린 주문 조회 실패: %w", err)
	}

	var ordersRaw []struct {
		OrderID       int64   `json:"orderId"`
		Symbol        string  `json:"symbol"`
		Status        string  `json:"status"`
		ClientOrderID string  `json:"clientOrderId"`
		Price         float64 `json:"price,string"`
		AvgPrice      float64 `json:"avgPrice,string"`
		OrigQty       float64 `json:"origQty,string"`
		ExecutedQty   float64 `json:"executedQty,string"`
		CumQuote      float64 `json:"cumQuote,string"`
		TimeInForce   string  `json:"timeInForce"`
		Type          string  `json:"type"`
		ReduceOnly    bool    `json:"reduceOnly"`
		ClosePosition bool    `json:"closePosition"`
		Side          string  `json:"side"`
		PositionSide  string  `json:"positionSide"`
		StopPrice     float64 `json:"stopPrice,string"`
		WorkingType   string  `json:"workingType"`
		PriceProtect  bool    `json:"priceProtect"`
		OrigType      string  `json:"origType"`
		UpdateTime    int64   `json:"updateTime"`
		Time          int64   `json:"time"`
	}

	if err := json.Unmarshal(resp, &ordersRaw); err != nil {
		return nil, fmt.Errorf("주문 데이터 파싱 실패: %w", err)
	}

	orders := make([]domain.OrderResponse, len(ordersRaw))
	for i, o := range ordersRaw {
		orders[i] = domain.OrderResponse{
			OrderID:          o.OrderID,
			Symbol:           o.Symbol,
			Status:           o.Status,
			ClientOrderID:    o.ClientOrderID,
			Price:            o.Price,
			AvgPrice:         o.AvgPrice,
			OrigQuantity:     o.OrigQty,
			ExecutedQuantity: o.ExecutedQty,
			Side:             domain.OrderSide(o.Side),
			PositionSide:     domain.PositionSide(o.PositionSide),
			Type:             domain.OrderType(o.Type),
			CreateTime:       time.Unix(0, o.Time*int64(time.Millisecond)),
		}
	}

	return orders, nil
}

// GetLeverageBrackets는 심볼의 레버리지 브라켓 정보를 조회합니다
func (c *Client) GetLeverageBrackets(ctx context.Context, symbol string) ([]domain.LeverageBracket, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/leverageBracket", params, true)
	if err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 응답 구조는 심볼 기준으로 다릅니다
	// 단일 심볼 조회시: [{"symbol":"BTCUSDT","brackets":[...]}]
	// 모든 심볼 조회시: [{"symbol":"BTCUSDT","brackets":[...]}, {"symbol":"ETHUSDT","brackets":[...]}]
	var bracketsRaw []struct {
		Symbol   string `json:"symbol"`
		Brackets []struct {
			Bracket          int     `json:"bracket"`
			InitialLeverage  int     `json:"initialLeverage"`
			NotionalCap      float64 `json:"notionalCap"`
			NotionalFloor    float64 `json:"notionalFloor"`
			MaintMarginRatio float64 `json:"maintMarginRatio"`
			Cum              float64 `json:"cum"`
		} `json:"brackets"`
	}

	if err := json.Unmarshal(resp, &bracketsRaw); err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 데이터 파싱 실패: %w", err)
	}

	// 결과 처리
	var result []domain.LeverageBracket
	for _, symbolBrackets := range bracketsRaw {

		for _, b := range symbolBrackets.Brackets {
			bracket := domain.LeverageBracket{
				Bracket:          b.Bracket,
				InitialLeverage:  b.InitialLeverage,
				MaintMarginRatio: b.MaintMarginRatio,
				Notional:         b.NotionalCap,
			}
			result = append(result, bracket)
		}

		// 특정 심볼만 요청했으면 첫 번째 항목만 필요
		if symbol != "" {
			break
		}
	}

	return result, nil
}

// PlaceOrder는 새로운 주문을 생성합니다
func (c *Client) PlaceOrder(ctx context.Context, order domain.OrderRequest) (*domain.OrderResponse, error) {
	params := url.Values{}
	params.Add("symbol", order.Symbol)
	params.Add("side", string(order.Side))

	if order.PositionSide != "" {
		params.Add("positionSide", string(order.PositionSide))
	}

	switch order.Type {
	case domain.Market:
		params.Add("type", "MARKET")
		if order.QuoteQuantity > 0 {
			// USDT 금액으로 주문
			params.Add("quoteOrderQty", strconv.FormatFloat(order.QuoteQuantity, 'f', -1, 64))
		} else {
			// 코인 수량으로 주문
			params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		}

	case domain.Limit:
		params.Add("type", "LIMIT")
		params.Add("timeInForce", order.TimeInForce)
		if order.TimeInForce == "" {
			params.Add("timeInForce", "GTC")
		}
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("price", strconv.FormatFloat(order.Price, 'f', -1, 64))

	case domain.StopMarket:
		params.Add("type", "STOP_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))

	case domain.TakeProfitMarket:
		params.Add("type", "TAKE_PROFIT_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
	}

	// 클라이언트 주문 ID가 설정되었으면 추가
	if order.ClientOrderID != "" {
		params.Add("newClientOrderId", order.ClientOrderID)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/order", params, true)
	if err != nil {
		return nil, fmt.Errorf("주문 실행 실패 [심볼: %s, 타입: %s, 수량: %.8f]: %w",
			order.Symbol, order.Type, order.Quantity, err)
	}

	var result struct {
		OrderID       int64  `json:"orderId"`
		Symbol        string `json:"symbol"`
		Status        string `json:"status"`
		ClientOrderID string `json:"clientOrderId"`
		Price         string `json:"price"`
		AvgPrice      string `json:"avgPrice"`
		OrigQty       string `json:"origQty"`
		ExecutedQty   string `json:"executedQty"`
		Side          string `json:"side"`
		PositionSide  string `json:"positionSide"`
		Type          string `json:"type"`
		UpdateTime    int64  `json:"updateTime"`
		Time          int64  `json:"time"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("주문 응답 파싱 실패: %w", err)
	}

	// 문자열을 숫자로 변환
	price, _ := strconv.ParseFloat(result.Price, 64)
	avgPrice, _ := strconv.ParseFloat(result.AvgPrice, 64)
	origQuantity, _ := strconv.ParseFloat(result.OrigQty, 64)
	executedQuantity, _ := strconv.ParseFloat(result.ExecutedQty, 64)

	return &domain.OrderResponse{
		OrderID:          result.OrderID,
		Symbol:           result.Symbol,
		Status:           result.Status,
		ClientOrderID:    result.ClientOrderID,
		Price:            price,
		AvgPrice:         avgPrice,
		OrigQuantity:     origQuantity,
		ExecutedQuantity: executedQuantity,
		Side:             domain.OrderSide(result.Side),
		PositionSide:     domain.PositionSide(result.PositionSide),
		Type:             domain.OrderType(result.Type),
		CreateTime:       time.Unix(0, result.Time*int64(time.Millisecond)),
	}, nil
}

// CancelOrder는 주문을 취소합니다
func (c *Client) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("orderId", strconv.FormatInt(orderID, 10))

	_, err := c.doRequest(ctx, http.MethodDelete, "/fapi/v1/order", params, true)
	if err != nil {
		return fmt.Errorf("주문 취소 실패: %w", err)
	}

	return nil
}

// SetLeverage는 레버리지를 설정합니다
func (c *Client) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("leverage", strconv.Itoa(leverage))

	_, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/leverage", params, true)
	if err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	return nil
}

// SetPositionMode는 포지션 모드를 설정합니다
func (c *Client) SetPositionMode(ctx context.Context, hedgeMode bool) error {
	params := url.Values{}
	params.Add("dualSidePosition", strconv.FormatBool(hedgeMode))

	_, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/positionSide/dual", params, true)
	if err != nil {
		// API 에러 타입 확인
		if strings.Contains(err.Error(), "No need to change position side") {
			// 이미 원하는 모드로 설정된 경우, 에러가 아님
			return nil
		}
		return fmt.Errorf("포지션 모드 설정 실패: %w", err)
	}

	return nil
}

// SyncTime은 바이낸스 서버와 시간을 동기화합니다
func (c *Client) SyncTime(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/time", nil, false)
	if err != nil {
		return fmt.Errorf("서버 시간 조회 실패: %w", err)
	}

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("서버 시간 파싱 실패: %w", err)
	}

	c.serverTimeOffset = result.ServerTime - time.Now().UnixMilli()
	return nil
}
