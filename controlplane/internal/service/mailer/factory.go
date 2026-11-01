package mailer

import "github.com/axadrn/ops/controlplane/internal/config"

// NewProvider sends through Cloudflare Email Sending. Config validation only
// allows missing credentials in development, where mail goes to the log.
func NewProvider(cfg *config.Config) (Mailer, error) {
	if cfg.CloudflareAccountID == "" {
		return LogMailer{}, nil
	}
	return NewCloudflareMailer(cfg.CloudflareAccountID, cfg.CloudflareAPIToken, cfg.EmailFrom), nil
}
