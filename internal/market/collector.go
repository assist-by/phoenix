package market

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/exchange"
	"github.com/assist-by/phoenix/internal/notification/discord"
	"github.com/assist-by/phoenix/internal/position"
	"github.com/assist-by/phoenix/internal/strategy"
)

// RetryConfig는 재시도 설정을 정의합니다
type RetryConfig struct {
	MaxRetries int           // 최대 재시도 횟수
	BaseDelay  time.Duration // 기본 대기 시간
	MaxDelay   time.Duration // 최대 대기 시간
	Factor     float64       // 대기 시간 증가 계수
}

// Collector는 시장 데이터 수집기를 구현합니다
type Collector struct {
	exchange        exchange.Exchange
	discord         *discord.Client
	strategy        strategy.Strategy
	config          *config.Config
	positionManager position.Manager

	retry RetryConfig
	mu    sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(exchange exchange.Exchange, discord *discord.Client, strategy strategy.Strategy, positionManager position.Manager, config *config.Config, opts ...CollectorOption) *Collector {
	c := &Collector{
		exchange:        exchange,
		discord:         discord,
		strategy:        strategy,
		positionManager: positionManager,
		config:          config,
		retry: RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
			Factor:     2.0,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// CollectorOption은 수집기의 옵션을 정의합니다
type CollectorOption func(*Collector)

// WithCandleLimit은 캔들 데이터 조회 개수를 설정합니다
func WithCandleLimit(limit int) CollectorOption {
	return func(c *Collector) {
		c.config.App.CandleLimit = limit
	}
}

// WithRetryConfig는 재시도 설정을 지정합니다
func WithRetryConfig(config RetryConfig) CollectorOption {
	return func(c *Collector) {
		c.retry = config
	}
}

// collect는 한 번의 데이터 수집 사이클을 수행합니다
func (c *Collector) Collect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 심볼 목록 결정
	var symbols []string
	var err error

	if c.config.App.UseTopSymbols {
		symbols, err = c.exchange.GetTopVolumeSymbols(ctx, c.config.App.TopSymbolsCount)
		if err != nil {
			errMsg := fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
			if discordErr := c.discord.SendError(errMsg); discordErr != nil {
				log.Printf("에러 알림 전송 실패: %v", discordErr)
			}
			return errMsg
		}
	} else {
		// 설정된 심볼 사용
		if len(c.config.App.Symbols) > 0 {
			symbols = c.config.App.Symbols
		} else {
			// 기본값으로 BTCUSDT 사용
			symbols = []string{"BTCUSDT"}
		}
	}

	// 잔고 정보 조회
	balances, err := c.exchange.GetBalance(ctx)
	if err != nil {
		errMsg := fmt.Errorf("잔고 조회 실패: %w", err)
		if discordErr := c.discord.SendError(errMsg); discordErr != nil {
			log.Printf("에러 알림 전송 실패: %v", discordErr)
		}
		return errMsg
	}

	// 잔고 정보 로깅 및 알림
	balanceInfo := "현재 보유 잔고:\n"
	for asset, balance := range balances {
		if balance.Available > 0 || balance.Locked > 0 {
			balanceInfo += fmt.Sprintf("%s: 총: %.8f, 사용가능: %.8f, 잠금: %.8f\n",
				asset, balance.CrossWalletBalance, balance.Available, balance.Locked)
		}
	}
	if c.discord != nil {
		if err := c.discord.SendInfo(balanceInfo); err != nil {
			log.Printf("잔고 정보 알림 전송 실패: %v", err)
		}
	}

	// 각 심볼의 캔들 데이터 수집
	for _, symbol := range symbols {
		err := c.withRetry(ctx, fmt.Sprintf("%s 캔들 데이터 조회", symbol), func() error {
			candles, err := c.exchange.GetKlines(ctx, symbol, c.getIntervalString(), c.config.App.CandleLimit)
			if err != nil {
				return err
			}

			log.Printf("%s 심볼의 캔들 데이터 %d개 수집 완료", symbol, len(candles))

			// 시그널 감지
			s, err := c.strategy.Analyze(ctx, symbol, candles)
			if err != nil {
				log.Printf("시그널 감지 실패 (%s): %v", symbol, err)
				return nil
			}

			// 시그널 정보 로깅
			log.Printf("%s 시그널 감지 결과: %+v", symbol, s)

			if s != nil {
				// Discord로 시그널 알림 전송
				if err := c.discord.SendSignal(s); err != nil {
					log.Printf("시그널 알림 전송 실패 (%s): %v", symbol, err)
				}

				if s.Type != domain.NoSignal {
					// 매매 실행
					if err := c.ExecuteSignalTrade(ctx, s); err != nil {
						errMsg := fmt.Errorf("매매 실행 실패 (%s): %w", symbol, err)
						if discordErr := c.discord.SendError(errMsg); discordErr != nil {
							log.Printf("에러 알림 전송 실패: %v", discordErr)
						}
						return errMsg
					} else {
						log.Printf("%s %s 포지션 진입 및 TP/SL 설정 완료", s.Symbol, s.Type.String())
					}
				}
			}

			return nil
		})

		if err != nil {
			log.Printf("%s 심볼 데이터 수집 실패: %v", symbol, err)
			continue // 한 심볼 처리 실패해도 다음 심볼 진행
		}
	}

	return nil
}

// CalculatePosition은 코인의 특성과 최소 주문 단위를 고려하여 실제 포지션 크기와 수량을 계산합니다
// 단계별 계산:
// 1. 이론적 최대 포지션 = 가용잔고 × 레버리지
// 2. 이론적 최대 수량 = 이론적 최대 포지션 ÷ 코인 가격
// 3. 실제 수량 = 이론적 최대 수량을 최소 주문 단위로 내림
// 4. 실제 포지션 가치 = 실제 수량 × 코인 가격
// 5. 수수료 및 마진 고려해 최종 조정
func (c *Collector) CalculatePosition(
	balance float64, // 가용 잔고
	totalBalance float64, // 총 잔고 (usdtBalance.CrossWalletBalance)
	leverage int, // 레버리지
	coinPrice float64, // 코인 현재 가격
	stepSize float64, // 코인 최소 주문 단위
	maintMargin float64, // 유지증거금률
) (PositionSizeResult, error) {
	// 1. 사용 가능한 잔고에서 항상 90%만 사용
	maxAllocationPercent := 0.9
	allocatedBalance := totalBalance * maxAllocationPercent

	// 가용 잔고가 필요한 할당 금액보다 작은 경우 에러 반환
	if balance < allocatedBalance {
		return PositionSizeResult{}, fmt.Errorf("가용 잔고가 부족합니다: 필요 %.2f USDT, 현재 %.2f USDT",
			allocatedBalance, balance)
	}

	// 2. 레버리지 적용 및 수수료 고려
	totalFeeRate := 0.002 // 0.2% (진입 + 청산 수수료 + 여유분)
	effectiveMargin := maintMargin + totalFeeRate

	// 안전하게 사용 가능한 최대 포지션 가치 계산
	maxSafePositionValue := (allocatedBalance * float64(leverage)) / (1 + effectiveMargin)

	// 3. 최대 안전 수량 계산
	maxSafeQuantity := maxSafePositionValue / coinPrice

	// 4. 최소 주문 단위로 수량 조정
	// stepSize가 0.001이면 소수점 3자리
	precision := 0
	temp := stepSize
	for temp < 1.0 {
		temp *= 10
		precision++
	}

	// 소수점 자릿수에 맞춰 내림 계산
	scale := math.Pow(10, float64(precision))
	steps := math.Floor(maxSafeQuantity / stepSize)
	adjustedQuantity := steps * stepSize

	// 소수점 자릿수 정밀도 보장
	adjustedQuantity = math.Floor(adjustedQuantity*scale) / scale

	// 5. 최종 포지션 가치 계산
	finalPositionValue := adjustedQuantity * coinPrice

	// 포지션 크기에 대한 추가 안전장치 (최소값과 최대값 제한)
	finalPositionValue = math.Min(finalPositionValue, maxSafePositionValue)

	// 소수점 2자리까지 내림 (USDT 기준)
	return PositionSizeResult{
		PositionValue: math.Floor(finalPositionValue*100) / 100,
		Quantity:      adjustedQuantity,
	}, nil
}

// TODO: 단순 상향돌파만 체크하는게 아니라 MACD가 0 이상인지 이하인지 그거도 추세 판단하는데 사용되는걸 적용해야한다.
// ExecuteSignalTrade는 감지된 시그널에 따라 매매를 실행합니다
func (c *Collector) ExecuteSignalTrade(ctx context.Context, s *strategy.Signal) error {
	if s.Type == domain.NoSignal {
		return nil // 시그널이 없으면 아무것도 하지 않음
	}

	// 포지션 요청 객체 생성
	req := &position.PositionRequest{
		Signal:     s,
		Leverage:   c.config.Trading.Leverage,
		RiskFactor: 0.9, // 계정 잔고의 90% 사용 (설정에서 가져올 수도 있음)
	}

	// 포지션 매니저를 통해 포지션 오픈
	_, err := c.positionManager.OpenPosition(ctx, req)
	if err != nil {
		// 에러 발생 시 Discord 알림 전송
		errorMsg := fmt.Sprintf("포지션 진입 실패 (%s): %v", s.Symbol, err)
		if discordErr := c.discord.SendError(fmt.Errorf(errorMsg)); discordErr != nil {
			log.Printf("에러 알림 전송 실패: %v", discordErr)
		}

		return fmt.Errorf("매매 실행 실패: %w", err)
	}

	return nil

}

// getIntervalString은 수집 간격을 바이낸스 API 형식의 문자열로 변환합니다
func (c *Collector) getIntervalString() domain.TimeInterval {
	switch c.config.App.FetchInterval {
	case 1 * time.Minute:
		return domain.Interval1m
	case 3 * time.Minute:
		return domain.Interval3m
	case 5 * time.Minute:
		return domain.Interval5m
	case 15 * time.Minute:
		return domain.Interval15m
	case 30 * time.Minute:
		return domain.Interval30m
	case 1 * time.Hour:
		return domain.Interval1h
	case 2 * time.Hour:
		return domain.Interval2h
	case 4 * time.Hour:
		return domain.Interval4h
	case 6 * time.Hour:
		return domain.Interval6h
	case 8 * time.Hour:
		return domain.Interval8h
	case 12 * time.Hour:
		return domain.Interval12h
	case 24 * time.Hour:
		return domain.Interval1d
	default:
		return domain.Interval15m // 기본값
	}
}

// withRetry는 재시도 로직을 구현한 래퍼 함수입니다
func (c *Collector) withRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	delay := c.retry.BaseDelay

	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := fn(); err != nil {
				lastErr = err

				// 재시도 가능한 오류인지 확인
				if !IsRetryableError(err) {
					// 재시도가 필요 없는 오류는 바로 반환
					log.Printf("%s 실패 (재시도 불필요): %v", operation, err)
					return err
				}

				if attempt == c.retry.MaxRetries {
					// 마지막 시도에서 실패하면 Discord로 에러 알림 전송
					errMsg := fmt.Errorf("%s 실패 (최대 재시도 횟수 초과): %v", operation, err)
					if c.discord != nil {
						if notifyErr := c.discord.SendError(errMsg); notifyErr != nil {
							log.Printf("Discord 에러 알림 전송 실패: %v", notifyErr)
						}
					}
					return fmt.Errorf("최대 재시도 횟수 초과: %w", lastErr)
				}

				log.Printf("%s 실패 (attempt %d/%d): %v",
					operation, attempt+1, c.retry.MaxRetries, err)

				// 다음 재시도 전 대기
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
					// 대기 시간을 증가시키되, 최대 대기 시간을 넘지 않도록 함
					delay = time.Duration(float64(delay) * c.retry.Factor)
					if delay > c.retry.MaxDelay {
						delay = c.retry.MaxDelay
					}
				}
				continue
			}
			return nil
		}
	}
	return lastErr
}

// IsRetryableError 함수는 재 시도 할 작업인지 검사하는 함수
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// HTTP 관련 일시적 오류인 경우만 재시도
	if strings.Contains(errStr, "HTTP 에러(502)") || // Bad Gateway
		strings.Contains(errStr, "HTTP 에러(503)") || // Service Unavailable
		strings.Contains(errStr, "HTTP 에러(504)") || // Gateway Timeout
		strings.Contains(errStr, "i/o timeout") || // 타임아웃
		strings.Contains(errStr, "connection refused") || // 연결 거부
		strings.Contains(errStr, "EOF") || // 예기치 않은 연결 종료
		strings.Contains(errStr, "no such host") { // DNS 해석 실패
		return true
	}

	return false
}
