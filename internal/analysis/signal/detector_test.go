package signal

import (
	"math"
	"testing"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// 테스트용 가격 데이터 생성 함수
func generateTestPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := []indicator.PriceData{
		// EMA200 위, MACD 상향돌파, SAR 하락 -> Long 시그널 조건
		{Time: baseTime, Open: 100, High: 105, Low: 98, Close: 103, Volume: 1000},
		{Time: baseTime.AddDate(0, 0, 1), Open: 103, High: 108, Low: 102, Close: 106, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 2), Open: 106, High: 110, Low: 104, Close: 108, Volume: 1100},

		// EMA200 아래, MACD 하향돌파, SAR 상승 -> Short 시그널 조건
		{Time: baseTime.AddDate(0, 0, 3), Open: 108, High: 109, Low: 95, Close: 96, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 4), Open: 96, High: 97, Low: 90, Close: 91, Volume: 1600},
		{Time: baseTime.AddDate(0, 0, 5), Open: 91, High: 92, Low: 85, Close: 86, Volume: 1700},

		// 횡보구간 -> 시그널 없음
		{Time: baseTime.AddDate(0, 0, 6), Open: 86, High: 88, Low: 84, Close: 87, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 7), Open: 87, High: 89, Low: 85, Close: 86, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 8), Open: 86, High: 88, Low: 84, Close: 85, Volume: 1100},
	}

	// 충분한 데이터 생성을 위해 반복
	fullData := make([]indicator.PriceData, 0, 250)
	for len(fullData) < 250 {
		for _, p := range prices {
			if len(fullData) < 250 {
				newPrice := p
				newPrice.Time = baseTime.AddDate(0, 0, len(fullData))
				fullData = append(fullData, newPrice)
			}
		}
	}

	return fullData
}

func TestDetector_Detect(t *testing.T) {
	prices := generateTestPrices()

	tests := []struct {
		name       string
		config     DetectorConfig
		wantSignal SignalType
		wantError  bool
	}{
		{
			name: "기본 설정으로 시그널 감지",
			config: DetectorConfig{
				EMALength:     200,
				StopLossPct:   0.02,
				TakeProfitPct: 0.04,
			},
			wantSignal: Long, // 첫 번째 시그널은 Long이 될 것으로 예상
			wantError:  false,
		},
		{
			name: "좁은 손익비로 시그널 감지",
			config: DetectorConfig{
				EMALength:     200,
				StopLossPct:   0.01,
				TakeProfitPct: 0.02,
			},
			wantSignal: Long,
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector(tt.config)
			signal, err := detector.Detect("BTCUSDT", prices)

			// 에러 체크
			if (err != nil) != tt.wantError {
				t.Errorf("Detect() error = %v, wantError %v", err, tt.wantError)
				return
			}

			// 시그널이 기대한 대로인지 체크
			if signal != nil && signal.Type != tt.wantSignal {
				t.Errorf("Detect() signal type = %v, want %v", signal.Type, tt.wantSignal)
			}

			// 손절/익절 가격이 올바르게 설정되었는지 체크
			if signal != nil {
				currentPrice := signal.Price
				switch signal.Type {
				case Long:
					expectedSL := currentPrice * (1 - tt.config.StopLossPct)
					expectedTP := currentPrice * (1 + tt.config.TakeProfitPct)

					if !almostEqual(signal.StopLoss, expectedSL) {
						t.Errorf("Long StopLoss = %.2f, want %.2f", signal.StopLoss, expectedSL)
					}
					if !almostEqual(signal.TakeProfit, expectedTP) {
						t.Errorf("Long TakeProfit = %.2f, want %.2f", signal.TakeProfit, expectedTP)
					}

				case Short:
					expectedSL := currentPrice * (1 + tt.config.StopLossPct)
					expectedTP := currentPrice * (1 - tt.config.TakeProfitPct)

					if !almostEqual(signal.StopLoss, expectedSL) {
						t.Errorf("Short StopLoss = %.2f, want %.2f", signal.StopLoss, expectedSL)
					}
					if !almostEqual(signal.TakeProfit, expectedTP) {
						t.Errorf("Short TakeProfit = %.2f, want %.2f", signal.TakeProfit, expectedTP)
					}
				}
			}

			// 조건값들이 설정되었는지 체크
			if signal != nil {
				if signal.Conditions.EMAValue == 0 {
					t.Error("EMAValue not set")
				}
				if signal.Conditions.MACDValue == 0 {
					t.Error("MACDValue not set")
				}
				if signal.Conditions.SARValue == 0 {
					t.Error("SARValue not set")
				}
			}
		})
	}
}

// TestDuplicateSignals는 중복 시그널 방지를 테스트합니다
func TestDuplicateSignals(t *testing.T) {
	prices := generateTestPrices()
	detector := NewDetector(DetectorConfig{
		EMALength:     200,
		StopLossPct:   0.02,
		TakeProfitPct: 0.04,
	})

	// 첫 번째 시그널 감지
	signal1, err := detector.Detect("BTCUSDT", prices)
	if err != nil {
		t.Fatalf("First Detect() failed: %v", err)
	}
	if signal1 == nil {
		t.Fatal("Expected first signal, got nil")
	}

	// 동일한 데이터로 두 번째 시그널 감지 시도
	signal2, err := detector.Detect("BTCUSDT", prices)
	if err != nil {
		t.Fatalf("Second Detect() failed: %v", err)
	}

	// 중복 시그널이므로 nil이어야 함
	if signal2 != nil {
		t.Errorf("Expected nil for duplicate signal, got %v", signal2.Type)
	}
}

// almostEqual은 부동소수점 비교를 위한 헬퍼 함수입니다
func almostEqual(a, b float64) bool {
	const epsilon = 0.0001
	return math.Abs(a-b) < epsilon
}
