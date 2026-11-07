package service

import (
	"errors"
	"testing"
)

// Telegram takes a bot token and a chat, ignores a URL, and lists show
// only the chat.
func TestTelegramDestination(t *testing.T) {
	if _, err := (telegramDestination{}).check(AlertDestinationInput{BotToken: "nope", ChatID: "1"}); !errors.Is(err, ErrAlertBotToken) {
		t.Errorf("a bad token: %v", err)
	}
	for _, chat := range []string{"chat", "@abc", ""} {
		if _, err := (telegramDestination{}).check(AlertDestinationInput{BotToken: telegramToken, ChatID: chat}); !errors.Is(err, ErrAlertChatID) {
			t.Errorf("chat %q: %v", chat, err)
		}
	}
	for _, chat := range []string{"-1001234", "@moin_alerts"} {
		to, err := telegramDestination{}.check(AlertDestinationInput{BotToken: " " + telegramToken + " ", ChatID: " " + chat + " ", URL: "ignored"})
		if err != nil || to != (alertTarget{target: telegramToken, chatID: chat}) {
			t.Fatalf("check(%q) = %+v, %v", chat, to, err)
		}
		if hint := (telegramDestination{}).hint(to); hint != "chat "+chat {
			t.Errorf("hint = %q", hint)
		}
	}
}
