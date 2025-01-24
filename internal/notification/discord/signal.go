package discord

import (
	"fmt"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/signal"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s *signal.Signal) error {
	// 시그널 타입에 따른 이모지와 색상 설정
	var emoji string
	var color int
	switch s.Type {
	case signal.Long:
		emoji = "🚀"
		color = ColorSuccess
	case signal.Short:
		emoji = "🔻"
		color = ColorError
	default:
		emoji = "⏺️"
		color = ColorInfo
	}

	// 시그널 설명 생성
	description := fmt.Sprintf(`**시간**: %s
**현재가**: $%.4f
**방향**: %s
**손절가**: $%.4f (%.2f%%)
**목표가**: $%.4f (%.2f%%)`,
		s.Timestamp.Format("2006-01-02 15:04:05 KST"),
		s.Price,
		fmt.Sprintf("%s %s", emoji, s.Type),
		s.StopLoss,
		(s.StopLoss-s.Price)/s.Price*100,
		s.TakeProfit,
		(s.TakeProfit-s.Price)/s.Price*100,
	)

	// 기술적 지표 필드 추가
	technicalAnalysis := fmt.Sprintf("```\nEMA200: %.4f\nMACD: %.4f\nSignal: %.4f\nSAR: %.4f\n```",
		s.Conditions.EMAValue,
		s.Conditions.MACDValue,
		s.Conditions.SignalValue,
		s.Conditions.SARValue,
	)

	// 임베드 메시지 생성
	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s/USDT", emoji, s.Symbol)).
		SetDescription(description).
		SetColor(color).
		AddField("기술적 지표", technicalAnalysis, false).
		SetFooter("🤖 Phoenix Trading Bot").
		SetTimestamp(time.Now())

	// 시그널 웹훅으로 전송
	return c.sendToWebhook(c.signalWebhook, WebhookMessage{
		Embeds: []Embed{*embed},
	})
}
