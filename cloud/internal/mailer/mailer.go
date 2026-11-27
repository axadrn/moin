// Package mailer sends the mails of moin cloud: sign-in links and alerts.
// It sends through Cloudflare Email Sending, or writes each mail to the
// log when no Cloudflare account is set, for development.
package mailer

import (
	"context"
	"log/slog"
)

// Message is one mail.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer sends one mail.
type Mailer interface {
	Send(ctx context.Context, message Message) error
}

// New sends through Cloudflare with an account and a token, else to the log.
func New(accountID, token, from, api string) Mailer {
	if accountID == "" || token == "" {
		return Log{}
	}
	return NewCloudflare(accountID, token, from, api)
}

// Log writes every mail to the log instead of sending it.
type Log struct{}

func (Log) Send(_ context.Context, message Message) error {
	slog.Info("mail not sent, written to the log", "to", message.To, "subject", message.Subject, "text", message.Text)
	return nil
}
