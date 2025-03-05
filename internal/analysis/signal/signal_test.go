package signal

import (
	"math"
	"testing"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// 롱 시그널을 위한 테스트 데이터 생성
func generateLongSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250) // EMA 200 계산을 위해 충분한 데이터

	// 초기 하락 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - float64(i)*0.1,
			High:   startPrice - float64(i)*0.1 + 0.05,
			Low:    startPrice - float64(i)*0.1 - 0.05,
			Close:  startPrice - float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 상승 추세로 전환 (100-199)
	for i := 100; i < 200; i++ {
		increment := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + increment,
			High:   startPrice + increment + 0.05,
			Low:    startPrice + increment - 0.05,
			Close:  startPrice + increment,
			Volume: 1500.0,
		}
	}

	// 마지막 구간에서 강한 상승 추세 (200-249)
	// MACD 골든크로스와 SAR 하향 전환을 만들기 위한 데이터
	for i := 200; i < 250; i++ {
		increment := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 15.0 + increment,
			High:   startPrice + 15.0 + increment + 0.1,
			Low:    startPrice + 15.0 + increment - 0.05,
			Close:  startPrice + 15.0 + increment + 0.08,
			Volume: 2000.0,
		}
	}

	return prices
}

// 숏 시그널을 위한 테스트 데이터 생성
func generateShortSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// 초기 상승 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + float64(i)*0.1,
			High:   startPrice + float64(i)*0.1 + 0.05,
			Low:    startPrice + float64(i)*0.1 - 0.05,
			Close:  startPrice + float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 하락 추세로 전환 (100-199)
	for i := 100; i < 200; i++ {
		decrement := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - decrement,
			High:   startPrice - decrement + 0.05,
			Low:    startPrice - decrement - 0.05,
			Close:  startPrice - decrement,
			Volume: 1500.0,
		}
	}

	// 마지막 구간에서 강한 하락 추세 (200-249)
	for i := 200; i < 250; i++ {
		decrement := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 15.0 - decrement,
			High:   startPrice - 15.0 - decrement + 0.05,
			Low:    startPrice - 15.0 - decrement - 0.1,
			Close:  startPrice - 15.0 - decrement - 0.08,
			Volume: 2000.0,
		}
	}

	return prices
}

// 시그널이 발생하지 않는 데이터 생성
func generateNoSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// EMA200 주변에서 횡보하는 데이터 생성
	startPrice := 100.0
	for i := 0; i < 250; i++ {
		// sin 곡선을 사용하여 EMA200 주변에서 진동하는 가격 생성
		cycle := float64(i) * 0.1
		variation := math.Sin(cycle) * 0.5

		price := startPrice + variation
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   price - 0.1,
			High:   price + 0.2,
			Low:    price - 0.2,
			Close:  price,
			Volume: 1000.0 + math.Abs(variation)*100,
		}
	}

	return prices
}

func TestDetector_Detect(t *testing.T) {
	tests := []struct {
		name           string
		prices         []indicator.PriceData
		expectedSignal SignalType
		wantErr        bool
	}{
		{
			name:           "롱 시그널 테스트",
			prices:         generateLongSignalPrices(),
			expectedSignal: Long,
			wantErr:        false,
		},
		{
			name:           "숏 시그널 테스트",
			prices:         generateShortSignalPrices(),
			expectedSignal: Short,
			wantErr:        false,
		},
		{
			name:           "무시그널 테스트",
			prices:         generateNoSignalPrices(),
			expectedSignal: NoSignal,
			wantErr:        false,
		},
		{
			name:           "데이터 부족 테스트",
			prices:         generateLongSignalPrices()[:150], // 200개 미만의 데이터
			expectedSignal: NoSignal,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector(DetectorConfig{
				EMALength:     200,
				StopLossPct:   0.02,
				TakeProfitPct: 0.04,
			})

			signal, err := detector.Detect("BTCUSDT", tt.prices)

			// 에러 검증
			if (err != nil) != tt.wantErr {
				t.Errorf("Detect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// 시그널 타입 검증
			if signal.Type != tt.expectedSignal {
				t.Errorf("Expected %v signal, got %v", tt.expectedSignal, signal.Type)
			}

			// 시그널 타입별 추가 검증
			switch signal.Type {
			case Long:
				validateLongSignal(t, signal)
			case Short:
				validateShortSignal(t, signal)
			case NoSignal:
				validateNoSignal(t, signal)
			}
		})
	}
}

func validateLongSignal(t *testing.T, signal *Signal) {
	if !signal.Conditions.EMALong {
		t.Error("Expected price to be above EMA200")
	}
	if !signal.Conditions.MACDLong {
		t.Error("Expected MACD to cross above Signal line")
	}
	if !signal.Conditions.SARLong {
		t.Error("Expected SAR to be below price")
	}
	validateRiskRewardRatio(t, signal)
}

func validateShortSignal(t *testing.T, signal *Signal) {
	if !signal.Conditions.EMAShort {
		t.Error("Expected price to be below EMA200")
	}
	if !signal.Conditions.MACDShort {
		t.Error("Expected MACD to cross below Signal line")
	}
	if !signal.Conditions.SARShort {
		t.Error("Expected SAR to be above price")
	}
	validateRiskRewardRatio(t, signal)
}

func validateNoSignal(t *testing.T, signal *Signal) {
	if signal.StopLoss != 0 || signal.TakeProfit != 0 {
		t.Error("NoSignal should have zero StopLoss and TakeProfit")
	}
}

func validateRiskRewardRatio(t *testing.T, signal *Signal) {
	if signal.StopLoss == 0 || signal.TakeProfit == 0 {
		t.Error("StopLoss and TakeProfit should be non-zero")
		return
	}

	var riskAmount, rewardAmount float64
	if signal.Type == Long {
		riskAmount = signal.Price - signal.StopLoss
		rewardAmount = signal.TakeProfit - signal.Price
	} else {
		riskAmount = signal.StopLoss - signal.Price
		rewardAmount = signal.Price - signal.TakeProfit
	}

	if !almostEqual(riskAmount, rewardAmount, 0.0001) {
		t.Errorf("Risk:Reward ratio is not 1:1 (Risk: %.2f, Reward: %.2f)",
			riskAmount, rewardAmount)
	}
}

// 부동소수점 비교를 위한 헬퍼 함수
func almostEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// generatePendingLongSignalPrices는 Long 대기 상태를 확인하기 위한 테스트 데이터를 생성합니다
func generatePendingLongSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250) // EMA 200 계산을 위해 충분한 데이터

	// 초기 하락 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - float64(i)*0.1,
			High:   startPrice - float64(i)*0.1 + 0.05,
			Low:    startPrice - float64(i)*0.1 - 0.05,
			Close:  startPrice - float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 상승 추세로 전환 시작 (100-199)
	for i := 100; i < 200; i++ {
		increment := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + increment,
			High:   startPrice + increment + 0.05,
			Low:    startPrice + increment - 0.05,
			Close:  startPrice + increment,
			Volume: 1500.0,
		}
	}

	// 중간에 강한 상승 추세 (200-240) - MACD 골든 크로스 만들기
	for i := 200; i < 240; i++ {
		increment := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 15.0 + increment,
			High:   startPrice + 15.0 + increment + 0.1,
			Low:    startPrice + 15.0 + increment - 0.05,
			Close:  startPrice + 15.0 + increment + 0.08,
			Volume: 2000.0,
		}
	}

	// 마지막 부분 (240-244)에서 SAR은 아직 캔들 위에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		increment := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice + 27.0 + increment,
			// SAR이 계속 캔들 위에 유지되도록 고가를 낮게 설정
			High:   startPrice + 27.0 + increment + 0.03,
			Low:    startPrice + 27.0 + increment - 0.05,
			Close:  startPrice + 27.0 + increment + 0.02,
			Volume: 1800.0,
		}
	}

	// 마지막 캔들 (245-249)에서만 SAR이 캔들 아래로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		increment := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice + 28.0 + increment,
			// 마지막 캔들에서 확실하게 SAR이 아래로 가도록 고가를 확 높게 설정
			High:   startPrice + 28.0 + increment + 1.5,
			Low:    startPrice + 28.0 + increment - 0.1,
			Close:  startPrice + 28.0 + increment + 1.0,
			Volume: 2500.0,
		}
	}

	return prices
}

// generatePendingShortSignalPrices는 Short 대기 상태를 확인하기 위한 테스트 데이터를 생성합니다
func generatePendingShortSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// 초기 상승 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + float64(i)*0.1,
			High:   startPrice + float64(i)*0.1 + 0.05,
			Low:    startPrice + float64(i)*0.1 - 0.05,
			Close:  startPrice + float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 하락 추세로 전환 시작 (100-199)
	for i := 100; i < 200; i++ {
		decrement := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - decrement,
			High:   startPrice - decrement + 0.05,
			Low:    startPrice - decrement - 0.05,
			Close:  startPrice - decrement,
			Volume: 1500.0,
		}
	}

	// 강한 하락 추세 (200-240) - MACD 데드 크로스 만들기
	for i := 200; i < 240; i++ {
		decrement := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 15.0 - decrement,
			High:   startPrice - 15.0 - decrement + 0.05,
			Low:    startPrice - 15.0 - decrement - 0.1,
			Close:  startPrice - 15.0 - decrement - 0.08,
			Volume: 2000.0,
		}
	}

	// 마지막 부분 (240-244)에서 SAR은 아직 캔들 아래에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		decrement := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice - 27.0 - decrement,
			High: startPrice - 27.0 - decrement + 0.03,
			// SAR이 계속 캔들 아래에 유지되도록 저가를 높게 설정
			Low:    startPrice - 27.0 - decrement - 0.03,
			Close:  startPrice - 27.0 - decrement - 0.02,
			Volume: 1800.0,
		}
	}

	// 마지막 캔들 (245-249)에서만 SAR이 캔들 위로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		decrement := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice - 28.0 - decrement,
			High: startPrice - 28.0 - decrement + 0.1,
			// 마지막 캔들에서 확실하게 SAR이 위로 가도록 저가를 확 낮게 설정
			Low:    startPrice - 28.0 - decrement - 1.5,
			Close:  startPrice - 28.0 - decrement - 1.0,
			Volume: 2500.0,
		}
	}

	return prices
}

// TestPendingSignals는 대기 상태 및 신호 감지를 테스트합니다
func TestPendingSignals(t *testing.T) {
	detector := NewDetector(DetectorConfig{
		EMALength:      200,
		StopLossPct:    0.02,
		TakeProfitPct:  0.04,
		MaxWaitCandles: 5,
		MinHistogram:   0.00005,
	})

	t.Run("롱/숏 신호 감지 테스트", func(t *testing.T) {
		// 롱 신호 테스트
		longPrices := generateLongSignalPrices()
		longSignal, err := detector.Detect("BTCUSDT", longPrices)
		if err != nil {
			t.Fatalf("롱 신호 감지 에러: %v", err)
		}

		if longSignal.Type != Long {
			t.Errorf("롱 신호 감지 실패: 예상 타입 Long, 실제 %s", longSignal.Type)
		} else {
			t.Logf("롱 신호 감지 성공: 가격=%.2f, 손절=%.2f, 익절=%.2f",
				longSignal.Price, longSignal.StopLoss, longSignal.TakeProfit)
		}

		// 숏 신호 테스트
		shortPrices := generateShortSignalPrices()
		shortSignal, err := detector.Detect("BTCUSDT", shortPrices)
		if err != nil {
			t.Fatalf("숏 신호 감지 에러: %v", err)
		}

		if shortSignal.Type != Short {
			t.Errorf("숏 신호 감지 실패: 예상 타입 Short, 실제 %s", shortSignal.Type)
		} else {
			t.Logf("숏 신호 감지 성공: 가격=%.2f, 손절=%.2f, 익절=%.2f",
				shortSignal.Price, shortSignal.StopLoss, shortSignal.TakeProfit)
		}
	})

	t.Run("대기 상태 단위 테스트", func(t *testing.T) {
		// 롱 대기 상태 테스트
		symbolState := &SymbolState{
			PendingSignal:  PendingLong,
			WaitedCandles:  2,
			MaxWaitCandles: 5,
			PrevMACD:       0.001,
			PrevSignal:     0.0005,
			PrevHistogram:  0.0005,
		}

		// SAR이 캔들 아래로 반전된 상황 시뮬레이션
		baseSignal := &Signal{
			Type:      NoSignal,
			Symbol:    "BTCUSDT",
			Price:     100.0,
			Timestamp: time.Now(),
			Conditions: SignalConditions{
				SARValue: 98.0, // SAR이 캔들 아래로 이동
			},
		}

		// 롱 대기 상태에서 SAR 반전 시 롱 신호가 생성되는지 확인
		result := detector.processPendingState(symbolState, "BTCUSDT", baseSignal, 0.001, true, false)
		if result == nil {
			t.Errorf("롱 대기 상태에서 SAR 반전 시 신호가 생성되지 않음")
		} else if result.Type != Long {
			t.Errorf("롱 대기 상태에서 SAR 반전 시 잘못된 신호 타입: %s", result.Type)
		} else {
			t.Logf("롱 대기 상태 테스트 성공")
		}

		// 숏 대기 상태 테스트
		symbolState = &SymbolState{
			PendingSignal:  PendingShort,
			WaitedCandles:  2,
			MaxWaitCandles: 5,
			PrevMACD:       -0.001,
			PrevSignal:     -0.0005,
			PrevHistogram:  -0.0005,
		}

		// SAR이 캔들 위로 반전된 상황 시뮬레이션
		baseSignal = &Signal{
			Type:      NoSignal,
			Symbol:    "BTCUSDT",
			Price:     100.0,
			Timestamp: time.Now(),
			Conditions: SignalConditions{
				SARValue: 102.0, // SAR이 캔들 위로 이동
			},
		}

		// 숏 대기 상태에서 SAR 반전 시 숏 신호가 생성되는지 확인
		result = detector.processPendingState(symbolState, "BTCUSDT", baseSignal, -0.001, false, true)
		if result == nil {
			t.Errorf("숏 대기 상태에서 SAR 반전 시 신호가 생성되지 않음")
		} else if result.Type != Short {
			t.Errorf("숏 대기 상태에서 SAR 반전 시 잘못된 신호 타입: %s", result.Type)
		} else {
			t.Logf("숏 대기 상태 테스트 성공")
		}
	})
}
