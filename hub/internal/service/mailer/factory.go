package mailer

import "github.com/axadrn/moin/hub/internal/config"

// NewProvider sends through Cloudflare Email Sending. Config validation only
// allows missing credentials in development, where mail goes to the log.
func NewProvider(cfg *config.Config) Mailer {
	if cfg.CloudflareAccountID == "" {
		return LogMailer{}
	}
	return NewCloudflareMailer(cfg.CloudflareAccountID, cfg.CloudflareAPIToken, cfg.EmailFrom)
}
