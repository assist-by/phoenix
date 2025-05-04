package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// 바이낸스 API 설정
	Binance struct {
		// 메인넷 API 키
		APIKey    string `envconfig:"BINANCE_API_KEY" required:"true"`
		SecretKey string `envconfig:"BINANCE_SECRET_KEY" required:"true"`

		// 테스트넷 API 키
		TestAPIKey    string `envconfig:"BINANCE_TEST_API_KEY" required:"false"`
		TestSecretKey string `envconfig:"BINANCE_TEST_SECRET_KEY" required:"false"`

		// 테스트넷 사용 여부
		UseTestnet bool `envconfig:"USE_TESTNET" default:"false"`
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
		FetchInterval   time.Duration `envconfig:"FETCH_INTERVAL" default:"15m"`
		CandleLimit     int           `envconfig:"CANDLE_LIMIT" default:"100"`
		Symbols         []string      `envconfig:"SYMBOLS" default:""`              // 커스텀 심볼 목록
		UseTopSymbols   bool          `envconfig:"USE_TOP_SYMBOLS" default:"false"` // 거래량 상위 심볼 사용 여부
		TopSymbolsCount int           `envconfig:"TOP_SYMBOLS_COUNT" default:"3"`   // 거래량 상위 심볼 개수
	}

	// 거래 설정
	Trading struct {
		Leverage int `envconfig:"LEVERAGE" default:"5" validate:"min=1,max=100"`
	}

	// 백테스트 설정 추가
	Backtest struct {
		Strategy       string  `envconfig:"BACKTEST_STRATEGY" default:"MACD+SAR+EMA"`
		Symbol         string  `envconfig:"BACKTEST_SYMBOL" default:"BTCUSDT"`
		Days           int     `envconfig:"BACKTEST_DAYS" default:"30"`
		Interval       string  `envconfig:"BACKTEST_INTERVAL" default:"15m"`
		InitialBalance float64 `envconfig:"BACKTEST_INITIAL_BALANCE" default:"10000.0"` // 초기 잔고
		Leverage       int     `envconfig:"BACKTEST_LEVERAGE" default:"5"`              // 레버리지
		SlippagePct    float64 `envconfig:"BACKTEST_SLIPPAGE_PCT" default:"0.0"`        // 슬리피지 비율
		SaveResults    bool    `envconfig:"BACKTEST_SAVE_RESULTS" default:"false"`      // 결과 저장 여부
		ResultsPath    string  `envconfig:"BACKTEST_RESULTS_PATH" default:"./results"`  // 결과 저장 경로
	}
}

// ValidateConfig는 설정이 유효한지 확인합니다.
func ValidateConfig(cfg *Config) error {
	if cfg.Binance.UseTestnet {
		// 테스트넷 모드일 때 테스트넷 API 키 검증
		if cfg.Binance.TestAPIKey == "" || cfg.Binance.TestSecretKey == "" {
			return fmt.Errorf("테스트넷 모드에서는 BINANCE_TEST_API_KEY와 BINANCE_TEST_SECRET_KEY가 필요합니다")
		}
	} else {
		// 메인넷 모드일 때 메인넷 API 키 검증
		if cfg.Binance.APIKey == "" || cfg.Binance.SecretKey == "" {
			return fmt.Errorf("메인넷 모드에서는 BINANCE_API_KEY와 BINANCE_SECRET_KEY가 필요합니다")
		}
	}

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

	// 심볼 문자열 파싱
	if symbolsStr := os.Getenv("SYMBOLS"); symbolsStr != "" {
		cfg.App.Symbols = strings.Split(symbolsStr, ",")
		for i, s := range cfg.App.Symbols {
			cfg.App.Symbols[i] = strings.TrimSpace(s)
		}
	}

	// 설정값 검증
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("설정값 검증 실패: %w", err)
	}

	return &cfg, nil
}
