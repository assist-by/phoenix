package signal

import (
	"fmt"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// NewDetector는 새로운 시그널 감지기를 생성합니다
func NewDetector(config DetectorConfig) *Detector {
	return &Detector{
		states:        make(map[string]*SymbolState),
		emaLength:     config.EMALength,
		stopLossPct:   config.StopLossPct,
		takeProfitPct: config.TakeProfitPct,
	}
}

// DetectorConfig는 시그널 감지기 설정을 정의합니다
type DetectorConfig struct {
	EMALength     int     // EMA 기간 (기본값: 200)
	StopLossPct   float64 // 손절 비율 (기본값: 0.02 -> 2%)
	TakeProfitPct float64 // 익절 비율 (기본값: 0.04 -> 4%)
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

	// MACD 크로스 확인 - 이제 심볼별 상태 사용
	macdCross := d.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal

	signal := &Signal{
		Type:      NoSignal,
		Symbol:    symbol,
		Price:     currentPrice,
		Timestamp: prices[len(prices)-1].Time,
	}

	// Long 시그널 조건 수정
	if currentPrice > ema[len(ema)-1].Value && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		sar[len(sar)-1].SAR < prices[len(prices)-1].Low { // SAR이 현재 봉의 저가보다 낮음

		signal.Type = Long
		signal.StopLoss = sar[len(sar)-1].SAR                               // SAR 기반 손절가
		signal.TakeProfit = currentPrice + (currentPrice - signal.StopLoss) // 1:1 비율
	}

	// Short 시그널 조건 수정
	if currentPrice < ema[len(ema)-1].Value && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		sar[len(sar)-1].SAR > prices[len(prices)-1].High { // SAR이 현재 봉의 고가보다 높음

		signal.Type = Short
		signal.StopLoss = sar[len(sar)-1].SAR                               // SAR 기반 손절가
		signal.TakeProfit = currentPrice - (signal.StopLoss - currentPrice) // 1:1 비율
	}

	// 시그널 조건 저장
	signal.Conditions = SignalConditions{
		EMA:         currentPrice > ema[len(ema)-1].Value,
		MACD:        macdCross != 0,
		SAR:         !sar[len(sar)-1].IsLong,
		EMAValue:    ema[len(ema)-1].Value,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		SARValue:    sar[len(sar)-1].SAR,
	}

	state.LastSignal = signal
	return signal, nil
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
