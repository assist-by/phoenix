package signal

import (
	"fmt"
	"log"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// DetectorConfig는 시그널 감지기 설정을 정의합니다
type DetectorConfig struct {
	EMALength      int // EMA 기간 (기본값: 200)
	StopLossPct    float64
	TakeProfitPct  float64
	MinHistogram   float64 // 최소 MACD 히스토그램 값 (기본값: 0.00005)
	MaxWaitCandles int     // 최대 대기 캔들 수 (기본값: 5)
}

// NewDetector는 새로운 시그널 감지기를 생성합니다
func NewDetector(config DetectorConfig) *Detector {
	return &Detector{
		states:         make(map[string]*SymbolState),
		emaLength:      config.EMALength,
		stopLossPct:    config.StopLossPct,
		takeProfitPct:  config.TakeProfitPct,
		minHistogram:   config.MinHistogram,
		maxWaitCandles: config.MaxWaitCandles,
	}
}

// Detect는 주어진 데이터로부터 시그널을 감지합니다
func (d *Detector) Detect(symbol string, prices []indicator.PriceData) (*Signal, error) {
	if len(prices) < d.emaLength {
		return nil, fmt.Errorf("insufficient data: need at least %d prices", d.emaLength)
	}

	// 심볼별 상태 가져오기
	state := d.getSymbolState(symbol)

	// 지표 계산
	ema, err := indicator.EMA(prices, indicator.EMAOption{Period: d.emaLength})
	if err != nil {
		return nil, fmt.Errorf("calculating EMA: %w", err)
	}

	macd, err := indicator.MACD(prices, indicator.MACDOption{
		ShortPeriod:  12,
		LongPeriod:   26,
		SignalPeriod: 9,
	})
	if err != nil {
		return nil, fmt.Errorf("calculating MACD: %w", err)
	}

	sar, err := indicator.SAR(prices, indicator.DefaultSAROption())
	if err != nil {
		return nil, fmt.Errorf("calculating SAR: %w", err)
	}

	currentPrice := prices[len(prices)-1].Close
	currentMACD := macd[len(macd)-1].MACD
	currentSignal := macd[len(macd)-1].Signal
	currentHistogram := currentMACD - currentSignal
	currentEMA := ema[len(ema)-1].Value
	currentSAR := sar[len(sar)-1].SAR
	currentHigh := prices[len(prices)-1].High
	currentLow := prices[len(prices)-1].Low

	/// EMA 및 SAR 조건 확인
	isAboveEMA := currentPrice > currentEMA
	sarBelowCandle := currentSAR < currentLow
	sarAboveCandle := currentSAR > currentHigh

	// MACD 크로스 확인 - 이제 심볼별 상태 사용
	macdCross := d.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 기본 시그널 객체 생성
	signal := &Signal{
		Type:      NoSignal,
		Symbol:    symbol,
		Price:     currentPrice,
		Timestamp: prices[len(prices)-1].Time,
	}

	// 시그널 조건
	signal.Conditions = SignalConditions{
		EMALong:     isAboveEMA,
		EMAShort:    !isAboveEMA,
		MACDLong:    macdCross == 1,
		MACDShort:   macdCross == -1,
		SARLong:     sarBelowCandle,
		SARShort:    !sarBelowCandle,
		EMAValue:    currentEMA,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		SARValue:    currentSAR,
	}

	// 1. 대기 상태 확인 및 업데이트
	if state.PendingSignal != NoSignal {
		pendingSignal := d.processPendingState(state, symbol, signal, currentHistogram, sarBelowCandle, sarAboveCandle)
		if pendingSignal != nil {
			// 상태 업데이트
			state.PrevMACD = currentMACD
			state.PrevSignal = currentSignal
			state.PrevHistogram = currentHistogram
			state.LastSignal = pendingSignal
			return pendingSignal, nil
		}
	}

	// 2. 일반 시그널 조건 확인 (대기 상태가 없거나 취소된 경우)

	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= d.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signal.Type = Long
		signal.StopLoss = currentSAR                                        // SAR 기반 손절가
		signal.TakeProfit = currentPrice + (currentPrice - signal.StopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= d.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signal.Type = Short
		signal.StopLoss = currentSAR                                        // SAR 기반 손절가
		signal.TakeProfit = currentPrice - (signal.StopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// 3. 새로운 대기 상태 설정 (일반 시그널이 아닌 경우)
	if signal.Type == NoSignal {
		// MACD 상향돌파 + EMA 위 + SAR 캔들 아래가 아닌 경우 -> 롱 대기 상태
		if isAboveEMA && macdCross == 1 && !sarBelowCandle && currentHistogram > 0 {
			state.PendingSignal = PendingLong
			state.WaitedCandles = 0
			log.Printf("[%s] Long 대기 상태 시작: MACD 상향돌파, SAR 반전 대기", symbol)
		}

		// MACD 하향돌파 + EMA 아래 + SAR이 캔들 위가 아닌 경우 → 숏 대기 상태
		if !isAboveEMA && macdCross == -1 && !sarAboveCandle && currentHistogram < 0 {
			state.PendingSignal = PendingShort
			state.WaitedCandles = 0
			log.Printf("[%s] Short 대기 상태 시작: MACD 하향돌파, SAR 반전 대기", symbol)
		}
	}

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal
	state.PrevHistogram = currentHistogram

	if signal.Type != NoSignal {
		state.LastSignal = signal
	}

	state.LastSignal = signal
	return signal, nil
}

// processPendingState는 대기 상태를 처리하고 시그널을 생성합니다
func (d *Detector) processPendingState(state *SymbolState, symbol string, baseSignal *Signal, currentHistogram float64, sarBelowCandle bool, sarAboveCandle bool) *Signal {
	// 캔들 카운트 증가
	state.WaitedCandles++

	// 최대 대기 시간 초과 체크
	if state.WaitedCandles > state.MaxWaitCandles {
		log.Printf("[%s] 대기 상태 취소: 최대 대기 캔들 수 (%d) 초과", symbol, state.MaxWaitCandles)
		d.resetPendingState(state)
		return nil
	}

	resultSignal := &Signal{
		Type:       NoSignal,
		Symbol:     baseSignal.Symbol,
		Price:      baseSignal.Price,
		Timestamp:  baseSignal.Timestamp,
		Conditions: baseSignal.Conditions,
	}

	// Long 대기 상태 처리
	if state.PendingSignal == PendingLong {
		// 히스토그램이 음수로 바뀌면 취소(추세 역전)
		if currentHistogram < 0 && state.PrevHistogram > 0 {
			log.Printf("[%s] Long 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			d.resetPendingState(state)
			return nil
		}

		// SAR가 캔들 아래로 이동하면 롱 시그널 생성
		if sarBelowCandle {
			resultSignal.Type = Long
			resultSignal.StopLoss = baseSignal.Conditions.SARValue
			resultSignal.TakeProfit = baseSignal.Price + (baseSignal.Price - resultSignal.StopLoss)

			log.Printf("[%s] Long 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			d.resetPendingState(state)
			return resultSignal
		}
	}

	// Short 대기 상태 처리
	if state.PendingSignal == PendingShort {
		// 히스토그램이 양수로 바뀌면 취소 (추세 역전)
		if currentHistogram > 0 && state.PrevHistogram < 0 {
			log.Printf("[%s] Short 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			d.resetPendingState(state)
			return nil
		}

		// SAR이 캔들 위로 이동하면 숏 시그널 생성
		if sarAboveCandle {
			resultSignal.Type = Short
			resultSignal.StopLoss = baseSignal.Conditions.SARValue
			resultSignal.TakeProfit = baseSignal.Price - (resultSignal.StopLoss - baseSignal.Price)

			log.Printf("[%s] Short 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			d.resetPendingState(state)
			return resultSignal
		}
	}

	return nil
}

// checkMACDCross는 MACD 크로스를 확인합니다
// 반환값: 1 (상향돌파), -1 (하향돌파), 0 (크로스 없음)
func (d *Detector) checkMACDCross(currentMACD, currentSignal, prevMACD, prevSignal float64) int {
	if prevMACD <= prevSignal && currentMACD > currentSignal {
		return 1 // 상향돌파
	}
	if prevMACD >= prevSignal && currentMACD < currentSignal {
		return -1 // 하향돌파
	}
	return 0 // 크로스 없음
}

// isDuplicateSignal은 중복 시그널인지 확인합니다
// func (d *Detector) isDuplicateSignal(signal *Signal) bool {
// 	if d.lastSignal == nil {
// 		return false
// 	}

// 	// 동일 방향의 시그널이 이미 존재하는 경우
// 	if d.lastSignal.Type == signal.Type {
// 		return true
// 	}

// 	return false
// }
