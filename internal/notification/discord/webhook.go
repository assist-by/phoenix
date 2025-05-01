package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/assist-by/phoenix/internal/domain"
	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s domain.SignalInterface) error {
	if s == nil {
		return fmt.Errorf("nil signal received")
	}

	var title, emoji string
	var color int

	switch s.GetType() {
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

	// 알림 데이터 가져오기
	notificationData := s.ToNotificationData()

	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s %s/USDT", emoji, title, s.GetSymbol())).
		SetColor(color)

	// 기본 정보 설정 - 모든 시그널 타입에 공통
	if s.GetType() != domain.NoSignal {
		// 손익률 계산 및 표시
		var slPct, tpPct float64
		switch s.GetType() {
		case domain.Long:
			// Long: 실제 수치 그대로 표시
			slPct = (s.GetStopLoss() - s.GetPrice()) / s.GetPrice() * 100
			tpPct = (s.GetTakeProfit() - s.GetPrice()) / s.GetPrice() * 100
		case domain.Short:
			// Short: 부호 반대로 표시
			slPct = (s.GetPrice() - s.GetStopLoss()) / s.GetPrice() * 100
			tpPct = (s.GetPrice() - s.GetTakeProfit()) / s.GetPrice() * 100
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.GetTimestamp().Format("2006-01-02 15:04:05 KST"),
			s.GetPrice(),
			s.GetStopLoss(),
			slPct,
			s.GetTakeProfit(),
			tpPct,
		))
	} else if s.GetType() == domain.PendingLong || s.GetType() == domain.PendingShort {
		// 대기 상태 정보 표시
		var waitingFor string
		if s.GetType() == domain.PendingLong {
			waitingFor = "진입 대기 중"
		} else {
			waitingFor = "진입 대기 중"
		}

		// notificationData에서 대기 상태 설명이 있으면 사용
		if waitDesc, hasWaitDesc := notificationData["대기상태"]; hasWaitDesc {
			waitingFor = waitDesc.(string)
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
**현재가**: $%.2f
**대기 상태**: %s`,
			s.GetTimestamp().Format("2006-01-02 15:04:05 KST"),
			s.GetPrice(),
			waitingFor,
		))
	} else {
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f`,
			s.GetTimestamp().Format("2006-01-02 15:04:05 KST"),
			s.GetPrice(),
		))
	}

	// 전략별 필드들 추가
	// 전략은 ToNotificationData에서 "필드" 키로 필드 목록을 제공할 수 있음
	if fields, hasFields := notificationData["필드"].([]map[string]interface{}); hasFields {
		for _, field := range fields {
			name, _ := field["name"].(string)
			value, _ := field["value"].(string)
			inline, _ := field["inline"].(bool)
			embed.AddField(name, value, inline)
		}
	} else {
		// 기본 필드 추가 (전략이 "필드"를 제공하지 않는 경우)
		// 기술적 지표 요약 표시
		if technicalSummary, hasSummary := notificationData["기술지표요약"].(string); hasSummary {
			embed.AddField("기술적 지표", technicalSummary, false)
		}

		// 기타 조건들 표시
		if conditions, hasConditions := notificationData["조건"].(string); hasConditions {
			embed.AddField("조건", conditions, false)
		}
	}

	return c.sendToWebhook(c.signalWebhook, WebhookMessage{
		Embeds: []Embed{*embed},
	})
}

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

func getCheckMark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}
