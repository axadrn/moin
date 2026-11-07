package mailer

import (
	"context"
	"log/slog"
)

// LogMailer writes every email to the log. Development uses it when no
// Cloudflare credentials are set, so magic links work without a mail account.
type LogMailer struct{}

func (LogMailer) Name() string { return ProviderLog }

func (LogMailer) Send(_ context.Context, message Message) error {
	slog.Info("email not sent, written to the log", "to", message.To, "subject", message.Subject, "text", message.Text)
	return nil
}
