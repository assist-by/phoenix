package market

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
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
	client        *Client
	discord       *discord.Client
	fetchInterval time.Duration
	candleLimit   int
	retry         RetryConfig
	detector      *signal.Detector
	mu            sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(client *Client, discord *discord.Client, fetchInterval time.Duration, candleLimit int, opts ...CollectorOption) *Collector {
	c := &Collector{
		client:        client,
		discord:       discord,
		fetchInterval: fetchInterval,
		candleLimit:   candleLimit,
		detector: signal.NewDetector(signal.DetectorConfig{
			EMALength:     200,
			StopLossPct:   0.02,
			TakeProfitPct: 0.04,
		}),
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
		c.candleLimit = limit
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

	// 이하 collect() 함수의 내용을 그대로 사용
	var symbols []string
	err := c.withRetry(ctx, "상위 거래량 심볼 조회", func() error {
		var err error
		symbols, err = c.client.GetTopVolumeSymbols(ctx, 3)
		return err
	})
	if err != nil {
		return fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
	}

	// 각 심볼의 잔고 조회
	var balances map[string]Balance
	err = c.withRetry(ctx, "잔고 조회", func() error {
		var err error
		balances, err = c.client.GetBalance(ctx)
		return err
	})
	if err != nil {
		return err
	}

	// 잔고 정보 로깅 및 알림
	balanceInfo := "현재 보유 잔고:\n"
	for asset, balance := range balances {
		if balance.Available > 0 || balance.Locked > 0 {
			balanceInfo += fmt.Sprintf("%s: 사용가능: %.8f, 잠금: %.8f\n",
				asset, balance.Available, balance.Locked)
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

			brackets, err := c.client.GetLeverageBrackets(ctx, symbol)
			if err != nil {
				return fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
			}

			var symbolBracket *SymbolBrackets
			for _, b := range brackets {
				if b.Symbol == symbol {
					symbolBracket = &b
					break
				}
			}

			if symbolBracket != nil && len(symbolBracket.Brackets) > 0 {
				info := fmt.Sprintf("%s 유지증거금 정보:\n```", symbol)
				for _, bracket := range symbolBracket.Brackets {
					info += fmt.Sprintf("\n구간 %d: 최대레버리지 %dx, 유지증거금율 %.4f%%, 최대 포지션 %.2f USDT",
						bracket.Bracket,
						bracket.InitialLeverage,
						bracket.MaintMarginRatio*100,
						bracket.Notional)
				}
				info += "```"

				if err := c.discord.SendInfo(info); err != nil {
					log.Printf("유지증거금 정보 알림 전송 실패: %v", err)
				}
			}

			candles, err := c.client.GetKlines(ctx, symbol, c.getIntervalString(), c.candleLimit)
			if err != nil {
				return err
			}

			log.Printf("%s 심볼의 캔들 데이터 %d개 수집 완료", symbol, len(candles))

			// 캔들 데이터를 indicator.PriceData로 변환
			prices := make([]indicator.PriceData, len(candles))
			for i, candle := range candles {
				prices[i] = indicator.PriceData{
					Time:   time.Unix(candle.OpenTime/1000, 0),
					Open:   candle.Open,
					High:   candle.High,
					Low:    candle.Low,
					Close:  candle.Close,
					Volume: candle.Volume,
				}
			}

			// 시그널 감지
			s, err := c.detector.Detect(symbol, prices)
			if err != nil {
				log.Printf("시그널 감지 실패 (%s): %v", symbol, err)
				return nil
			}

			// 시그널 정보 로깅
			log.Printf("%s 시그널 감지 결과: %+v", symbol, s)

			if s != nil {
				if err := c.discord.SendSignal(s); err != nil {
					log.Printf("시그널 알림 전송 실패 (%s): %v", symbol, err)
				}

				if s.Type != signal.NoSignal {
					// 진입 가능 여부 확인
					available, reason, positionValue, err := c.checkEntryAvailable(ctx, s)
					if err != nil {
						log.Printf("진입 가능 여부 확인 실패: %v", err)
						if err := c.discord.SendError(err); err != nil {
							log.Printf("에러 알림 전송 실패: %v", err)
						}

					}

					if !available {
						log.Printf("진입 불가: %s", reason)

					}

					balances, err := c.client.GetBalance(ctx)
					if err != nil {
						return fmt.Errorf("잔고 조회 실패: %w", err)
					}
					usdtBalance := balances["USDT"].Available

					// TradeInfo 생성
					tradeInfo := notification.TradeInfo{
						Symbol:        s.Symbol,
						PositionType:  s.Type.String(),
						PositionValue: positionValue,
						EntryPrice:    s.Price,
						StopLoss:      s.StopLoss,
						TakeProfit:    s.TakeProfit,
						Balance:       usdtBalance,
						Leverage:      5,
					}

					if err := c.discord.SendTradeInfo(tradeInfo); err != nil {
						log.Printf("거래 정보 알림 전송 실패: %v", err)
						if err := c.discord.SendError(err); err != nil {
							log.Printf("에러 알림 전송 실패: %v", err)
						}
					}
				}
			}

			return nil
		})
		if err != nil {
			log.Printf("%s 심볼 데이터 수집 실패: %v", symbol, err)
			continue
		}
	}

	return nil
}

// calculatePositionValue는 수수료와 유지증거금을 고려하여 최대 포지션 크기를 계산합니다
// 최대 포지션 크기 계산식
// 최대 포지션 = 가용잔고 × 레버리지 / (1 + 유지증거금률 + 총수수료율)
func (c *Collector) calculatePositionValue(
	balance float64,
	leverage int,
	maintMargin float64,
) float64 {
	// 수수료율 (진입 + 청산)
	totalFeeRate := 0.001 // 0.1%

	// 포지션 크기 계산
	positionSize := (balance * float64(leverage)) / (1 + maintMargin + totalFeeRate)

	return math.Floor(positionSize*100) / 100 // 소수점 2자리까지 내림
}

// findBracket은 주어진 레버리지에 해당하는 브라켓을 찾습니다
func findBracket(brackets []LeverageBracket, leverage int) *LeverageBracket {
	// 레버리지가 높은 순으로 정렬되어 있으므로,
	// 설정된 레버리지보다 크거나 같은 첫 번째 브라켓을 찾습니다.
	for i := len(brackets) - 1; i >= 0; i-- {
		if brackets[i].InitialLeverage >= leverage {
			return &brackets[i]
		}
	}

	// 찾지 못한 경우 가장 낮은 레버리지 브라켓 반환
	if len(brackets) > 0 {
		return &brackets[0]
	}
	return nil
}

func (c *Collector) checkEntryAvailable(ctx context.Context, coinSignal *signal.Signal) (bool, string, float64, error) {
	// 1. 현재 포지션 조회
	positions, err := c.client.GetPositions(ctx)
	if err != nil {
		if len(positions) == 0 {
			log.Printf("활성 포지션 없음: %s", coinSignal.Symbol)
		} else {
			return false, "", 0, err
		}

	}

	// 포지션 체크크
	for _, pos := range positions {
		if pos.Symbol == coinSignal.Symbol {
			if coinSignal.Type == signal.Long && pos.PositionSide == "LONG" {
				return false, "이미 롱 포지션이 있습니다", 0, nil
			}
			if coinSignal.Type == signal.Short && pos.PositionSide == "SHORT" {
				return false, "이미 숏 포지션이 있습니다", 0, nil
			}
		}
	}

	// 2. 잔고 조회
	balances, err := c.client.GetBalance(ctx)
	if err != nil {
		return false, "", 0, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	// USDT 잔고 확인
	usdtBalance, exists := balances["USDT"]
	if !exists {
		return false, "USDT 잔고가 없습니다", 0, nil
	}

	// 3. 레버리지 브라켓 정보 조회
	brackets, err := c.client.GetLeverageBrackets(ctx, coinSignal.Symbol)
	if err != nil {
		return false, "", 0, fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 해당 심볼의 브라켓 정보 찾기
	var symbolBracket *SymbolBrackets
	for _, b := range brackets {
		if b.Symbol == coinSignal.Symbol {
			symbolBracket = &b
			break
		}
	}

	if symbolBracket == nil || len(symbolBracket.Brackets) == 0 {
		return false, "레버리지 브라켓 정보가 없습니다", 0, nil
	}

	// 설정된 레버리지에 맞는 브라켓 찾기
	leverage := 5 // 레버리지 설정값
	bracket := findBracket(symbolBracket.Brackets, leverage)
	if bracket == nil {
		return false, "적절한 레버리지 브라켓을 찾을 수 없습니다", 0, nil
	}

	// 브라켓 정보 로깅
	log.Printf("선택된 브라켓: 레버리지 %dx, 유지증거금률 %.4f%%, 최대 포지션 %.2f USDT",
		bracket.InitialLeverage,
		bracket.MaintMarginRatio*100,
		bracket.Notional)

	if err := c.discord.SendInfo(fmt.Sprintf("```\n%s\n레버리지: %dx\n유지증거금률: %.4f%%\n최대 포지션: %.2f USDT\n```",
		coinSignal.Symbol,
		bracket.InitialLeverage,
		bracket.MaintMarginRatio*100,
		bracket.Notional)); err != nil {
		log.Printf("브라켓 정보 알림 전송 실패: %v", err)
	}

	// 4. 포지션 크기 계산
	positionValue := c.calculatePositionValue(
		usdtBalance.Available,
		leverage,
		bracket.MaintMarginRatio,
	)

	return true, "", positionValue, nil
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
