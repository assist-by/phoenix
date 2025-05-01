package macdsarema

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// 심볼별 상태를 관리하기 위한 구조체
type SymbolState struct {
	PrevMACD       float64                // 이전 MACD 값
	PrevSignal     float64                // 이전 Signal 값
	PrevHistogram  float64                // 이전 히스토그램 값
	LastSignal     domain.SignalInterface // 마지막 발생 시그널
	PendingSignal  domain.SignalType      // 대기중인 시그널 타입
	WaitedCandles  int                    // 대기한 캔들 수
	MaxWaitCandles int                    // 최대 대기 캔들 수
}

// MACDSAREMAStrategy는 MACD + SAR + EMA 전략을 구현합니다
type MACDSAREMAStrategy struct {
	strategy.BaseStrategy
	emaIndicator  *indicator.EMA  // EMA 지표
	macdIndicator *indicator.MACD // MACD 지표
	sarIndicator  *indicator.SAR  // SAR 지표

	stopLossPct    float64 // 손절 비율
	takeProfitPct  float64 // 익절 비율
	minHistogram   float64 // MACD 히스토그램 최소값 (기본값: 0.00005)
	maxWaitCandles int     // 최대 대기 캔들 수 (기본값: 5)

	states map[string]*SymbolState
	mu     sync.RWMutex
}

// NewStrategy는 새로운 MACD+SAR+EMA 전략 인스턴스를 생성합니다
func NewStrategy(config map[string]interface{}) (strategy.Strategy, error) {
	// 기본 설정값
	emaLength := 200
	stopLossPct := 0.02
	takeProfitPct := 0.04
	minHistogram := 0.00005
	maxWaitCandles := 5

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
		if val, ok := config["maxWaitCandles"].(int); ok {
			maxWaitCandles = val
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
		emaIndicator:   emaIndicator,
		macdIndicator:  macdIndicator,
		sarIndicator:   sarIndicator,
		stopLossPct:    stopLossPct,
		takeProfitPct:  takeProfitPct,
		minHistogram:   minHistogram,
		maxWaitCandles: maxWaitCandles,
		states:         make(map[string]*SymbolState),
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

	// 1. 대기 상태 확인 및 업데이트
	if state.PendingSignal != domain.NoSignal {
		pendingSignal := s.processPendingState(state, symbol, conditions, currentPrice, currentHistogram, sarBelowCandle, sarAboveCandle, currentSAR)
		if pendingSignal != nil {
			// 상태 업데이트
			state.PrevMACD = currentMACD
			state.PrevSignal = currentSignal
			state.PrevHistogram = currentHistogram
			state.LastSignal = pendingSignal
			return pendingSignal, nil
		}
	}

	// 2. 일반 시그널 조건 확인
	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= s.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signalType = domain.Long
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice + (currentPrice - stopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= s.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signalType = domain.Short
		stopLoss = currentSAR                                 // SAR 기반 손절가
		takeProfit = currentPrice - (stopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// 3. 새로운 대기 상태 설정 (일반 시그널이 아닌 경우)
	if signalType == domain.NoSignal {
		// MACD 상향돌파 + EMA 위 + SAR 캔들 아래가 아닌 경우 -> 롱 대기 상태
		if isAboveEMA && macdCross == 1 && !sarBelowCandle && currentHistogram > 0 {
			state.PendingSignal = domain.PendingLong
			state.WaitedCandles = 0
			log.Printf("[%s] Long 대기 상태 시작: MACD 상향돌파, SAR 반전 대기", symbol)
		}

		// MACD 하향돌파 + EMA 아래 + SAR이 캔들 위가 아닌 경우 → 숏 대기 상태
		if !isAboveEMA && macdCross == -1 && !sarAboveCandle && currentHistogram < 0 {
			state.PendingSignal = domain.PendingShort
			state.WaitedCandles = 0
			log.Printf("[%s] Short 대기 상태 시작: MACD 하향돌파, SAR 반전 대기", symbol)
		}
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
		state = &SymbolState{
			PendingSignal:  domain.NoSignal,
			WaitedCandles:  0,
			MaxWaitCandles: s.maxWaitCandles,
		}
		s.states[symbol] = state
		s.mu.Unlock()
	}

	return state
}

// resetPendingState는 심볼의 대기 상태를 초기화합니다
func (s *MACDSAREMAStrategy) resetPendingState(state *SymbolState) {
	state.PendingSignal = domain.NoSignal
	state.WaitedCandles = 0
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

// processPendingState는 대기 상태를 처리하고 시그널을 생성합니다
func (s *MACDSAREMAStrategy) processPendingState(
	state *SymbolState,
	symbol string,
	conditions map[string]interface{},
	currentPrice float64,
	currentHistogram float64,
	sarBelowCandle bool,
	sarAboveCandle bool,
	currentSAR float64,
) domain.SignalInterface {
	// 캔들 카운트 증가
	state.WaitedCandles++

	// 최대 대기 시간 초과 체크
	if state.WaitedCandles > state.MaxWaitCandles {
		log.Printf("[%s] 대기 상태 취소: 최대 대기 캔들 수 (%d) 초과", symbol, state.MaxWaitCandles)
		s.resetPendingState(state)
		return nil
	}

	var resultSignal domain.SignalInterface = nil
	var stopLoss, takeProfit float64
	var resultType domain.SignalType = domain.NoSignal

	// Long 대기 상태 처리
	if state.PendingSignal == domain.PendingLong {
		// 히스토그램이 음수로 바뀌면 취소(추세 역전)
		if currentHistogram < 0 && state.PrevHistogram > 0 {
			log.Printf("[%s] Long 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			s.resetPendingState(state)
			return nil
		}

		// SAR가 캔들 아래로 이동하면 롱 시그널 생성
		if sarBelowCandle {
			resultType = domain.Long
			stopLoss = currentSAR
			takeProfit = currentPrice + (currentPrice - stopLoss)

			log.Printf("[%s] Long 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			s.resetPendingState(state)
			// return resultSignal
		}
	}

	// Short 대기 상태 처리
	if state.PendingSignal == domain.PendingShort {
		// 히스토그램이 양수로 바뀌면 취소 (추세 역전)
		if currentHistogram > 0 && state.PrevHistogram < 0 {
			log.Printf("[%s] Short 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			s.resetPendingState(state)
			return nil
		}

		// SAR이 캔들 위로 이동하면 숏 시그널 생성
		if sarAboveCandle {
			resultType = domain.Short
			stopLoss = currentSAR
			takeProfit = currentPrice - (stopLoss - currentPrice)

			log.Printf("[%s] Short 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			s.resetPendingState(state)
			// return resultSignal
		}
	}

	// 최종 시그널 생성
	if resultType != domain.NoSignal {
		macdSignal := &MACDSAREMASignal{
			BaseSignal: domain.NewBaseSignal(
				resultType,
				symbol,
				currentPrice,
				time.Now(), // or use a proper timestamp
				stopLoss,
				takeProfit,
			),
			// 특화 필드 설정
			EMAValue:    conditions["EMAValue"].(float64),
			EMAAbove:    conditions["EMALong"].(bool),
			MACDValue:   conditions["MACDValue"].(float64),
			SignalValue: conditions["SignalValue"].(float64),
			SARValue:    conditions["SARValue"].(float64),
			SARBelow:    conditions["SARLong"].(bool),
			MACDCross:   getMACDCrossValue(conditions),
			Histogram:   conditions["MACDValue"].(float64) - conditions["SignalValue"].(float64),
		}

		// 조건 정보 설정
		for k, v := range conditions {
			macdSignal.SetCondition(k, v)
		}

		resultSignal = macdSignal
	}

	return resultSignal
}

func getMACDCrossValue(conditions map[string]interface{}) int {
	if conditions["MACDLong"].(bool) {
		return 1 // 상향돌파
	} else if conditions["MACDShort"].(bool) {
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
