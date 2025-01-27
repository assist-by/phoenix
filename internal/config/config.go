package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// 바이낸스 API 설정
	Binance struct {
		APIKey    string `envconfig:"BINANCE_API_KEY" required:"true"`
		SecretKey string `envconfig:"BINANCE_SECRET_KEY" required:"true"`
	}

	// 디스코드 웹훅 설정
	Discord struct {
		SignalWebhook string `envconfig:"DISCORD_SIGNAL_WEBHOOK" required:"true"`
		TradeWebhook  string `envconfig:"DISCORD_TRADE_WEBHOOK" required:"true"`
		ErrorWebhook  string `envconfig:"DISCORD_ERROR_WEBHOOK" required:"true"`
		InfoWebhook   string `envconfig:"DISCORD_INFO_WEBHOOK" required:"true"`
	}

	// 애플리케이션 설정
	App struct {
		FetchInterval time.Duration `envconfig:"FETCH_INTERVAL" default:"15m"`
		CandleLimit   int           `envconfig:"CANDLE_LIMIT" default:"100"`
	}

	// 거래 설정
	Trading struct {
		Leverage int `envconfig:"LEVERAGE" default:"5" validate:"min=1,max=100"`
	}
}

// ValidateConfig는 설정이 유효한지 확인합니다.
func ValidateConfig(cfg *Config) error {
	if cfg.Trading.Leverage < 1 || cfg.Trading.Leverage > 100 {
		return fmt.Errorf("레버리지는 1 이상 100 이하이어야 합니다")
	}

	if cfg.App.FetchInterval < 1*time.Minute {
		return fmt.Errorf("FETCH_INTERVAL은 1분 이상이어야 합니다")
	}

	if cfg.App.CandleLimit < 300 {
		return fmt.Errorf("CANDLE_LIMIT은 300 이상이어야 합니다")
	}

	return nil
}

// LoadConfig는 환경변수에서 설정을 로드합니다.
func LoadConfig() (*Config, error) {
	// .env 파일 로드
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf(".env 파일 로드 실패: %w", err)
	}

	var cfg Config
	// 환경변수를 구조체로 파싱
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("환경변수 처리 실패: %w", err)
	}

	// 설정값 검증
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("설정값 검증 실패: %w", err)
	}

	return &cfg, nil
}
