// internal/market/collector.go

package market

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Collector는 시장 데이터 수집기를 구현합니다
type Collector struct {
	client        *Client
	fetchInterval time.Duration
	candleLimit   int
	stopChan      chan struct{}
	mu            sync.RWMutex
	isRunning     bool
}

// CollectorOption은 수집기의 옵션을 정의합니다
type CollectorOption func(*Collector)

// WithFetchInterval은 데이터 수집 간격을 설정합니다
func WithFetchInterval(interval time.Duration) CollectorOption {
	return func(c *Collector) {
		c.fetchInterval = interval
	}
}

// WithCandleLimit은 캔들 데이터 조회 개수를 설정합니다
func WithCandleLimit(limit int) CollectorOption {
	return func(c *Collector) {
		c.candleLimit = limit
	}
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(client *Client, opts ...CollectorOption) *Collector {
	c := &Collector{
		client:        client,
		fetchInterval: 15 * time.Minute, // 기본값 15분
		candleLimit:   100,              // 기본값 100개
		stopChan:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Start는 데이터 수집을 시작합니다
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.isRunning {
		c.mu.Unlock()
		return fmt.Errorf("수집기가 이미 실행 중입니다")
	}
	c.isRunning = true
	c.mu.Unlock()

	// 상위 거래량 심볼 조회 및 데이터 수집 시작
	go c.run(ctx)

	return nil
}

// Stop은 데이터 수집을 중지합니다
func (c *Collector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRunning {
		close(c.stopChan)
		c.isRunning = false
	}
}

// run은 실제 데이터 수집 작업을 수행합니다
func (c *Collector) run(ctx context.Context) {
	ticker := time.NewTicker(c.fetchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("컨텍스트 취소로 수집기를 종료합니다")
			return
		case <-c.stopChan:
			log.Println("수집기를 중지합니다")
			return
		case <-ticker.C:
			if err := c.collect(ctx); err != nil {
				log.Printf("데이터 수집 중 에러 발생: %v", err)
			}
		}
	}
}

// collect는 한 번의 데이터 수집 사이클을 수행합니다
func (c *Collector) collect(ctx context.Context) error {
	// 상위 거래량 심볼 조회
	symbols, err := c.client.GetTopVolumeSymbols(ctx, 3) // 상위 3개 심볼 조회
	if err != nil {
		return fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
	}

	// 각 심볼의 잔고 조회
	balances, err := c.client.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("잔고 조회 실패: %w", err)
	}

	log.Printf("현재 보유 잔고:")
	for asset, balance := range balances {
		if balance.Available > 0 || balance.Locked > 0 {
			log.Printf("%s: 사용가능: %.8f, 잠금: %.8f",
				asset, balance.Available, balance.Locked)
		}
	}

	// 각 심볼의 캔들 데이터 수집
	for _, symbol := range symbols {
		candles, err := c.client.GetKlines(ctx, symbol, c.getIntervalString(), c.candleLimit)
		if err != nil {
			log.Printf("%s 심볼의 캔들 데이터 조회 실패: %v", symbol, err)
			continue
		}

		log.Printf("%s 심볼의 캔들 데이터 %d개 수집 완료", symbol, len(candles))
		// TODO: 수집된 데이터 처리 (데이터베이스 저장 또는 시그널 생성)
	}

	return nil
}

// getIntervalString은 수집 간격을 바이낸스 API 형식의 문자열로 변환합니다
func (c *Collector) getIntervalString() string {
	switch c.fetchInterval {
	case 1 * time.Minute:
		return "1m"
	case 3 * time.Minute:
		return "3m"
	case 5 * time.Minute:
		return "5m"
	case 15 * time.Minute:
		return "15m"
	case 30 * time.Minute:
		return "30m"
	case 1 * time.Hour:
		return "1h"
	case 2 * time.Hour:
		return "2h"
	case 4 * time.Hour:
		return "4h"
	case 6 * time.Hour:
		return "6h"
	case 8 * time.Hour:
		return "8h"
	case 12 * time.Hour:
		return "12h"
	case 24 * time.Hour:
		return "1d"
	default:
		return "15m" // 기본값
	}
}
