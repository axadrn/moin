package service

import (
	"context"
	"html"
	"regexp"
	"strings"

	"github.com/axadrn/moin/hub/internal/brand"
)

var (
	telegramTokenPattern = regexp.MustCompile(`^[0-9]{3,20}:[A-Za-z0-9_-]{20,100}$`)
	telegramChatPattern  = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31})$`)
)

// telegramDestination posts through a bot of the workspace to a chat. The
// target is the bot token; the URL of the input is ignored.
type telegramDestination struct{}

func (telegramDestination) check(input AlertDestinationInput) (alertTarget, error) {
	to := alertTarget{target: strings.TrimSpace(input.BotToken), chatID: strings.TrimSpace(input.ChatID)}
	if !telegramTokenPattern.MatchString(to.target) {
		return alertTarget{}, ErrAlertBotToken
	}
	if !telegramChatPattern.MatchString(to.chatID) {
		return alertTarget{}, ErrAlertChatID
	}
	return to, nil
}

func (telegramDestination) hint(to alertTarget) string { return "chat " + to.chatID }

func (telegramDestination) send(ctx context.Context, sender *alertSender, to alertTarget, message alertMessage) error {
	body := map[string]any{"chat_id": to.chatID, "text": telegramText(message), "parse_mode": "HTML", "disable_web_page_preview": true}
	return sender.post(ctx, sender.telegramAPI+"/bot"+to.target+"/sendMessage", body, nil, to.target)
}

// telegramText is an alert in Telegram's HTML.
func telegramText(message alertMessage) string {
	lines := []string{"<b>" + html.EscapeString(message.Title()) + "</b>"}
	for _, line := range message.Lines {
		lines = append(lines, html.EscapeString(line))
	}
	lines = append(lines, `<a href="`+html.EscapeString(message.URL)+`">Open in `+brand.Name+`</a>`)
	return strings.Join(lines, "\n")
}
