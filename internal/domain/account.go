package domain

// Balance는 계정 잔고 정보를 표현합니다
type Balance struct {
	Asset              string  // 자산 심볼 (예: USDT, BTC)
	Available          float64 // 사용 가능한 잔고
	Locked             float64 // 주문 등에 잠긴 잔고
	CrossWalletBalance float64 // 교차 마진 지갑 잔고
}

// AccountInfo는 계정 정보를 표현합니다
type AccountInfo struct {
	Balances              map[string]Balance // 자산별 잔고
	CanTrade              bool               // 거래 가능 여부
	CanDeposit            bool               // 입금 가능 여부
	CanWithdraw           bool               // 출금 가능 여부
	TotalMarginBalance    float64            // 총 마진 잔고
	TotalUnrealizedProfit float64            // 총 미실현 손익
}

// TradeInfo는 거래 실행 정보를 정의합니다
type TradeInfo struct {
	Symbol        string       // 심볼 (예: BTCUSDT)
	PositionSide  PositionSide // 포지션 방향 (LONG/SHORT)
	PositionValue float64      // 포지션 크기 (USDT)
	Quantity      float64      // 구매/판매 수량 (코인)
	EntryPrice    float64      // 진입가
	StopLoss      float64      // 손절가
	TakeProfit    float64      // 익절가
	Balance       float64      // 현재 USDT 잔고
	Leverage      int          // 사용 레버리지
}
