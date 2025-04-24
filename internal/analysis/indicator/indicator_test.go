package indicator

import (
	"fmt"
	"testing"
	"time"
)

// 테스트용 가격 데이터 생성
func generateTestPrices() []PriceData {
	// 2024년 1월 1일부터 시작하는 50일간의 가격 데이터
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := []PriceData{
		// 상승 구간 (변동성 있는)
		{Time: baseTime, Open: 100, High: 105, Low: 98, Close: 103, Volume: 1000},
		{Time: baseTime.AddDate(0, 0, 1), Open: 103, High: 108, Low: 102, Close: 106, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 2), Open: 106, High: 110, Low: 104, Close: 108, Volume: 1100},
		{Time: baseTime.AddDate(0, 0, 3), Open: 108, High: 112, Low: 107, Close: 110, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 4), Open: 110, High: 115, Low: 109, Close: 113, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 5), Open: 113, High: 116, Low: 111, Close: 114, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 6), Open: 114, High: 118, Low: 113, Close: 116, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 7), Open: 116, High: 119, Low: 115, Close: 117, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 8), Open: 117, High: 120, Low: 114, Close: 119, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 9), Open: 119, High: 122, Low: 118, Close: 120, Volume: 1600},

		// 하락 구간 (급격한 하락)
		{Time: baseTime.AddDate(0, 0, 10), Open: 120, High: 121, Low: 115, Close: 116, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 11), Open: 116, High: 117, Low: 112, Close: 113, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 12), Open: 113, High: 115, Low: 108, Close: 109, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 13), Open: 109, High: 110, Low: 105, Close: 106, Volume: 2400},
		{Time: baseTime.AddDate(0, 0, 14), Open: 106, High: 107, Low: 102, Close: 103, Volume: 2600},

		// 횡보 구간 (변동성 있는)
		{Time: baseTime.AddDate(0, 0, 15), Open: 103, High: 107, Low: 102, Close: 105, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 16), Open: 105, High: 108, Low: 103, Close: 104, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 17), Open: 104, High: 106, Low: 102, Close: 106, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 18), Open: 106, High: 108, Low: 104, Close: 105, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 19), Open: 105, High: 107, Low: 103, Close: 104, Volume: 1600},

		// 추가 하락 구간
		{Time: baseTime.AddDate(0, 0, 20), Open: 104, High: 105, Low: 100, Close: 101, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 21), Open: 101, High: 102, Low: 98, Close: 99, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 22), Open: 99, High: 100, Low: 96, Close: 97, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 23), Open: 97, High: 98, Low: 94, Close: 95, Volume: 2400},
		{Time: baseTime.AddDate(0, 0, 24), Open: 95, High: 96, Low: 92, Close: 93, Volume: 2600},
		{Time: baseTime.AddDate(0, 0, 25), Open: 93, High: 94, Low: 90, Close: 91, Volume: 2800},

		// 반등 구간
		{Time: baseTime.AddDate(0, 0, 26), Open: 91, High: 96, Low: 91, Close: 95, Volume: 3000},
		{Time: baseTime.AddDate(0, 0, 27), Open: 95, High: 98, Low: 94, Close: 97, Volume: 2800},
		{Time: baseTime.AddDate(0, 0, 28), Open: 97, High: 100, Low: 96, Close: 99, Volume: 2600},
		{Time: baseTime.AddDate(0, 0, 29), Open: 99, High: 102, Low: 98, Close: 101, Volume: 2400},

		// MACD 계산을 위한 추가 데이터
		{Time: baseTime.AddDate(0, 0, 30), Open: 101, High: 104, Low: 100, Close: 103, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 31), Open: 103, High: 106, Low: 102, Close: 105, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 32), Open: 105, High: 108, Low: 104, Close: 107, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 33), Open: 107, High: 110, Low: 106, Close: 109, Volume: 1600},
		{Time: baseTime.AddDate(0, 0, 34), Open: 109, High: 112, Low: 108, Close: 111, Volume: 1400},
	}
	return prices
}

func TestEMA(t *testing.T) {
	prices := generateTestPrices()

	testCases := []struct {
		period int
		name   string
	}{
		{9, "EMA(9)"},
		{12, "EMA(12)"},
		{26, "EMA(26)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := EMA(prices, EMAOption{Period: tc.period})
			if err != nil {
				t.Errorf("%s 계산 중 에러 발생: %v", tc.name, err)
				return
			}

			fmt.Printf("\n%s 결과:\n", tc.name)
			for i, result := range results {
				if i < tc.period-1 { // 초기값 건너뜀
					continue
				}
				fmt.Printf("날짜: %s, EMA: %.2f\n",
					result.Timestamp.Format("2006-01-02"), result.Value)

				// EMA 값 검증
				if result.Value <= 0 {
					t.Errorf("잘못된 EMA 값: %.2f", result.Value)
				}
			}
		})
	}
}

func TestMACD(t *testing.T) {
	prices := generateTestPrices()

	macdResults, err := MACD(prices, MACDOption{
		ShortPeriod:  12,
		LongPeriod:   26,
		SignalPeriod: 9,
	})
	if err != nil {
		t.Errorf("MACD 계산 중 에러 발생: %v", err)
		return
	}

	fmt.Println("\nMACD(12,26,9) 결과:")
	for _, result := range macdResults {
		fmt.Printf("날짜: %s, MACD: %.2f, Signal: %.2f, Histogram: %.2f\n",
			result.Timestamp.Format("2006-01-02"),
			result.MACD, result.Signal, result.Histogram)
	}
}

func TestSAR(t *testing.T) {
	prices := generateTestPrices()

	testCases := []struct {
		name string
		opt  SAROption
	}{
		{"기본설정", DefaultSAROption()},
		{"민감설정", SAROption{AccelerationInitial: 0.04, AccelerationMax: 0.4}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := SAR(prices, tc.opt)
			if err != nil {
				t.Errorf("SAR 계산 중 에러 발생: %v", err)
				return
			}

			fmt.Printf("\nParabolic SAR (%s) 결과:\n", tc.name)
			prevSAR := 0.0
			prevTrend := true
			for i, result := range results {
				fmt.Printf("날짜: %s, SAR: %.2f, 추세: %s\n",
					result.Timestamp.Format("2006-01-02"),
					result.SAR,
					map[bool]string{true: "상승", false: "하락"}[result.IsLong])

				// SAR 값 검증
				if i > 0 {
					// SAR 값이 이전 값과 동일한지 체크
					if result.SAR == prevSAR {
						t.Logf("경고: %s에 SAR 값이 변화없음: %.2f",
							result.Timestamp.Format("2006-01-02"), result.SAR)
					}
					// 추세 전환 확인
					if result.IsLong != prevTrend {
						fmt.Printf(">>> 추세 전환 발생: %s에서 %s로 전환\n",
							map[bool]string{true: "상승", false: "하락"}[prevTrend],
							map[bool]string{true: "상승", false: "하락"}[result.IsLong])
					}
				}
				prevSAR = result.SAR
				prevTrend = result.IsLong
			}
		})
	}
}

func TestAll(t *testing.T) {
	prices := generateTestPrices()

	t.Run("EMA Test", func(t *testing.T) { TestEMA(t) })
	t.Run("MACD Test", func(t *testing.T) { TestMACD(t) })
	t.Run("SAR Test", func(t *testing.T) { TestSAR(t) })

	fmt.Println("\n=== 테스트 데이터 요약 ===")
	fmt.Printf("데이터 기간: %s ~ %s\n",
		prices[0].Time.Format("2006-01-02"),
		prices[len(prices)-1].Time.Format("2006-01-02"))
	fmt.Printf("데이터 개수: %d\n", len(prices))
	fmt.Printf("시작가: %.2f, 종가: %.2f\n", prices[0].Close, prices[len(prices)-1].Close)

	// 데이터 특성 분석
	maxPrice := prices[0].High
	minPrice := prices[0].Low
	maxVolume := prices[0].Volume
	for _, p := range prices {
		if p.High > maxPrice {
			maxPrice = p.High
		}
		if p.Low < minPrice {
			minPrice = p.Low
		}
		if p.Volume > maxVolume {
			maxVolume = p.Volume
		}
	}
	fmt.Printf("가격 범위: %.2f ~ %.2f (변동폭: %.2f%%)\n",
		minPrice, maxPrice, (maxPrice-minPrice)/minPrice*100)
	fmt.Printf("최대 거래량: %.0f\n", maxVolume)
}
