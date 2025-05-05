package doublersi

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/indicator"
	"github.com/assist-by/phoenix/internal/strategy"
)

// 심볼별 상태를 관리하기 위한 구조체
type SymbolState struct {
	PrevHourlyRSI float64                // 이전 시간봉 RSI 값
	LastSignal    domain.SignalInterface // 마지막 발생 시그널
	PrevCandles   domain.CandleList      // 최근 몇 개 캔들 저장 (고점/저점 계산용)
}

// DoubleRSIStrategy는 더블 RSI 전략을 구현합니다
type DoubleRSIStrategy struct {
	strategy.BaseStrategy

	// RSI 지표 인스턴스
	dailyRSI  *indicator.RSI // 일봉용 RSI
	hourlyRSI *indicator.RSI // 시간봉용 RSI

	// 설정값
	dailyRSIPeriod     int     // dailyRSI 기간 (일봉)
	hourlyRSIPeriod    int     // hourlyRSI 기간 (시간봉)
	dailyRSIUpperBand  float64 // dailyRSI 상단 밴드 (기본 60)
	dailyRSILowerBand  float64 // dailyRSI 하단 밴드 (기본 40)
	hourlyRSIUpperBand float64 // hourlyRSI 상단 밴드 (기본 60)
	hourlyRSILowerBand float64 // hourlyRSI 하단 밴드 (기본 40)
	tpRatio            float64 // 손익비 (TP/SL 비율)
	lookbackPeriod     int     // 고점/저점 탐색 기간

	states map[string]*SymbolState
	mu     sync.RWMutex
}

// NewStrategy는 새로운 더블 RSI 전략 인스턴스를 생성합니다
func NewStrategy(config map[string]interface{}) (strategy.Strategy, error) {
	// 기본 설정값
	dailyRSIPeriod := 7  // 일봉 RSI 기간
	hourlyRSIPeriod := 7 // 시간봉 RSI 기간
	dailyRSIUpperBand := 60.0
	dailyRSILowerBand := 40.0
	hourlyRSIUpperBand := 60.0
	hourlyRSILowerBand := 40.0
	tpRatio := 1.5      // 익절:손절 = 1.5:1
	lookbackPeriod := 5 // 직전 5개 봉에서 고점/저점 찾기

	// 설정에서 값 로드
	if config != nil {
		if val, ok := config["dailyRSIPeriod"].(int); ok {
			dailyRSIPeriod = val
		}
		if val, ok := config["hourlyRSIPeriod"].(int); ok {
			hourlyRSIPeriod = val
		}
		if val, ok := config["dailyRSIUpperBand"].(float64); ok {
			dailyRSIUpperBand = val
		}
		if val, ok := config["dailyRSILowerBand"].(float64); ok {
			dailyRSILowerBand = val
		}
		if val, ok := config["hourlyRSIUpperBand"].(float64); ok {
			hourlyRSIUpperBand = val
		}
		if val, ok := config["hourlyRSILowerBand"].(float64); ok {
			hourlyRSILowerBand = val
		}
		if val, ok := config["tpRatio"].(float64); ok {
			tpRatio = val
		}
		if val, ok := config["lookbackPeriod"].(int); ok {
			lookbackPeriod = val
		}
	}

	// RSI 지표 인스턴스 생성
	dailyRSIIndicator := indicator.NewRSI(dailyRSIPeriod, false)
	hourlyRSIIndicator := indicator.NewRSI(hourlyRSIPeriod, false)

	s := &DoubleRSIStrategy{
		BaseStrategy: strategy.BaseStrategy{
			Name:        "DoubleRSI",
			Description: "일봉 RSI와 시간봉 RSI를 조합한 트렌드 추종 전략",
			Config:      config,
		},
		dailyRSI:           dailyRSIIndicator,
		hourlyRSI:          hourlyRSIIndicator,
		dailyRSIPeriod:     dailyRSIPeriod,
		hourlyRSIPeriod:    hourlyRSIPeriod,
		dailyRSIUpperBand:  dailyRSIUpperBand,
		dailyRSILowerBand:  dailyRSILowerBand,
		hourlyRSIUpperBand: hourlyRSIUpperBand,
		hourlyRSILowerBand: hourlyRSILowerBand,
		tpRatio:            tpRatio,
		lookbackPeriod:     lookbackPeriod,
		states:             make(map[string]*SymbolState),
	}

	return s, nil
}

// RegisterStrategy는 이 전략을 레지스트리에 등록합니다
func RegisterStrategy(registry *strategy.Registry) {
	registry.Register("DoubleRSI", NewStrategy)
}

// Initialize는 전략을 초기화합니다
func (s *DoubleRSIStrategy) Initialize(ctx context.Context) error {
	// 전략 초기화 내용 로깅
	log.Printf("전략 초기화: %s", s.GetName())
	log.Printf("설정 - 일봉 RSI 기간: %d, 시간봉 RSI 기간: %d", s.dailyRSIPeriod, s.hourlyRSIPeriod)
	log.Printf("설정 - 일봉 RSI 밴드: %.1f/%.1f, 시간봉 RSI 밴드: %.1f/%.1f",
		s.dailyRSIUpperBand, s.dailyRSILowerBand, s.hourlyRSIUpperBand, s.hourlyRSILowerBand)
	log.Printf("설정 - TP/SL 비율: %.1f, 고점/저점 탐색 기간: %d", s.tpRatio, s.lookbackPeriod)

	// 심볼 상태 맵 초기화
	s.states = make(map[string]*SymbolState)

	return nil
}

// Analyze는 주어진 캔들 데이터를 분석하여 매매 신호를 생성합니다
func (s *DoubleRSIStrategy) Analyze(ctx context.Context, symbol string, candles domain.CandleList) (domain.SignalInterface, error) {
	// 데이터 검증
	if len(candles) < 24+s.dailyRSIPeriod {
		return nil, fmt.Errorf("부족한 데이터: 최소 %d개의 캔들이 필요합니다", 24+s.dailyRSIPeriod)
	}

	// 심볼별 상태 가져오기
	state := s.getSymbolState(symbol)

	// 1. 현재 진행 중인 일봉 생성
	lastCandle := candles[len(candles)-1]
	dailyCandle, err := domain.ConvertHourlyToCurrentDaily(candles, lastCandle.OpenTime)
	if err != nil {
		return nil, fmt.Errorf("일봉 데이터 생성 실패: %w", err)
	}

	// 2. 일봉 데이터 준비 (최소 dailyRSIPeriod+1개 필요)
	// 현재 시간까지의 일봉 데이터를 수집
	var dailyCandles domain.CandleList
	dailyCandles = append(dailyCandles, *dailyCandle) // 현재 진행 중인 일봉 추가

	// 과거 일봉 데이터 수집
	currentTime := lastCandle.OpenTime
	startDate := time.Date(
		currentTime.Year(), currentTime.Month(), currentTime.Day(),
		0, 0, 0, 0, currentTime.Location(),
	)

	// 1일씩 이전으로 이동하며 일봉 생성
	for i := 1; i <= s.dailyRSIPeriod; i++ {
		prevDate := startDate.AddDate(0, 0, -i)

		// 해당 날짜의 캔들만 추출
		dayCandles := []domain.Candle{}
		for _, c := range candles {
			candleDate := time.Date(
				c.OpenTime.Year(), c.OpenTime.Month(), c.OpenTime.Day(),
				0, 0, 0, 0, c.OpenTime.Location(),
			)
			if candleDate.Equal(prevDate) {
				dayCandles = append(dayCandles, c)
			}
		}

		// 해당 날짜의 캔들이 있으면 일봉으로 변환 추가
		if len(dayCandles) > 0 {
			// 정렬 (시간순)
			sort.Slice(dayCandles, func(i, j int) bool {
				return dayCandles[i].OpenTime.Before(dayCandles[j].OpenTime)
			})

			// 마지막 캔들 시간 기준으로 일봉 생성
			completedCandle, err := domain.ConvertHourlyToCurrentDaily(dayCandles, dayCandles[len(dayCandles)-1].OpenTime)
			if err == nil && completedCandle != nil {
				dailyCandles = append(dailyCandles, *completedCandle)
			}
		}
	}

	// 날짜 역순으로 추가된 캔들을 날짜순으로 정렬
	sort.Slice(dailyCandles, func(i, j int) bool {
		return dailyCandles[i].OpenTime.Before(dailyCandles[j].OpenTime)
	})

	// 3. 지표 계산용 데이터 형식으로 변환
	hourlyPrices := indicator.ConvertCandlesToPriceData(candles)
	dailyPrices := indicator.ConvertCandlesToPriceData(dailyCandles)

	// 4. RSI 계산
	dailyRSIResults, err := s.dailyRSI.Calculate(dailyPrices)
	if err != nil {
		return nil, fmt.Errorf("dailyRSI 계산 실패: %w", err)
	}

	hourlyRSIResults, err := s.hourlyRSI.Calculate(hourlyPrices)
	if err != nil {
		return nil, fmt.Errorf("hourlyRSI 계산 실패: %w", err)
	}

	// 5. 필요한 지표값 추출
	// 현재 및 이전 RSI 값
	var currentDailyRSI float64
	if len(dailyRSIResults) > 0 {
		currentDailyRSI = dailyRSIResults[len(dailyRSIResults)-1].(indicator.RSIResult).Value
	} else {
		return nil, fmt.Errorf("dailyRSI 결과가 없습니다")
	}

	var currentHourlyRSI, prevHourlyRSI float64
	if len(hourlyRSIResults) > 1 {
		currentHourlyRSI = hourlyRSIResults[len(hourlyRSIResults)-1].(indicator.RSIResult).Value
		prevHourlyRSI = hourlyRSIResults[len(hourlyRSIResults)-2].(indicator.RSIResult).Value
	} else {
		currentHourlyRSI = hourlyRSIResults[len(hourlyRSIResults)-1].(indicator.RSIResult).Value
		prevHourlyRSI = state.PrevHourlyRSI // 저장된 이전 값 사용
	}

	currentPrice := lastCandle.Close

	// 6. 시그널 조건 확인
	signalType := domain.NoSignal
	var stopLoss, takeProfit float64

	// 시장 상태 로깅
	log.Printf("[%s] - 현재 상태: 가격=%.2f, 일봉 RSI=%.2f, 시간봉 RSI=%.2f (이전: %.2f)",
		symbol, currentPrice, currentDailyRSI, currentHourlyRSI, prevHourlyRSI)

	// 6.1 롱 시그널 조건
	longCondition := (currentDailyRSI > s.dailyRSIUpperBand) && // 일봉 RSI > 60
		(prevHourlyRSI < s.hourlyRSILowerBand) && // 이전 시간봉 RSI < 40
		(currentHourlyRSI >= s.hourlyRSILowerBand) // 현재 시간봉 RSI >= 40 (상향돌파)

	// 6.2 숏 시그널 조건
	shortCondition := (currentDailyRSI < s.dailyRSILowerBand) && // 일봉 RSI < 40
		(prevHourlyRSI > s.hourlyRSIUpperBand) && // 이전 시간봉 RSI > 60
		(currentHourlyRSI <= s.hourlyRSIUpperBand) // 현재 시간봉 RSI <= 60 (하향돌파)

	// 6.3 롱 시그널
	if longCondition {
		signalType = domain.Long

		// 직전 저점 찾기 (최대 lookbackPeriod개 캔들 확인)
		low := math.MaxFloat64
		startIdx := max(0, len(candles)-s.lookbackPeriod-1)
		for i := startIdx; i < len(candles)-1; i++ {
			if candles[i].Low < low {
				low = candles[i].Low
			}
		}

		// StopLoss는 직전 저점
		stopLoss = low

		// TakeProfit은 (진입가 - StopLoss) * tpRatio
		risk := currentPrice - stopLoss
		takeProfit = currentPrice + (risk * s.tpRatio)

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, 일봉 RSI=%.2f, 시간봉 RSI=%.2f → %.2f (시간: %s)",
			symbol, currentPrice, currentDailyRSI, prevHourlyRSI, currentHourlyRSI,
			lastCandle.OpenTime.Format("2006-01-02 15:04:05"))
	}

	// 6.4 숏 시그널
	if shortCondition {
		signalType = domain.Short

		// 직전 고점 찾기 (최대 lookbackPeriod개 캔들 확인)
		high := 0.0
		startIdx := max(0, len(candles)-s.lookbackPeriod-1)
		for i := startIdx; i < len(candles)-1; i++ {
			if candles[i].High > high {
				high = candles[i].High
			}
		}

		// StopLoss는 직전 고점
		stopLoss = high

		// TakeProfit은 (StopLoss - 진입가) * tpRatio
		risk := stopLoss - currentPrice
		takeProfit = currentPrice - (risk * s.tpRatio)

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, 일봉 RSI=%.2f, 시간봉 RSI=%.2f → %.2f (시간: %s)",
			symbol, currentPrice, currentDailyRSI, prevHourlyRSI, currentHourlyRSI,
			lastCandle.OpenTime.Format("2006-01-02 15:04:05"))
	}

	// 7. 시그널 생성
	signal := &DoubleRSISignal{
		BaseSignal: domain.NewBaseSignal(
			signalType,
			symbol,
			currentPrice,
			lastCandle.OpenTime,
			stopLoss,
			takeProfit,
		),
		DailyRSIValue:  currentDailyRSI,
		HourlyRSIValue: currentHourlyRSI,
		HourlyRSIPrev:  prevHourlyRSI,
		CrossUp:        prevHourlyRSI < s.hourlyRSILowerBand && currentHourlyRSI >= s.hourlyRSILowerBand,
		CrossDown:      prevHourlyRSI > s.hourlyRSIUpperBand && currentHourlyRSI <= s.hourlyRSIUpperBand,
		PrevLow:        lastCandle.Low,
		PrevHigh:       lastCandle.High,
	}

	// 8. 상태 업데이트
	state.PrevHourlyRSI = currentHourlyRSI
	if signalType != domain.NoSignal {
		state.LastSignal = signal

		// 직전 캔들 몇 개 저장 (다음 분석 시 활용)
		startIndex := max(0, len(candles)-s.lookbackPeriod)
		state.PrevCandles = candles[startIndex:]
	}

	return signal, nil

}

// getSymbolState는 심볼별 상태를 가져옵니다
func (s *DoubleRSIStrategy) getSymbolState(symbol string) *SymbolState {
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
