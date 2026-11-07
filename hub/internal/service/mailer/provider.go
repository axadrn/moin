package mailer

import "context"

const (
	ProviderCloudflare = "cloudflare"
	ProviderLog        = "log"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Mailer interface {
	Send(ctx context.Context, message Message) error
	Name() string
}
