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
