package market

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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

// sign은 요청에 대한 서명을 생성합니다
func (c *Client) sign(payload string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// doRequest는 HTTP 요청을 실행하고 결과를 반환합니다
func (c *Client) doRequest(ctx context.Context, method, endpoint string, params url.Values, needSign bool) ([]byte, error) {
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
		// 서버 시간으로 타임스탬프 설정
		timestamp := strconv.FormatInt(c.getServerTime(), 10)
		params.Set("timestamp", timestamp)
		// recvWindow 설정 (선택적)
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

// GetServerTime은 서버 시간을 조회합니다
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
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

// GetKlines는 캔들 데이터를 조회합니다
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]CandleData, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("interval", interval)
	params.Add("limit", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var rawCandles [][]interface{}
	if err := json.Unmarshal(resp, &rawCandles); err != nil {
		return nil, fmt.Errorf("캔들 데이터 파싱 실패: %w", err)
	}

	candles := make([]CandleData, len(rawCandles))
	for i, raw := range rawCandles {
		candles[i] = CandleData{
			OpenTime:  int64(raw[0].(float64)),
			CloseTime: int64(raw[6].(float64)),
		}
		// 숫자 문자열을 float64로 변환
		if open, err := strconv.ParseFloat(raw[1].(string), 64); err == nil {
			candles[i].Open = open
		}
		if high, err := strconv.ParseFloat(raw[2].(string), 64); err == nil {
			candles[i].High = high
		}
		if low, err := strconv.ParseFloat(raw[3].(string), 64); err == nil {
			candles[i].Low = low
		}
		if close, err := strconv.ParseFloat(raw[4].(string), 64); err == nil {
			candles[i].Close = close
		}
		if volume, err := strconv.ParseFloat(raw[5].(string), 64); err == nil {
			candles[i].Volume = volume
		}
	}

	return candles, nil
}

// GetBalance는 계정의 잔고를 조회합니다
func (c *Client) GetBalance(ctx context.Context) (map[string]Balance, error) {
	params := url.Values{}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/account", params, true)
	if err != nil {
		return nil, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	var result struct {
		Assets []struct {
			Asset            string  `json:"asset"`
			WalletBalance    float64 `json:"walletBalance,string"`
			UnrealizedProfit float64 `json:"unrealizedProfit,string"`
			MarginBalance    float64 `json:"marginBalance,string"`
			AvailableBalance float64 `json:"availableBalance,string"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	balances := make(map[string]Balance)
	for _, asset := range result.Assets {
		if asset.WalletBalance > 0 {
			balances[asset.Asset] = Balance{
				Asset:              asset.Asset,
				Available:          asset.AvailableBalance,
				Locked:             asset.WalletBalance - asset.AvailableBalance,
				CrossWalletBalance: asset.WalletBalance,
			}
		}
	}

	return balances, nil
}

// PlaceOrder는 새로운 주문을 생성합니다
// PlaceOrder는 새로운 주문을 생성합니다
func (c *Client) PlaceOrder(ctx context.Context, order OrderRequest) (*OrderResponse, error) {
	params := url.Values{}
	params.Add("symbol", order.Symbol)
	params.Add("side", string(order.Side))

	if order.PositionSide != "" {
		params.Add("positionSide", string(order.PositionSide))
	}

	var endpoint string = "/fapi/v1/order"

	switch order.Type {
	case Market:
		params.Add("type", "MARKET")
		if order.QuoteOrderQty > 0 {
			// USDT 금액으로 주문 (추가된 부분)
			params.Add("quoteOrderQty", strconv.FormatFloat(order.QuoteOrderQty, 'f', -1, 64))
		} else {
			// 기존 방식 (코인 수량으로 주문)
			params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		}

	case Limit:
		params.Add("type", "LIMIT")
		params.Add("timeInForce", "GTC")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("price", strconv.FormatFloat(order.Price, 'f', -1, 64))

	case StopMarket:
		params.Add("type", "STOP_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))

	case TakeProfitMarket:
		params.Add("type", "TAKE_PROFIT_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
	}

	resp, err := c.doRequest(ctx, http.MethodPost, endpoint, params, true)
	if err != nil {
		return nil, fmt.Errorf("주문 실행 실패: %w", err)
	}

	var result OrderResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("주문 응답 파싱 실패: %w", err)
	}

	return &result, nil
}

// GetSymbolInfo는 특정 심볼의 거래 정보만 조회합니다
func (c *Client) GetSymbolInfo(ctx context.Context, symbol string) (*SymbolInfo, error) {
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

	info := &SymbolInfo{
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
					log.Printf("LOT_SIZE 파싱 오류: %v", err)
					continue
				}
				info.StepSize = stepSize
			}
		case "PRICE_FILTER": // 가격 단위 필터
			if filter.TickSize != "" {
				tickSize, err := strconv.ParseFloat(filter.TickSize, 64)
				if err != nil {
					log.Printf("PRICE_FILTER 파싱 오류: %v", err)
					continue
				}
				info.TickSize = tickSize
			}
		case "MIN_NOTIONAL": // 최소 주문 가치 필터
			if filter.MinNotional != "" {
				minNotional, err := strconv.ParseFloat(filter.MinNotional, 64)
				if err != nil {
					log.Printf("MIN_NOTIONAL 파싱 오류: %v", err)
					continue
				}
				info.MinNotional = minNotional
			}
		}
	}

	// 정보 로깅
	log.Printf("심볼 정보 조회: %s (최소단위: %.8f, 가격단위: %.8f, 최소주문가치: %.2f)",
		info.Symbol, info.StepSize, info.TickSize, info.MinNotional)

	return info, nil
}

// func (c *Client) PlaceOrder(ctx context.Context, order OrderRequest) (*OrderResponse, error) {
// 	params := url.Values{}
// 	params.Add("symbol", order.Symbol)
// 	params.Add("side", string(order.Side))
// 	params.Add("type", string(order.Type))
// 	params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
// 	params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
// 	params.Add("stopLimitPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
// 	params.Add("price", strconv.FormatFloat(order.TakeProfit, 'f', -1, 64))

// 	if order.PositionSide != "" {
// 		params.Add("positionSide", string(order.PositionSide))
// 	}

// 	resp, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/order/oco", params, true)
// 	if err != nil {
// 		return nil, fmt.Errorf("OCO 주문 실행 실패: %w", err)
// 	}

// 	var result OrderResponse
// 	if err := json.Unmarshal(resp, &result); err != nil {
// 		return nil, fmt.Errorf("주문 응답 파싱 실패: %w", err)
// 	}

// 	return &result, nil
// }

// // PlaceTPSLOrder는 손절/익절 주문을 생성합니다
// func (c *Client) PlaceTPSLOrder(ctx context.Context, mainOrder *OrderResponse, stopLoss, takeProfit float64) error {
// 	if stopLoss > 0 {
// 		slOrder := OrderRequest{
// 			Symbol:       mainOrder.Symbol,
// 			Side:         getOppositeOrderSide(OrderSide(mainOrder.Side)),
// 			Type:         StopMarket,
// 			Quantity:     mainOrder.ExecutedQuantity,
// 			StopPrice:    stopLoss,
// 			PositionSide: mainOrder.PositionSide,
// 		}
// 		if _, err := c.PlaceOrder(ctx, slOrder); err != nil {
// 			return fmt.Errorf("손절 주문 실패: %w", err)
// 		}
// 	}

// 	if takeProfit > 0 {
// 		tpOrder := OrderRequest{
// 			Symbol:       mainOrder.Symbol,
// 			Side:         getOppositeOrderSide(OrderSide(mainOrder.Side)),
// 			Type:         TakeProfitMarket,
// 			Quantity:     mainOrder.ExecutedQuantity,
// 			StopPrice:    takeProfit,
// 			PositionSide: mainOrder.PositionSide,
// 		}
// 		if _, err := c.PlaceOrder(ctx, tpOrder); err != nil {
// 			return fmt.Errorf("익절 주문 실패: %w", err)
// 		}
// 	}

// 	return nil
// }

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
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == ErrPositionModeNoChange {
			return nil // 이미 원하는 모드로 설정된 경우
		}
		// 문자열 검사 추가
		if strings.Contains(err.Error(), "No need to change position side") {
			return nil
		}
		return fmt.Errorf("포지션 모드 설정 실패: %w", err)
	}
	return nil
}

// getOppositeOrderSide는 주문의 반대 방향을 반환합니다
func getOppositeOrderSide(side OrderSide) OrderSide {
	if side == Buy {
		return Sell
	}
	return Buy
}

// GetTopVolumeSymbols는 거래량 기준 상위 n개 심볼을 조회합니다
func (c *Client) GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("거래량 데이터 조회 실패: %w", err)
	}

	var tickers []SymbolVolume
	if err := json.Unmarshal(resp, &tickers); err != nil {
		return nil, fmt.Errorf("거래량 데이터 파싱 실패: %w", err)
	}

	// USDT 마진 선물만 필터링
	var filteredTickers []SymbolVolume
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

	// 거래량 로깅 (시각화)
	if len(filteredTickers) > 0 {
		maxVolume := filteredTickers[0].QuoteVolume
		log.Println("\n=== 상위 거래량 심볼 ===")
		for i := 0; i < resultCount; i++ {
			ticker := filteredTickers[i]
			barLength := int((ticker.QuoteVolume / maxVolume) * 50) // 최대 50칸
			bar := strings.Repeat("=", barLength)
			log.Printf("%-12s %15.2f USDT ||%s\n",
				ticker.Symbol, ticker.QuoteVolume, bar)
		}
		log.Println("========================")
	}

	return symbols, nil
}

// GetPositions는 현재 열림 포지션을 조회합니다
func (c *Client) GetPositions(ctx context.Context) ([]PositionInfo, error) {
	params := url.Values{}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/positionRisk", params, true)
	if err != nil {
		return nil, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	var positions []PositionInfo
	if err := json.Unmarshal(resp, &positions); err != nil {
		return nil, fmt.Errorf("포지션 데이터 파싱 실패: %w", err)
	}

	activePositions := []PositionInfo{}
	for _, p := range positions {
		if p.Quantity != 0 {
			activePositions = append(activePositions, p)
		}
	}
	return activePositions, nil
}

// GetLeverageBrackets는 심볼의 레버리지 브라켓 정보를 조회합니다
func (c *Client) GetLeverageBrackets(ctx context.Context, symbol string) ([]SymbolBrackets, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/leverageBracket", params, true)
	if err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	var brackets []SymbolBrackets
	if err := json.Unmarshal(resp, &brackets); err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 데이터 파싱 실패: %w", err)
	}

	return brackets, nil
}

// =================================
// 시간 관련된 함수

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

// getServerTime은 현재 서버 시간을 반환합니다
func (c *Client) getServerTime() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UnixMilli() + c.serverTimeOffset
}
