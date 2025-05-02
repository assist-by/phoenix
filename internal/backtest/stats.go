package backtest

import (
	"math"
	"time"
)

// CalculateStats는 백테스트 결과에 대한 고급 통계를 계산합니다
func CalculateStats(trades []Trade, account *Account) *Result {
	// 기본 결과 생성
	result := &Result{
		TotalTrades:   len(trades),
		WinningTrades: 0,
		LosingTrades:  0,
		StartTime:     account.EquityHistory[0].Timestamp,
		EndTime:       account.EquityHistory[len(account.EquityHistory)-1].Timestamp,
		Trades:        trades,
	}

	// 트레이드가 없는 경우 빈 결과 반환
	if len(trades) == 0 {
		return result
	}

	// 시간대별 성과 맵 초기화
	result.TimeBasedPerformance = make(map[string]TimePerformance)
	result.DayOfWeekPerformance = make(map[string]TimePerformance)
	result.MonthlyReturns = make(map[string]float64)

	// 통계 계산에 필요한 변수들
	totalProfit := 0.0
	totalLoss := 0.0
	totalWinAmount := 0.0
	totalLossAmount := 0.0
	totalHoldingTime := time.Duration(0)

	// 연속 승/패 계산 변수
	currentConsecutiveWins := 0
	currentConsecutiveLosses := 0
	maxConsecutiveWins := 0
	maxConsecutiveLosses := 0

	// 트레이드 루프
	for _, trade := range trades {
		// 승/패 계산
		if trade.ProfitPct > 0 {
			result.WinningTrades++
			totalProfit += trade.ProfitPct
			totalWinAmount += trade.ProfitPct

			// 연속 승 카운트
			currentConsecutiveWins++
			currentConsecutiveLosses = 0
			if currentConsecutiveWins > maxConsecutiveWins {
				maxConsecutiveWins = currentConsecutiveWins
			}
		} else if trade.ProfitPct < 0 {
			result.LosingTrades++
			totalLoss += math.Abs(trade.ProfitPct)
			totalLossAmount += math.Abs(trade.ProfitPct)

			// 연속 패 카운트
			currentConsecutiveLosses++
			currentConsecutiveWins = 0
			if currentConsecutiveLosses > maxConsecutiveLosses {
				maxConsecutiveLosses = currentConsecutiveLosses
			}
		}

		// 거래 지속 시간 계산
		holdingPeriod := trade.ExitTime.Sub(trade.EntryTime)
		totalHoldingTime += holdingPeriod

		// 시간대별 통계 업데이트
		updateTimeBasedStats(result, trade)

		// 요일별 통계 업데이트
		updateDayOfWeekStats(result, trade)

		// 월별 통계 업데이트
		updateMonthlyStats(result, trade)
	}

	// 승률 계산
	if len(trades) > 0 {
		result.WinRate = float64(result.WinningTrades) / float64(len(trades)) * 100
	}

	// 평균 수익률 계산
	result.CumulativeReturn = (account.Balance - account.InitialBalance) / account.InitialBalance * 100
	result.AverageReturn = (totalProfit - totalLoss) / float64(len(trades))

	// 프로핏 팩터 계산
	if totalLoss > 0 {
		result.ProfitFactor = totalProfit / totalLoss
	} else {
		result.ProfitFactor = totalProfit // 손실이 없는 경우
	}

	// 평균 승/패 금액 계산
	if result.WinningTrades > 0 {
		result.AvgWinAmount = totalWinAmount / float64(result.WinningTrades)
	}
	if result.LosingTrades > 0 {
		result.AvgLossAmount = totalLossAmount / float64(result.LosingTrades)
	}

	// 평균 거래 지속 시간
	if len(trades) > 0 {
		result.AvgTradeTime = totalHoldingTime / time.Duration(len(trades))
	}

	// 최대 연속 승/패 기록
	result.MaxConsecutiveWins = maxConsecutiveWins
	result.MaxConsecutiveLosses = maxConsecutiveLosses

	// 자산 곡선 복사
	result.EquityCurve = account.EquityHistory

	// 최대 낙폭 기록
	result.MaxDrawdown = account.MaxDrawdown

	return result
}

// updateTimeBasedStats는 시간대별 성과 통계를 업데이트합니다
func updateTimeBasedStats(result *Result, trade Trade) {
	// 시간대 결정 (0-5: 새벽, 6-11: 오전, 12-17: 오후, 18-23: 저녁)
	var timeOfDay string
	hour := trade.EntryTime.Hour()

	switch {
	case hour >= 0 && hour < 6:
		timeOfDay = "새벽 (0-5시)"
	case hour >= 6 && hour < 12:
		timeOfDay = "오전 (6-11시)"
	case hour >= 12 && hour < 18:
		timeOfDay = "오후 (12-17시)"
	default:
		timeOfDay = "저녁 (18-23시)"
	}

	// 해당 시간대 통계 가져오기 또는 초기화
	stats, exists := result.TimeBasedPerformance[timeOfDay]
	if !exists {
		stats = TimePerformance{}
	}

	// 통계 업데이트
	stats.TotalTrades++
	if trade.ProfitPct > 0 {
		stats.WinningTrades++
		stats.CumulativeReturn += trade.ProfitPct
	} else if trade.ProfitPct < 0 {
		stats.LosingTrades++
		stats.CumulativeReturn += trade.ProfitPct
	}

	// 승률 및 평균 수익률 계산
	stats.WinRate = float64(stats.WinningTrades) / float64(stats.TotalTrades) * 100
	stats.AverageReturn = stats.CumulativeReturn / float64(stats.TotalTrades)

	// 업데이트된 통계 저장
	result.TimeBasedPerformance[timeOfDay] = stats
}

// updateDayOfWeekStats는 요일별 성과 통계를 업데이트합니다
func updateDayOfWeekStats(result *Result, trade Trade) {
	// 요일 결정
	days := []string{"일요일", "월요일", "화요일", "수요일", "목요일", "금요일", "토요일"}
	dayOfWeek := days[trade.EntryTime.Weekday()]

	// 해당 요일 통계 가져오기 또는 초기화
	stats, exists := result.DayOfWeekPerformance[dayOfWeek]
	if !exists {
		stats = TimePerformance{}
	}

	// 통계 업데이트
	stats.TotalTrades++
	if trade.ProfitPct > 0 {
		stats.WinningTrades++
		stats.CumulativeReturn += trade.ProfitPct
	} else if trade.ProfitPct < 0 {
		stats.LosingTrades++
		stats.CumulativeReturn += trade.ProfitPct
	}

	// 승률 및 평균 수익률 계산
	stats.WinRate = float64(stats.WinningTrades) / float64(stats.TotalTrades) * 100
	stats.AverageReturn = stats.CumulativeReturn / float64(stats.TotalTrades)

	// 업데이트된 통계 저장
	result.DayOfWeekPerformance[dayOfWeek] = stats
}

// updateMonthlyStats는 월별 수익률 통계를 업데이트합니다
func updateMonthlyStats(result *Result, trade Trade) {
	// 월 형식: "2023-01"
	monthKey := trade.EntryTime.Format("2006-01")

	// 현재 월 수익률 가져오기
	currentReturn, exists := result.MonthlyReturns[monthKey]
	if !exists {
		currentReturn = 0.0
	}

	// 수익률 업데이트
	result.MonthlyReturns[monthKey] = currentReturn + trade.ProfitPct
}

// CalculateDrawdownStats는 자산 이력에서 낙폭 통계를 계산합니다
func CalculateDrawdownStats(equityHistory []EquityPoint) (maxDrawdown float64, avgDrawdown float64, drawdownDuration time.Duration) {
	if len(equityHistory) == 0 {
		return 0, 0, 0
	}

	highWaterMark := equityHistory[0].Equity
	currentDrawdown := 0.0
	maxDrawdown = 0.0
	totalDrawdown := 0.0
	drawdownCount := 0

	drawdownStart := time.Time{}
	inDrawdown := false
	longestDrawdownDuration := time.Duration(0)

	for _, point := range equityHistory {
		// 신규 최고점 갱신
		if point.Equity > highWaterMark {
			highWaterMark = point.Equity

			// 낙폭 종료
			if inDrawdown {
				inDrawdown = false
				duration := point.Timestamp.Sub(drawdownStart)
				if duration > longestDrawdownDuration {
					longestDrawdownDuration = duration
				}
			}
		}

		// 현재 낙폭 계산
		if highWaterMark > 0 {
			currentDrawdown = (highWaterMark - point.Equity) / highWaterMark * 100

			// 낙폭 시작
			if currentDrawdown > 0 && !inDrawdown {
				inDrawdown = true
				drawdownStart = point.Timestamp
			}

			// 최대 낙폭 갱신
			if currentDrawdown > maxDrawdown {
				maxDrawdown = currentDrawdown
			}

			// 평균 낙폭 계산용
			if currentDrawdown > 0 {
				totalDrawdown += currentDrawdown
				drawdownCount++
			}
		}
	}

	// 평균 낙폭 계산
	if drawdownCount > 0 {
		avgDrawdown = totalDrawdown / float64(drawdownCount)
	}

	return maxDrawdown, avgDrawdown, longestDrawdownDuration
}

// GetPerformanceByTimeRange는 특정 기간 동안의 성과를 계산합니다 (예: 월간, 주간)
func GetPerformanceByTimeRange(trades []Trade, start, end time.Time) TimePerformance {
	performance := TimePerformance{}

	for _, trade := range trades {
		// 기간 내의 거래만 필터링
		if trade.EntryTime.After(start) && trade.ExitTime.Before(end) {
			performance.TotalTrades++

			if trade.ProfitPct > 0 {
				performance.WinningTrades++
				performance.CumulativeReturn += trade.ProfitPct
			} else if trade.ProfitPct < 0 {
				performance.LosingTrades++
				performance.CumulativeReturn += trade.ProfitPct
			}
		}
	}

	// 승률 및 평균 수익률 계산
	if performance.TotalTrades > 0 {
		performance.WinRate = float64(performance.WinningTrades) / float64(performance.TotalTrades) * 100
		performance.AverageReturn = performance.CumulativeReturn / float64(performance.TotalTrades)
	}

	return performance
}

// CalculateAnnualizedReturn은 연율화 수익률을 계산합니다
func CalculateAnnualizedReturn(startEquity, endEquity float64, startTime, endTime time.Time) float64 {
	// 총 수익률
	totalReturn := (endEquity - startEquity) / startEquity

	// 거래 기간 (연 단위)
	yearDiff := float64(endTime.Sub(startTime)) / float64(365*24*time.Hour)

	// 연율화 수익률 계산 공식: (1 + totalReturn)^(1/yearDiff) - 1
	if yearDiff > 0 {
		return math.Pow(1+totalReturn, 1/yearDiff) - 1
	}

	return totalReturn // 1년 미만인 경우
}
