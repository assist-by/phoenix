package backtest

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// Engine은 백테스트 실행 엔진입니다
type Engine struct {
	Strategy       strategy.Strategy   // 테스트할 전략
	Manager        *Manager            // 백테스트 포지션 관리자
	SymbolInfo     *domain.SymbolInfo  // 심볼 정보
	Candles        domain.CandleList   // 캔들 데이터
	Config         *config.Config      // 백테스트 설정
	IndicatorCache *IndicatorCache     // 지표 캐시
	StartTime      time.Time           // 백테스트 시작 시간
	EndTime        time.Time           // 백테스트 종료 시간
	Symbol         string              // 테스트 심볼
	Interval       domain.TimeInterval // 테스트 간격
	WarmupPeriod   int                 // 웜업 기간 (캔들 수)
}

// NewEngine은 새로운 백테스트 엔진을 생성합니다
func NewEngine(
	cfg *config.Config,
	strategy strategy.Strategy,
	symbolInfo *domain.SymbolInfo,
	candles domain.CandleList,
	symbol string,
	interval domain.TimeInterval,
) *Engine {
	// 백테스트 시작/종료 시간 결정
	startTime := candles[0].OpenTime
	endTime := candles[len(candles)-1].CloseTime

	// 심볼 정보 맵 생성
	symbolInfos := make(map[string]*domain.SymbolInfo)
	symbolInfos[symbol] = symbolInfo

	// 포지션 관리자 생성
	manager := NewManager(cfg, symbolInfos)

	// 지표 캐시 생성
	cache := NewIndicatorCache()

	// 웜업 기간 결정 (최소 200 캔들 또는 전략에 따라 조정)
	warmupPeriod := 200
	if len(candles) < warmupPeriod {
		warmupPeriod = len(candles) / 4 // 데이터가 부족한 경우 25% 사용
	}

	return &Engine{
		Strategy:       strategy,
		Manager:        manager,
		SymbolInfo:     symbolInfo,
		Candles:        candles,
		Config:         cfg,
		IndicatorCache: cache,
		StartTime:      startTime,
		EndTime:        endTime,
		Symbol:         symbol,
		Interval:       interval,
		WarmupPeriod:   warmupPeriod,
	}
}

// Run은 백테스트를 실행합니다
func (e *Engine) Run() (*Result, error) {
	// 데이터 검증
	if len(e.Candles) < e.WarmupPeriod {
		return nil, fmt.Errorf("충분한 캔들 데이터가 없습니다: 필요 %d, 현재 %d",
			e.WarmupPeriod, len(e.Candles))
	}

	// 1. 지표 사전 계산
	if err := e.prepareIndicators(); err != nil {
		return nil, fmt.Errorf("지표 계산 실패: %w", err)
	}

	log.Printf("백테스트 시작: 심볼=%s, 간격=%s, 캔들 수=%d, 웜업 기간=%d",
		e.Symbol, e.Interval, len(e.Candles), e.WarmupPeriod)

	// 2. 순차적 캔들 처리
	ctx := context.Background()
	candleMap := make(map[string]domain.Candle)

	for i := e.WarmupPeriod; i < len(e.Candles); i++ {
		currentCandle := e.Candles[i]

		// 현재 시점까지의 데이터로 서브셋 생성 (미래 정보 누수 방지)
		subsetCandles := e.Candles[:i+1]

		// 전략 분석 및 시그널 생성
		signal, err := e.Strategy.Analyze(ctx, e.Symbol, subsetCandles)
		if err != nil {
			log.Printf("전략 분석 실패 (캔들 %d): %v", i, err)
			continue
		}

		// 신호가 있는 경우 포지션 진입
		if signal != nil && signal.GetType() != domain.NoSignal {
			if _, err := e.Manager.OpenPosition(signal, currentCandle); err != nil {
				log.Printf("포지션 진입 실패 (캔들 %d) (캔들시간: %s): %v", i, err,
					currentCandle.OpenTime.Format("2006-01-02 15:04:05"))
			} else {
				sign := 1.0
				if signal.GetType() != domain.Long {
					sign = -1.0
				}

				log.Printf("포지션 진입: %s %s @ %.2f, SL: %.2f (%.2f%%), TP: %.2f (%.2f%%) (캔들시간: %s)",
					e.Symbol, signal.GetType().String(), signal.GetPrice(),
					signal.GetStopLoss(),
					(signal.GetStopLoss()-signal.GetPrice())/signal.GetPrice()*100*sign,
					signal.GetTakeProfit(),
					(signal.GetTakeProfit()-signal.GetPrice())/signal.GetPrice()*100*sign,
					currentCandle.OpenTime.Format("2006-01-02 15:04:05"))
			}
		}

		// 기존 포지션 업데이트 (TP/SL 체크)
		closedPositions := e.Manager.UpdatePositions(currentCandle, signal)

		// 청산된 포지션 로깅
		for _, pos := range closedPositions {
			log.Printf("포지션 청산: %s %s, 수익: %.2f%%, 이유: %s (캔들시간: %s)",
				pos.Symbol,
				string(pos.Side),
				pos.PnLPercentage,
				getExitReasonString(pos.ExitReason),
				currentCandle.OpenTime.Format("2006-01-02 15:04:05"))
		}

		// 계정 자산 업데이트
		candleMap[e.Symbol] = currentCandle
		e.Manager.UpdateEquity(candleMap)
	}

	// 3. 미청산 포지션 처리
	e.closeAllPositions()

	// 4. 결과 계산 및 반환
	result := e.Manager.GetBacktestResult(e.StartTime, e.EndTime, e.Symbol, e.Interval)

	log.Printf("백테스트 완료: 총 거래=%d, 승률=%.2f%%, 누적 수익률=%.2f%%, 최대 낙폭=%.2f%%",
		result.TotalTrades,
		result.WinRate,
		result.CumulativeReturn,
		result.MaxDrawdown)

	return result, nil
}

// prepareIndicators는 지표를 미리 계산하고 캐싱합니다
func (e *Engine) prepareIndicators() error {
	// 캔들 데이터를 지표 계산용 형식으로 변환
	prices := indicator.ConvertCandlesToPriceData(e.Candles)

	// 기본 지표 집합 가져오기
	indicatorSpecs := GetDefaultIndicators()

	// 지표 계산 및 캐싱
	if err := e.IndicatorCache.CacheIndicators(indicatorSpecs, prices); err != nil {
		return fmt.Errorf("지표 캐싱 실패: %w", err)
	}

	return nil
}

// closeAllPositions은 모든 열린 포지션을 마지막 가격으로 청산합니다
func (e *Engine) closeAllPositions() {
	if len(e.Candles) == 0 {
		return
	}

	// 마지막 캔들 가져오기
	lastCandle := e.Candles[len(e.Candles)-1]

	// 모든 열린 포지션 가져오기 및 청산
	positions := e.Manager.Account.Positions

	// 복사본 생성 (청산 중 슬라이스 변경 방지)
	positionsCopy := make([]*Position, len(positions))
	copy(positionsCopy, positions)

	for _, pos := range positionsCopy {
		if err := e.Manager.ClosePosition(pos, lastCandle.Close, lastCandle.CloseTime, EndOfBacktest); err != nil {
			log.Printf("백테스트 종료 시 포지션 청산 실패: %v", err)
		} else {
			log.Printf("백테스트 종료 포지션 청산: %s %s, 수익: %.2f%%",
				pos.Symbol, string(pos.Side), pos.PnLPercentage)
		}
	}
}

// getExitReasonString은 청산 이유를 문자열로 변환합니다
func getExitReasonString(reason ExitReason) string {
	switch reason {
	case StopLossHit:
		return "손절(SL)"
	case TakeProfitHit:
		return "익절(TP)"
	case SignalReversal:
		return "신호 반전"
	case EndOfBacktest:
		return "백테스트 종료"
	default:
		return "알 수 없음"
	}
}
