package discord

import (
	"fmt"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/strategy"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s *strategy.Signal) error {
	var title, emoji string
	var color int

	switch s.Type {
	case domain.Long:
		emoji = "🚀"
		title = "LONG"
		color = notification.ColorSuccess
	case domain.Short:
		emoji = "🔻"
		title = "SHORT"
		color = notification.ColorError
	case domain.PendingLong:
		emoji = "⏳"
		title = "PENDING LONG"
		color = notification.ColorWarning
	case domain.PendingShort:
		emoji = "⏳"
		title = "PENDING SHORT"
		color = notification.ColorWarning
	default:
		emoji = "⚠️"
		title = "NO SIGNAL"
		color = notification.ColorInfo
	}

	// 시그널 조건 상태 표시
	longConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 위)
%s MACD (시그널 상향돌파)
%s SAR (SAR이 가격 아래)`,
		getCheckMark(s.Conditions["EMALong"].(bool)),
		getCheckMark(s.Conditions["MACDLong"].(bool)),
		getCheckMark(s.Conditions["SARLong"].(bool)))

	shortConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 아래)
		%s MACD (시그널 하향돌파)
		%s SAR (SAR이 가격 위)`,
		getCheckMark(s.Conditions["EMAShort"].(bool)),
		getCheckMark(s.Conditions["MACDShort"].(bool)),
		getCheckMark(s.Conditions["SARShort"].(bool)))

	// 기술적 지표 값
	technicalValues := fmt.Sprintf("```\n[EMA200]: %.5f\n[MACD Line]: %.5f\n[Signal Line]: %.5f\n[Histogram]: %.5f\n[SAR]: %.5f```",
		s.Conditions["EMAValue"].(float64),
		s.Conditions["MACDValue"].(float64),
		s.Conditions["SignalValue"].(float64),
		s.Conditions["MACDValue"].(float64)-s.Conditions["SignalValue"].(float64),
		s.Conditions["SARValue"].(float64))

	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s %s/USDT", emoji, title, s.Symbol)).
		SetColor(color)

	if s.Type != domain.NoSignal {
		// 손익률 계산 및 표시
		var slPct, tpPct float64
		switch s.Type {
		case domain.Long:
			// Long: 실제 수치 그대로 표시
			slPct = (s.StopLoss - s.Price) / s.Price * 100
			tpPct = (s.TakeProfit - s.Price) / s.Price * 100
		case domain.Short:
			// Short: 부호 반대로 표시
			slPct = (s.Price - s.StopLoss) / s.Price * 100
			tpPct = (s.Price - s.TakeProfit) / s.Price * 100
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			s.StopLoss,
			slPct,
			s.TakeProfit,
			tpPct,
		))
	} else if s.Type == domain.PendingLong || s.Type == domain.PendingShort {
		// 대기 상태 정보 표시
		var waitingFor string
		if s.Type == domain.PendingLong {
			waitingFor = "SAR가 캔들 아래로 이동 대기 중"
		} else {
			waitingFor = "SAR가 캔들 위로 이동 대기 중"
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
**현재가**: $%.2f
**대기 상태**: %s
**조건**: MACD 크로스 발생, SAR 위치 부적절`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			waitingFor,
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
