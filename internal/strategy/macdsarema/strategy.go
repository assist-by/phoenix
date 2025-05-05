package macdsarema

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// 심볼별 상태를 관리하기 위한 구조체
type SymbolState struct {
	PrevMACD      float64                // 이전 MACD 값
	PrevSignal    float64                // 이전 Signal 값
	PrevHistogram float64                // 이전 히스토그램 값
	LastSignal    domain.SignalInterface // 마지막 발생 시그널
}

// MACDSAREMAStrategy는 MACD + SAR + EMA 전략을 구현합니다
type MACDSAREMAStrategy struct {
	strategy.BaseStrategy
	emaIndicator  *indicator.EMA  // EMA 지표
	macdIndicator *indicator.MACD // MACD 지표
	sarIndicator  *indicator.SAR  // SAR 지표

	stopLossPct   float64 // 손절 비율
	takeProfitPct float64 // 익절 비율
	minHistogram  float64 // MACD 히스토그램 최소값 (기본값: 0.00005)

	states map[string]*SymbolState
	mu     sync.RWMutex
}

// NewStrategy는 새로운 MACD+SAR+EMA 전략 인스턴스를 생성합니다
func NewStrategy(config map[string]interface{}) (strategy.Strategy, error) {
	// 기본 설정값
	emaLength := 200
	stopLossPct := 0.02
	takeProfitPct := 0.04
	minHistogram := 0.005

	// 설정에서 값 로드
	if config != nil {
		if val, ok := config["emaLength"].(int); ok {
			emaLength = val
		}
		if val, ok := config["stopLossPct"].(float64); ok {
			stopLossPct = val
		}
		if val, ok := config["takeProfitPct"].(float64); ok {
			takeProfitPct = val
		}
		if val, ok := config["minHistogram"].(float64); ok {
			minHistogram = val
		}
	}

	// 필요한 지표 인스턴스 생성
	emaIndicator := indicator.NewEMA(emaLength)
	macdIndicator := indicator.NewMACD(12, 26, 9) // 기본 MACD 설정
	sarIndicator := indicator.NewDefaultSAR()     // 기본 SAR 설정

	s := &MACDSAREMAStrategy{
		BaseStrategy: strategy.BaseStrategy{
			Name:        "MACD+SAR+EMA",
			Description: "MACD, Parabolic SAR, 200 EMA를 조합한 트렌드 팔로잉 전략",
			Config:      config,
		},
		emaIndicator:  emaIndicator,
		macdIndicator: macdIndicator,
		sarIndicator:  sarIndicator,
		stopLossPct:   stopLossPct,
		takeProfitPct: takeProfitPct,
		minHistogram:  minHistogram,
		states:        make(map[string]*SymbolState),
	}

	return s, nil
}

// Initialize는 전략을 초기화합니다
func (s *MACDSAREMAStrategy) Initialize(ctx context.Context) error {
	// 필요한 초기화 작업 수행
	log.Printf("전략 초기화: %s", s.GetName())
	return nil
}

// Analyze는 주어진 캔들 데이터를 분석하여 매매 신호를 생성합니다
func (s *MACDSAREMAStrategy) Analyze(ctx context.Context, symbol string, candles domain.CandleList) (domain.SignalInterface, error) {
	// 데이터 검증
	emaLength := s.emaIndicator.Period
	if len(candles) < emaLength {
		return nil, fmt.Errorf("insufficient data: need at least %d candles", emaLength)
	}

	// 캔들 데이터를 지표 계산에 필요한 형식으로 변환
	prices := indicator.ConvertCandlesToPriceData(candles)

	// 심볼별 상태 가져오기
	state := s.getSymbolState(symbol)

	// 지표 계산
	emaResults, err := s.emaIndicator.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("calculating EMA: %w", err)
	}

	macdResults, err := s.macdIndicator.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("calculating MACD: %w", err)
	}

	sarResults, err := s.sarIndicator.Calculate(prices)
	if err != nil {
		return nil, fmt.Errorf("calculating SAR: %w", err)
	}

	// 마지막 캔들 정보
	lastCandle := prices[len(prices)-1]
	currentPrice := lastCandle.Close

	// 필요한 지표 값 추출
	lastEMA := emaResults[len(emaResults)-1].(indicator.EMAResult)
	currentEMA := lastEMA.Value

	var currentMACD, currentSignal, currentHistogram float64
	// MACD 결과에서 마지막 유효한 값 찾기
	for i := len(macdResults) - 1; i >= 0; i-- {
		if macdResults[i] != nil {
			macdResult := macdResults[i].(indicator.MACDResult)
			currentMACD = macdResult.MACD
			currentSignal = macdResult.Signal
			currentHistogram = macdResult.Histogram
			break
		}
	}

	lastSAR := sarResults[len(sarResults)-1].(indicator.SARResult)
	currentSAR := lastSAR.SAR

	// 현재 캔들 고가와 저가
	currentHigh := lastCandle.High
	currentLow := lastCandle.Low

	// EMA 및 SAR 조건 확인
	isAboveEMA := currentPrice > currentEMA
	sarBelowCandle := currentSAR < currentLow
	sarAboveCandle := currentSAR > currentHigh

	// MACD 크로스 확인
	macdCross := s.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 시그널 객체 초기화
	signalType := domain.NoSignal
	var stopLoss, takeProfit float64

	// 조건 맵 생성 (기존과 동일)
	conditions := map[string]interface{}{
		"EMALong":     isAboveEMA,
		"EMAShort":    !isAboveEMA,
		"MACDLong":    macdCross == 1,
		"MACDShort":   macdCross == -1,
		"SARLong":     sarBelowCandle,
		"SARShort":    !sarBelowCandle,
		"EMAValue":    currentEMA,
		"MACDValue":   currentMACD,
		"SignalValue": currentSignal,
		"SARValue":    currentSAR,
	}

	// 지표 계산 후 로깅 추가
	log.Printf("DEBUG [%s]: price=%.2f, EMA=%.2f, MACD=%.5f, Signal=%.5f, Hist=%.5f, SAR=%.2f, isAboveEMA=%v, sarBelowCandle=%v, macdCross=%d (시간 %s)",
		symbol, currentPrice, currentEMA, currentMACD, currentSignal, currentHistogram, currentSAR, isAboveEMA, sarBelowCandle, macdCross, lastCandle.Time)

	// Long 시그널 조건 직전에 조건 로깅 추가
	log.Printf("LONG 조건 검사 [%s]: EMA above=%v, MACD cross=%v, Histogram=%.5f (min=%.5f), SAR below=%v",
		symbol, isAboveEMA, macdCross == 1, currentHistogram, s.minHistogram, sarBelowCandle)

	// 1. 일반 시그널 조건 확인
	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= s.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signalType = domain.Long
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice + (currentPrice - stopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f (시간: %s)",
			symbol, currentPrice, currentEMA, currentSAR,
			lastCandle.Time.Format("2006-01-02 15:04:05"))
	}

	// Short 시그널 조건 직전에 조건 로깅 추가
	log.Printf("SHORT 조건 검사 [%s]: EMA below=%v, MACD cross=%v, Histogram=%.5f (min=%.5f), SAR above=%v",
		symbol, !isAboveEMA, macdCross == -1, -currentHistogram, s.minHistogram, sarAboveCandle)

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= s.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signalType = domain.Short
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice - (stopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f (시간: %s)",
			symbol, currentPrice, currentEMA, currentSAR,
			lastCandle.Time.Format("2006-01-02 15:04:05"))
	}

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal
	state.PrevHistogram = currentHistogram

	macdSignal := &MACDSAREMASignal{
		BaseSignal: domain.NewBaseSignal(
			signalType,
			symbol,
			currentPrice,
			lastCandle.Time,
			stopLoss,
			takeProfit,
		),
		EMAValue:    currentEMA,
		EMAAbove:    isAboveEMA,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		Histogram:   currentHistogram,
		MACDCross:   macdCross,
		SARValue:    currentSAR,
		SARBelow:    sarBelowCandle,
	}

	// 조건 정보 설정
	for k, v := range conditions {
		macdSignal.SetCondition(k, v)
	}

	// 시그널이 생성되었으면 상태에 저장
	if signalType != domain.NoSignal {
		state.LastSignal = macdSignal
	}

	return macdSignal, nil
}

// getSymbolState는 심볼별 상태를 가져옵니다
func (s *MACDSAREMAStrategy) getSymbolState(symbol string) *SymbolState {
	s.mu.RLock()
	state, exists := s.states[symbol]
	s.mu.RUnlock()

	if !exists {
		s.mu.Lock()
		state = &SymbolState{}
		s.states[symbol] = state
		s.mu.Unlock()
	}

	return state
}

// checkMACDCross는 MACD 크로스를 확인합니다
// 반환값: 1 (상향돌파), -1 (하향돌파), 0 (크로스 없음)
func (s *MACDSAREMAStrategy) checkMACDCross(currentMACD, currentSignal, prevMACD, prevSignal float64) int {
	if prevMACD <= prevSignal && currentMACD > currentSignal {
		return 1 // 상향돌파
	}
	if prevMACD >= prevSignal && currentMACD < currentSignal {
		return -1 // 하향돌파
	}
	return 0 // 크로스 없음
}

// CalculateTPSL은 현재 SAR 값을 기반으로 TP/SL 가격을 계산합니다
func (s *MACDSAREMAStrategy) CalculateTPSL(
	ctx context.Context,
	symbol string,
	entryPrice float64,
	signalType domain.SignalType,
	currentSAR float64, // SAR 값을 파라미터로 받음
	symbolInfo *domain.SymbolInfo, // 심볼 정보도 파라미터로 받음
) (stopLoss, takeProfit float64) {
	isLong := signalType == domain.Long || signalType == domain.PendingLong

	// SAR 기반 손절가 및 1:1 비율 익절가 계산
	if isLong {
		stopLoss = domain.AdjustPrice(currentSAR, symbolInfo.TickSize, symbolInfo.PricePrecision)
		// 1:1 비율로 익절가 설정
		tpDistance := entryPrice - stopLoss
		takeProfit = domain.AdjustPrice(entryPrice+tpDistance, symbolInfo.TickSize, symbolInfo.PricePrecision)
	} else {
		stopLoss = domain.AdjustPrice(currentSAR, symbolInfo.TickSize, symbolInfo.PricePrecision)
		// 1:1 비율로 익절가 설정
		tpDistance := stopLoss - entryPrice
		takeProfit = domain.AdjustPrice(entryPrice-tpDistance, symbolInfo.TickSize, symbolInfo.PricePrecision)
	}

	return stopLoss, takeProfit
}

// RegisterStrategy는 이 전략을 레지스트리에 등록합니다
func RegisterStrategy(registry *strategy.Registry) {
	registry.Register("MACD+SAR+EMA", NewStrategy)
}
