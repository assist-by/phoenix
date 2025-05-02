package backtest

import (
	"time"

	"github.com/assist-by/phoenix/internal/domain"
)

// Result는 백테스트 결과를 저장하는 구조체입니다
type Result struct {
	TotalTrades      int                 // 총 거래 횟수
	WinningTrades    int                 // 승리 거래 횟수
	LosingTrades     int                 // 패배 거래 횟수
	WinRate          float64             // 승률 (%)
	CumulativeReturn float64             // 누적 수익률 (%)
	AverageReturn    float64             // 평균 수익률 (%)
	MaxDrawdown      float64             // 최대 낙폭 (%)
	Trades           []Trade             // 개별 거래 기록
	StartTime        time.Time           // 백테스트 시작 시간
	EndTime          time.Time           // 백테스트 종료 시간
	Symbol           string              // 테스트한 심볼
	Interval         domain.TimeInterval // 테스트 간격
}

// Trade는 개별 거래 정보를 저장합니다
type Trade struct {
	EntryTime  time.Time           // 진입 시간
	ExitTime   time.Time           // 종료 시간
	EntryPrice float64             // 진입 가격
	ExitPrice  float64             // 종료 가격
	Side       domain.PositionSide // 포지션 방향
	ProfitPct  float64             // 수익률 (%)
	ExitReason string              // 종료 이유 (TP, SL, 신호 반전 등)
}

// PositionStatus는 포지션 상태를 정의합니다
type PositionStatus int

const (
	Open   PositionStatus = iota // 열린 포지션
	Closed                       // 청산된 포지션
)

// ExitReason은 포지션 청산 이유를 정의합니다
type ExitReason int

const (
	NoExit         ExitReason = iota // 청산되지 않음
	StopLossHit                      // 손절
	TakeProfitHit                    // 익절
	SignalReversal                   // 반대 신호 발생
	EndOfBacktest                    // 백테스트 종료
)

// Position은 백테스트 중 포지션 정보를 나타냅니다
type Position struct {
	Symbol        string              // 심볼 (예: BTCUSDT)
	Side          domain.PositionSide // 롱/숏 포지션
	EntryPrice    float64             // 진입가
	Quantity      float64             // 수량
	EntryTime     time.Time           // 진입 시간
	StopLoss      float64             // 손절가
	TakeProfit    float64             // 익절가
	ClosePrice    float64             // 청산가 (청산 시에만 설정)
	CloseTime     time.Time           // 청산 시간 (청산 시에만 설정)
	PnL           float64             // 손익 (청산 시에만 설정)
	PnLPercentage float64             // 손익률 % (청산 시에만 설정)
	Status        PositionStatus      // 포지션 상태
	ExitReason    ExitReason          // 청산 이유
}

// Account는 백테스트 계정 상태를 나타냅니다
type Account struct {
	InitialBalance float64     // 초기 잔고
	Balance        float64     // 현재 잔고
	Positions      []*Position // 열린 포지션
	ClosedTrades   []*Position // 청산된 포지션 기록
	Equity         float64     // 총 자산 (잔고 + 미실현 손익)
	HighWaterMark  float64     // 최고 자산 기록 (MDD 계산용)
	Drawdown       float64     // 현재 낙폭
	MaxDrawdown    float64     // 최대 낙폭
}

// TradingRules는 백테스트 트레이딩 규칙을 정의합니다
type TradingRules struct {
	MaxPositions    int     // 동시 오픈 가능한 최대 포지션 수
	MaxRiskPerTrade float64 // 거래당 최대 리스크 (%)
	SlPriority      bool    // 동일 시점에 TP/SL 모두 조건 충족시 SL 우선 적용 여부
}
