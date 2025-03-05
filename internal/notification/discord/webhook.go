package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 전송합니다
// func (c *Client) SendSignal(signal notification.Signal) error {
// 	embed := NewEmbed().
// 		SetTitle(fmt.Sprintf("트레이딩 시그널: %s", signal.Symbol)).
// 		SetDescription(fmt.Sprintf("**타입**: %s\n**가격**: $%.2f\n**이유**: %s",
// 			signal.Type, signal.Price, signal.Reason)).
// 		SetColor(getColorForSignal(signal.Type)).
// 		SetFooter("Assist by Trading Bot 🤖").
// 		SetTimestamp(signal.Timestamp)

// 	msg := WebhookMessage{
// 		Embeds: []Embed{*embed},
// 	}

// 	return c.sendToWebhook(c.signalWebhook, msg)
// }

// SendError는 에러 알림을 전송합니다
func (c *Client) SendError(err error) error {
	embed := NewEmbed().
		SetTitle("에러 발생").
		SetDescription(fmt.Sprintf("```%v```", err)).
		SetColor(notification.ColorError).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.errorWebhook, msg)
}

// SendInfo는 일반 정보 알림을 전송합니다
func (c *Client) SendInfo(message string) error {
	embed := NewEmbed().
		SetDescription(message).
		SetColor(notification.ColorInfo).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.infoWebhook, msg)
}

// SendTradeInfo는 거래 실행 정보를 전송합니다
func (c *Client) SendTradeInfo(info notification.TradeInfo) error {
	embed := NewEmbed().
		SetTitle(fmt.Sprintf("거래 실행: %s", info.Symbol)).
		SetDescription(fmt.Sprintf(
			"**포지션**: %s\n**수량**: %.8f %s\n**포지션 크기**: %.2f USDT\n**레버리지**: %dx\n**진입가**: $%.2f\n**손절가**: $%.2f\n**목표가**: $%.2f\n**현재 잔고**: %.2f USDT",
			info.PositionType,
			info.Quantity,
			strings.TrimSuffix(info.Symbol, "USDT"), // BTCUSDT에서 BTC만 추출
			info.PositionValue,
			info.Leverage,
			info.EntryPrice,
			info.StopLoss,
			info.TakeProfit,
			info.Balance,
		)).
		SetColor(notification.GetColorForPosition(info.PositionType)).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.tradeWebhook, msg)
}
