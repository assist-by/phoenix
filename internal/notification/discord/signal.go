package discord

import (
	"fmt"

	"github.com/assist-by/phoenix/internal/analysis/signal"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s *signal.Signal) error {
	var title, emoji string
	var color int

	switch s.Type {
	case signal.Long:
		emoji = "🚀"
		title = "LONG"
		color = ColorSuccess
	case signal.Short:
		emoji = "🔻"
		title = "SHORT"
		color = ColorError
	default:
		emoji = "⚠️"
		title = "NO SIGNAL"
		color = ColorInfo
	}

	// 시그널 조건 상태 표시
	longConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 위)
%s MACD (시그널 상향돌파)
%s SAR (SAR이 가격 아래)`,
		getCheckMark(s.Conditions.EMALong),
		getCheckMark(s.Conditions.MACDLong),
		getCheckMark(s.Conditions.SARLong))

	shortConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 아래)
		%s MACD (시그널 하향돌파)
		%s SAR (SAR이 가격 위)`,
		getCheckMark(s.Conditions.EMAShort),
		getCheckMark(s.Conditions.MACDShort),
		getCheckMark(s.Conditions.SARShort))
	// 기술적 지표 값
	technicalValues := fmt.Sprintf("```\n[EMA200]: %.5f\n[MACD Line]: %.5f\n[Signal Line]: %.5f\n[Histogram]: %.5f\n[SAR]: %.5f```",
		s.Conditions.EMAValue,
		s.Conditions.MACDValue,
		s.Conditions.SignalValue,
		s.Conditions.MACDValue-s.Conditions.SignalValue,
		s.Conditions.SARValue)

	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s %s/USDT", emoji, title, s.Symbol)).
		SetColor(color)

	if s.Type != signal.NoSignal {
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			s.StopLoss,
			(s.StopLoss-s.Price)/s.Price*100,
			s.TakeProfit,
			(s.TakeProfit-s.Price)/s.Price*100,
		))
	} else {
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
		))
	}

	embed.AddField("LONG 조건", longConditions, true)
	embed.AddField("SHORT 조건", shortConditions, true)
	embed.AddField("기술적 지표", technicalValues, false)

	return c.sendToWebhook(c.signalWebhook, WebhookMessage{
		Embeds: []Embed{*embed},
	})
}

func getCheckMark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}
