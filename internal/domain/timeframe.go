package domain

import (
	"fmt"
	"time"
)

// ConvertHourlyToCurrentDaily는 1시간봉 데이터를 사용하여 특정 시점까지의
// "현재 진행 중인" 일봉을 생성합니다.
// hourlyCandles: 1시간봉 캔들 리스트
// asOf: 기준 시점 (이 시점까지의 데이터로 일봉 구성)
// Returns: 구성된 일봉 캔들 또는 데이터가 없는 경우 nil
func ConvertHourlyToCurrentDaily(hourlyCandles CandleList, asOf time.Time) (*Candle, error) {
	if len(hourlyCandles) == 0 {
		return nil, fmt.Errorf("캔들 데이터가 비어있습니다")
	}

	// 시간 정렬 확인 (첫 캔들이 가장 오래된 데이터)
	for i := 1; i < len(hourlyCandles); i++ {
		if hourlyCandles[i].OpenTime.Before(hourlyCandles[i-1].OpenTime) {
			return nil, fmt.Errorf("캔들 데이터가 시간순으로 정렬되어 있지 않습니다")
		}
	}

	// asOf와 같은 날짜(UTC 기준)의 캔들만 필터링
	// UTC 기준 자정(00:00:00)
	startOfDay := time.Date(
		asOf.UTC().Year(),
		asOf.UTC().Month(),
		asOf.UTC().Day(),
		0, 0, 0, 0,
		time.UTC,
	)

	// 현재 날짜의 캔들 필터링
	var todayCandles CandleList
	for _, candle := range hourlyCandles {
		// 오늘 자정 이후 & asOf 이전 캔들만 포함
		if !candle.OpenTime.Before(startOfDay) && !candle.OpenTime.After(asOf) {
			todayCandles = append(todayCandles, candle)
		}
	}

	// 해당 날짜의 캔들이 없는 경우
	if len(todayCandles) == 0 {
		return nil, fmt.Errorf("지정된 날짜(%s)에 해당하는 캔들 데이터가 없습니다", asOf.Format("2006-01-02"))
	}

	// 일봉 구성
	// - 시가: 첫 번째 캔들의 시가
	// - 고가: 모든 캔들 중 최고가
	// - 저가: 모든 캔들 중 최저가
	// - 종가: 마지막 캔들의 종가
	// - 거래량: 모든 캔들의 거래량 합계
	dailyCandle := Candle{
		Symbol:    todayCandles[0].Symbol,
		Interval:  Interval1d,
		OpenTime:  startOfDay,
		CloseTime: time.Date(startOfDay.Year(), startOfDay.Month(), startOfDay.Day(), 23, 59, 59, 999999999, time.UTC),
		Open:      todayCandles[0].Open,
		High:      todayCandles[0].High,
		Low:       todayCandles[0].Low,
		Close:     todayCandles[len(todayCandles)-1].Close,
		Volume:    0,
	}

	// 최고가, 최저가, 거래량 계산
	for _, candle := range todayCandles {
		if candle.High > dailyCandle.High {
			dailyCandle.High = candle.High
		}
		if candle.Low < dailyCandle.Low {
			dailyCandle.Low = candle.Low
		}
		dailyCandle.Volume += candle.Volume
	}

	return &dailyCandle, nil
}

// CreateRollingDaily는 주어진 시점을 기준으로 최근 N개의 시간봉으로 일봉을 생성합니다
func CreateRollingDaily(hourlyCandles CandleList, referenceTime time.Time, hourCount int) (*Candle, error) {
	// 기본값 설정
	if hourCount <= 0 {
		hourCount = 24 // 기본적으로 24시간 데이터 사용
	}

	// 레퍼런스 시간 이전의 캔들만 필터링
	var validCandles CandleList
	for _, candle := range hourlyCandles {
		if !candle.OpenTime.After(referenceTime) {
			validCandles = append(validCandles, candle)
		}
	}

	// 충분한 데이터가 있는지 확인
	if len(validCandles) < 2 { // 최소 2개 이상은 필요
		return nil, fmt.Errorf("충분한 캔들 데이터가 없습니다: %d개 (최소 2개 필요)", len(validCandles))
	}

	// 최근 N 시간의 캔들만 추출
	startIdx := len(validCandles) - hourCount
	if startIdx < 0 {
		startIdx = 0 // 가용 데이터가 hourCount보다 적으면 모든 데이터 사용
	}
	recentCandles := validCandles[startIdx:]

	// 일봉 데이터 계산
	dailyCandle := Candle{
		Symbol:    recentCandles[0].Symbol,
		Interval:  Interval1d,
		OpenTime:  recentCandles[0].OpenTime,
		CloseTime: recentCandles[len(recentCandles)-1].CloseTime,
		Open:      recentCandles[0].Open,
		High:      recentCandles[0].High,
		Low:       recentCandles[0].Low,
		Close:     recentCandles[len(recentCandles)-1].Close,
		Volume:    0,
	}

	// 최고가, 최저가, 거래량 계산
	for _, candle := range recentCandles {
		if candle.High > dailyCandle.High {
			dailyCandle.High = candle.High
		}
		if candle.Low < dailyCandle.Low {
			dailyCandle.Low = candle.Low
		}
		dailyCandle.Volume += candle.Volume
	}

	return &dailyCandle, nil
}
