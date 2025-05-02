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
