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

	// 마지막 부분 (241-249)에서 SAR은 아직 캔들 위에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		increment := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 27.0 + increment,
			High:   startPrice + 27.0 + increment + 0.05,
			Low:    startPrice + 27.0 + increment - 0.05,
			Close:  startPrice + 27.0 + increment + 0.04,
			Volume: 1800.0,
		}
	}

	// 마지막 부분 (245-249)에서 SAR이 캔들 아래로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		increment := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 28.0 + increment,
			High:   startPrice + 28.0 + increment + 0.5,
			Low:    startPrice + 28.0 + increment - 0.1,
			Close:  startPrice + 28.0 + increment + 0.4,
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

	// 마지막 부분 (241-245)에서 SAR은 아직 캔들 아래에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		decrement := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 27.0 - decrement,
			High:   startPrice - 27.0 - decrement + 0.05,
			Low:    startPrice - 27.0 - decrement - 0.05,
			Close:  startPrice - 27.0 - decrement - 0.04,
			Volume: 1800.0,
		}
	}

	// 마지막 부분 (245-249)에서 SAR이 캔들 위로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		decrement := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 28.0 - decrement,
			High:   startPrice - 28.0 - decrement + 0.1,
			Low:    startPrice - 28.0 - decrement - 0.5,
			Close:  startPrice - 28.0 - decrement - 0.4,
			Volume: 2500.0,
		}
	}

	return prices
}

// TestPendingSignals는 대기 상태에서 시그널 감지가 제대로 동작하는지 테스트합니다
func TestPendingSignals(t *testing.T) {
	detector := NewDetector(DetectorConfig{
		EMALength:      200,
		StopLossPct:    0.02,
		TakeProfitPct:  0.04,
		MaxWaitCandles: 5,
		MinHistogram:   0.00005,
	})

	t.Run("롱 대기 상태 테스트", func(t *testing.T) {
		prices := generatePendingLongSignalPrices()

		// 테스트는 마지막 5개 캔들만 사용
		startIdx := len(prices) - 5

		// 첫 번째 캔들 (초기 상태)
		signal, err := detector.Detect("BTCUSDT", prices[:startIdx+1])
		if err != nil {
			t.Fatalf("시그널 감지 에러: %v", err)
		}
		if signal.Type != NoSignal {
			t.Errorf("첫 캔들에서 예상 시그널 NoSignal, 실제 %s", signal.Type)
		}

		// 두 번째 캔들 (MACD 골든크로스 발생, PendingLong 상태 진입)
		signal, err = detector.Detect("BTCUSDT", prices[:startIdx+2])
		if err != nil {
			t.Fatalf("시그널 감지 에러: %v", err)
		}

		// PendingLong 상태가 직접 리턴되지는 않지만, 내부적으로 상태가 유지됨
		// 따라서 계속 NoSignal이 리턴됨
		if signal.Type != NoSignal {
			t.Errorf("두 번째 캔들에서 예상 시그널 NoSignal, 실제 %s", signal.Type)
		}

		// 세 번째, 네 번째 캔들 (SAR 반전 기다리는 중)
		for i := 3; i <= 4; i++ {
			signal, err = detector.Detect("BTCUSDT", prices[:startIdx+i])
			if err != nil {
				t.Fatalf("시그널 감지 에러: %v", err)
			}
			if signal.Type != NoSignal {
				t.Errorf("%d번째 캔들에서 예상 시그널 NoSignal, 실제 %s", i, signal.Type)
			}
		}

		// 다섯 번째 캔들 (SAR 반전 발생, Long 시그널 생성)
		signal, err = detector.Detect("BTCUSDT", prices)
		if err != nil {
			t.Fatalf("시그널 감지 에러: %v", err)
		}
		if signal.Type != Long {
			t.Errorf("다섯 번째 캔들에서 예상 시그널 Long, 실제 %s", signal.Type)
		}

		// 스탑로스가 제대로 설정되었는지 확인
		if signal.Type == Long && signal.StopLoss <= 0 {
			t.Errorf("Long 시그널의 스탑로스가 올바르게 설정되지 않음: %f", signal.StopLoss)
		}
	})

	t.Run("숏 대기 상태 테스트", func(t *testing.T) {
		prices := generatePendingShortSignalPrices()

		// 테스트는 마지막 5개 캔들만 사용
		startIdx := len(prices) - 5

		// 첫 번째 캔들 (초기 상태)
		signal, err := detector.Detect("BTCUSDT", prices[:startIdx+1])
		if err != nil {
			t.Fatalf("시그널 감지 에러: %v", err)
		}
		if signal.Type != NoSignal {
			t.Errorf("첫 캔들에서 예상 시그널 NoSignal, 실제 %s", signal.Type)
		}

		// 두 번째 캔들 (MACD 데드크로스 발생, PendingShort 상태 진입)
		signal, err = detector.Detect("BTCUSDT", prices[:startIdx+2])
		if err != nil {
			t.Fatalf("시그널 감지 에러: %v", err)
		}

		// PendingShort 상태가 직접 리턴되지는 않지만, 내부적으로 상태가 유지됨
		if signal.Type != NoSignal {
			t.Errorf("두 번째 캔들에서 예상 시그널 NoSignal, 실제 %s", signal.Type)
		}

		// 세 번째, 네 번째 캔들 (SAR 반전 기다리는 중)
		for i := 3; i <= 4; i++ {
			signal, err = detector.Detect("BTCUSDT", prices[:startIdx+i])
			if err != nil {
				t.Fatalf("시그널 감지 에러: %v", err)
			}
			if signal.Type != NoSignal {
				t.Errorf("%d번째 캔들에서 예상 시그널 NoSignal, 실제 %s", i, signal.Type)
			}
		}

		// 다섯 번째 캔들 (SAR 반전 발생, Short 시그널 생성)
		signal, err = detector.Detect("BTCUSDT", prices)
		if err != nil {
			t.Fatalf("시그널 감지 에러: %v", err)
		}
		if signal.Type != Short {
			t.Errorf("다섯 번째 캔들에서 예상 시그널 Short, 실제 %s", signal.Type)
		}

		// 스탑로스가 제대로 설정되었는지 확인
		if signal.Type == Short && signal.StopLoss <= 0 {
			t.Errorf("Short 시그널의 스탑로스가 올바르게 설정되지 않음: %f", signal.StopLoss)
		}
	})
}
