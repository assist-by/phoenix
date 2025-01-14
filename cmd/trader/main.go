// cmd/trader/main.go

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	// Discord 클라이언트 생성
	discordClient := discord.NewClient(
		cfg.Discord.SignalWebhook,
		cfg.Discord.TradeWebhook,
		cfg.Discord.ErrorWebhook,
		cfg.Discord.InfoWebhook,
		discord.WithTimeout(10*time.Second),
	)

	// 시그널 알림 테스트
	testSignal := notification.Signal{
		Type:      notification.SignalLong,
		Symbol:    "BTCUSDT",
		Price:     50000.0,
		Timestamp: time.Now(),
		Reason:    "테스트 시그널입니다.",
	}
	discordClient.SendSignal(testSignal)

	// 거래 실행 알림 테스트
	testTrade := notification.TradeInfo{
		Symbol:       "BTCUSDT",
		PositionType: "LONG",
		Quantity:     0.1,
		EntryPrice:   50000.0,
		StopLoss:     49000.0,
		TakeProfit:   52000.0,
	}
	discordClient.SendTradeInfo(testTrade)

	// 에러 알림 테스트
	testError := fmt.Errorf("테스트 에러 발생")
	discordClient.SendError(testError)
}
