// Package config reads the site's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

type Config struct {
	RenderConfig
	Port string
	// OldURLs are addresses the site had before APP_URL, such as
	// https://old.example.com. A request on one goes on to APP_URL.
	OldURLs []string

	// Database is the SQLite file of moin cloud: DATABASE_FILE.
	Database string
	// EmailFrom sends the sign-in links and alerts, through Cloudflare
	// Email Sending with CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN.
	// Without them every mail goes to the log.
	EmailFrom           string
	CloudflareAccountID string
	CloudflareAPIToken  string
	CloudflareAPIURL    string
	// The GitHub App signs people in with GitHub: GITHUB_APP_CLIENT_ID and
	// GITHUB_APP_CLIENT_SECRET. Without them only the mail link signs in.
	GitHubClientID     string
	GitHubClientSecret string
	GitHubURL          string
	GitHubAPIURL       string
	// The landing page shows the knocks of one server in moin cloud:
	// KNOCKS_SERVER, the workspace id and the server's name, like
	// 3f9c/web-1. Without it the landing page shows none.
	KnocksWorkspace string
	KnocksServer    string
	// Stripe sells moin cloud: STRIPE_SECRET_KEY, STRIPE_WEBHOOK_SECRET,
	// the product STRIPE_PRODUCT_ID_PRO, and its two prices
	// STRIPE_PRICE_ID_PRO_MONTHLY and STRIPE_PRICE_ID_PRO_YEARLY. The
	// names are the old service's, so its settings keep working. Without them
	// nobody can subscribe.
	StripeSecretKey     string
	StripeWebhookSecret string
	StripeProduct       string
	StripePriceMonthly  string
	StripePriceYearly   string
	StripeAPIURL        string
	// The bucket for the copies servers send: COPIES_ENDPOINT, like
	// https://<account>.r2.cloudflarestorage.com, COPIES_BUCKET, and
	// COPIES_ACCESS_KEY and COPIES_SECRET_KEY, a key that may read, write
	// and delete objects in that bucket only. Without them no copy is
	// taken.
	CopiesEndpoint  string
	CopiesBucket    string
	CopiesAccessKey string
	CopiesSecretKey string
}

// RenderConfig is what templates may know of the settings: nothing secret.
type RenderConfig struct {
	AppURL string
	// Development reads content/ and assets/ from disk on every request,
	// so an edit shows on reload, and shows every part of a page, made-up
	// numbers too, marked: APP_ENV=development. The image sets
	// APP_ENV=production.
	Development bool
	// The Umami page counter, on the Umami server UmamiHost, such as
	// umami.example.com. Off without both.
	UmamiWebsiteID string
	UmamiHost      string
}

func Load() (*Config, error) {
	cfg := &Config{
		RenderConfig: RenderConfig{
			AppURL:         env("APP_URL", ""),
			UmamiWebsiteID: env("UMAMI_WEBSITE_ID", ""),
			UmamiHost:      env("UMAMI_HOST", ""),
		},
		Port:                env("PORT", "8090"),
		Database:            env("DATABASE_FILE", "data/cloud.db"),
		EmailFrom:           env("EMAIL_FROM", "noreply@example.com"),
		CloudflareAccountID: env("CLOUDFLARE_ACCOUNT_ID", ""),
		CloudflareAPIToken:  env("CLOUDFLARE_API_TOKEN", ""),
		CloudflareAPIURL:    env("CLOUDFLARE_API_URL", "https://api.cloudflare.com/client/v4"),
		GitHubClientID:      env("GITHUB_APP_CLIENT_ID", ""),
		GitHubClientSecret:  env("GITHUB_APP_CLIENT_SECRET", ""),
		GitHubURL:           env("GITHUB_URL", "https://github.com"),
		GitHubAPIURL:        env("GITHUB_API_URL", "https://api.github.com"),
		StripeSecretKey:     env("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: env("STRIPE_WEBHOOK_SECRET", ""),
		StripeProduct:       env("STRIPE_PRODUCT_ID_PRO", ""),
		StripePriceMonthly:  env("STRIPE_PRICE_ID_PRO_MONTHLY", ""),
		StripePriceYearly:   env("STRIPE_PRICE_ID_PRO_YEARLY", ""),
		StripeAPIURL:        env("STRIPE_API_URL", "https://api.stripe.com"),
		CopiesEndpoint:      env("COPIES_ENDPOINT", ""),
		CopiesBucket:        env("COPIES_BUCKET", ""),
		CopiesAccessKey:     env("COPIES_ACCESS_KEY", ""),
		CopiesSecretKey:     env("COPIES_SECRET_KEY", ""),
	}
	var errs []error
	switch env("APP_ENV", "") {
	case "development":
		cfg.Development = true
	case "production":
	default:
		errs = append(errs, errors.New(`APP_ENV must be "development" or "production"`))
	}
	if !absoluteURL(cfg.AppURL) {
		errs = append(errs, errors.New("APP_URL must be an absolute HTTP or HTTPS URL"))
	}
	for _, old := range strings.Split(env("OLD_URLS", ""), ",") {
		old = strings.TrimRight(strings.TrimSpace(old), "/")
		if old == "" {
			continue
		}
		if !absoluteURL(old) {
			errs = append(errs, fmt.Errorf("OLD_URLS: %q is no absolute HTTP or HTTPS URL", old))
		}
		cfg.OldURLs = append(cfg.OldURLs, old)
	}
	if (cfg.CloudflareAccountID == "") != (cfg.CloudflareAPIToken == "") {
		errs = append(errs, errors.New("mail needs both CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN, or neither for mail in the log"))
	}
	if (cfg.GitHubClientID == "") != (cfg.GitHubClientSecret == "") {
		errs = append(errs, errors.New("GitHub sign-in needs both GITHUB_APP_CLIENT_ID and GITHUB_APP_CLIENT_SECRET, or neither"))
	}
	stripe := []string{cfg.StripeSecretKey, cfg.StripeWebhookSecret, cfg.StripeProduct, cfg.StripePriceMonthly, cfg.StripePriceYearly}
	if slices.Contains(stripe, "") && slices.ContainsFunc(stripe, func(value string) bool { return value != "" }) {
		errs = append(errs, errors.New("billing needs all of STRIPE_SECRET_KEY, STRIPE_WEBHOOK_SECRET, STRIPE_PRODUCT_ID_PRO, STRIPE_PRICE_ID_PRO_MONTHLY and STRIPE_PRICE_ID_PRO_YEARLY, or none"))
	}
	bucket := []string{cfg.CopiesEndpoint, cfg.CopiesBucket, cfg.CopiesAccessKey, cfg.CopiesSecretKey}
	if slices.Contains(bucket, "") && slices.ContainsFunc(bucket, func(value string) bool { return value != "" }) {
		errs = append(errs, errors.New("copies need all of COPIES_ENDPOINT, COPIES_BUCKET, COPIES_ACCESS_KEY and COPIES_SECRET_KEY, or none"))
	}
	if cfg.CopiesEndpoint != "" && !absoluteURL(cfg.CopiesEndpoint) {
		errs = append(errs, errors.New("COPIES_ENDPOINT must be an absolute HTTP or HTTPS URL"))
	}
	if knocks := env("KNOCKS_SERVER", ""); knocks != "" {
		var found bool
		cfg.KnocksWorkspace, cfg.KnocksServer, found = strings.Cut(knocks, "/")
		if !found || cfg.KnocksWorkspace == "" || cfg.KnocksServer == "" {
			errs = append(errs, errors.New("KNOCKS_SERVER must be a workspace id and a server name, like 3f9c/web-1"))
		}
	}
	if port, err := strconv.Atoi(cfg.Port); err != nil || port < 1 || port > 65535 {
		errs = append(errs, errors.New("PORT must be a number between 1 and 65535"))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func absoluteURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
