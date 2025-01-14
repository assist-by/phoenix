package discord

import (
	"time"
)

// WebhookMessage는 Discord 웹훅 메시지를 정의합니다
type WebhookMessage struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// Embed는 Discord 메시지 임베드를 정의합니다
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// EmbedField는 임베드 필드를 정의합니다
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// EmbedFooter는 임베드 푸터를 정의합니다
type EmbedFooter struct {
	Text string `json:"text"`
}

// 임베드 색상 상수
const (
	ColorSuccess = 0x00FF00 // 초록색
	ColorError   = 0xFF0000 // 빨간색
	ColorInfo    = 0x0099FF // 파란색
)

// NewEmbed는 새로운 임베드를 생성합니다
func NewEmbed() *Embed {
	return &Embed{}
}

// SetTitle은 임베드 제목을 설정합니다
func (e *Embed) SetTitle(title string) *Embed {
	e.Title = title
	return e
}

// SetDescription은 임베드 설명을 설정합니다
func (e *Embed) SetDescription(desc string) *Embed {
	e.Description = desc
	return e
}

// SetColor는 임베드 색상을 설정합니다
func (e *Embed) SetColor(color int) *Embed {
	e.Color = color
	return e
}

// AddField는 임베드에 필드를 추가합니다
func (e *Embed) AddField(name, value string, inline bool) *Embed {
	e.Fields = append(e.Fields, EmbedField{
		Name:   name,
		Value:  value,
		Inline: inline,
	})
	return e
}

// SetFooter는 임베드 푸터를 설정합니다
func (e *Embed) SetFooter(text string) *Embed {
	e.Footer = &EmbedFooter{Text: text}
	return e
}

// SetTimestamp는 임베드 타임스탬프를 설정합니다
func (e *Embed) SetTimestamp(t time.Time) *Embed {
	e.Timestamp = t.Format(time.RFC3339)
	return e
}
