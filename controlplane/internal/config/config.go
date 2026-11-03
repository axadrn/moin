package config

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/axadrn/ops/internal/client"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
	GoogleOAuthEnabled                 = false
)

type Config struct {
	// Application
	AppEnv        Environment
	AppURL        string
	Port          string
	SupportEmail  string
	AdminEmails   []string
	RobotsNoIndex bool

	// Database
	DBConnection string

	// Security
	SessionExpiry            time.Duration
	TokenEmailVerifyExpiry   time.Duration
	TokenPasswordResetExpiry time.Duration
	TokenEmailChangeExpiry   time.Duration
	TokenMagicLinkExpiry     time.Duration
	TrustedProxyCIDRs        []netip.Prefix

	// OAuth
	APIURL             string
	GitHubClientID     string
	GitHubClientSecret string

	// The ops GitHub App, which connects repos. Without an app ID the
	// control plane runs without it.
	GitHubAppID            string
	GitHubAppSlug          string
	GitHubAppPrivateKey    string
	GitHubAppWebhookSecret string
	// The app's client ID and secret check who installed it.
	GitHubAppClientID     string
	GitHubAppClientSecret string
	GitHubAPIURL          string
	GitHubURL             string

	// CloudflareAPIURL is where tunnels are made with the users' own
	// Cloudflare tokens; tests point it at a fake.
	CloudflareAPIURL string

	// Email through Cloudflare Email Sending. Without credentials in
	// development, mail is written to the log instead.
	EmailFrom           string
	CloudflareAccountID string
	CloudflareAPIToken  string

	// SecretsKey seals app env vars: 32 bytes, base64. Development falls
	// back to a fixed key.
	SecretsKey []byte
	// Billing off runs a self-hosted control plane: no Stripe, no prices,
	// every workspace gets everything.
	Billing bool
	// SignupsOpen false closes sign-up: new accounts only for admins and
	// invited people. A self-hosted control plane is closed by default.
	SignupsOpen bool
	// BackupDatabase sends every change of the control plane's own SQLite
	// database to its bucket and restores it on start when it is missing.
	// On by default in production.
	BackupDatabase bool
	// Telemetry has a self-hosted control plane send a few anonymous counts
	// to TelemetryURL every five minutes. The hosted one never sends.
	Telemetry    bool
	TelemetryURL string
	// UptimeChecks has the control plane check every domain of every live
	// app once a minute. On by default in production, so development and
	// tests never call real domains.
	UptimeChecks bool
	// Payment, Stripe
	StripeSecretKey         string
	StripeWebhookSecret     string
	StripePriceIDProMonthly string
	StripePriceIDProYearly  string
	// Analytics
	UmamiWebsiteID    string
	UmamiHost         string
	PlausibleDomain   string
	PlausibleHost     string
	GoogleAnalyticsID string

	// Logging
	LogHTTP bool

	// JSON API
	CORSAllowedOrigins []string

	// Storage
	S3Region               string
	S3Bucket               string
	S3AccessKey            string
	S3SecretKey            string
	S3Endpoint             string
	S3PresignExpiryPublic  time.Duration
	S3PresignExpiryPrivate time.Duration
}

type RenderConfig struct {
	AppEnv            Environment
	AppURL            string
	SupportEmail      string
	RobotsNoIndex     bool
	UmamiWebsiteID    string
	UmamiHost         string
	PlausibleDomain   string
	PlausibleHost     string
	GoogleAnalyticsID string
	Billing           bool
	SignupsOpen       bool
	// GitHubLogin shows "Continue with GitHub": only with a client ID and
	// secret, so the button never leads to a broken GitHub page.
	GitHubLogin bool
}

func Load() (*Config, error) {
	r := envReader{}
	appEnv := Environment(r.string("APP_ENV", ""))
	s3PresignExpiryPublic := 168 * time.Hour
	s3PresignExpiryPrivate := time.Hour
	s3PresignExpiryPublic = r.duration("S3_PRESIGN_EXPIRY_PUBLIC", s3PresignExpiryPublic)
	s3PresignExpiryPrivate = r.duration("S3_PRESIGN_EXPIRY_PRIVATE", s3PresignExpiryPrivate)

	cfg := &Config{
		AppEnv:       appEnv,
		AppURL:       r.string("APP_URL", ""),
		Port:         r.string("PORT", "8090"),
		SupportEmail: r.string("SUPPORT_EMAIL", "hello@example.com"),
		AdminEmails:  r.emails("ADMIN_EMAILS"),
		DBConnection: r.string("DB_CONNECTION", "./data/acme.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"),

		SessionExpiry:            r.duration("SESSION_EXPIRY", 168*time.Hour),
		TokenEmailVerifyExpiry:   r.duration("TOKEN_EMAIL_VERIFY_EXPIRY", 24*time.Hour),
		TokenPasswordResetExpiry: r.duration("TOKEN_PASSWORD_RESET_EXPIRY", time.Hour),
		TokenEmailChangeExpiry:   r.duration("TOKEN_EMAIL_CHANGE_EXPIRY", 24*time.Hour),
		TokenMagicLinkExpiry:     r.duration("TOKEN_MAGIC_LINK_EXPIRY", 10*time.Minute),
		TrustedProxyCIDRs: r.prefixes("TRUSTED_PROXY_CIDRS", []netip.Prefix{
			netip.MustParsePrefix("127.0.0.0/8"),
			netip.MustParsePrefix("::1/128"),
		}),

		EmailFrom:               r.string("EMAIL_FROM", "noreply@example.com"),
		CloudflareAccountID:     r.string("CLOUDFLARE_ACCOUNT_ID", ""),
		CloudflareAPIToken:      r.string("CLOUDFLARE_API_TOKEN", ""),
		Billing:                 r.boolean("BILLING", true),
		Telemetry:               r.boolean("TELEMETRY", true),
		TelemetryURL:            r.string("TELEMETRY_URL", client.DefaultURL+"/webhooks/telemetry"),
		StripeSecretKey:         r.string("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:     r.string("STRIPE_WEBHOOK_SECRET", ""),
		StripePriceIDProMonthly: r.string("STRIPE_PRICE_ID_PRO_MONTHLY", ""),
		StripePriceIDProYearly:  r.string("STRIPE_PRICE_ID_PRO_YEARLY", ""),
		UmamiWebsiteID:          r.string("UMAMI_WEBSITE_ID", ""),
		UmamiHost:               r.string("UMAMI_HOST", "cloud.umami.is"),
		PlausibleDomain:         r.string("PLAUSIBLE_DOMAIN", ""),
		PlausibleHost:           r.string("PLAUSIBLE_HOST", "plausible.io"),
		GoogleAnalyticsID:       r.string("GOOGLE_ANALYTICS_ID", ""),

		LogHTTP: r.boolean("LOG_HTTP", true),

		CORSAllowedOrigins:     r.origins("CORS_ALLOWED_ORIGINS"),
		S3Region:               r.string("S3_REGION", ""),
		S3Bucket:               r.string("S3_BUCKET", ""),
		S3AccessKey:            r.string("S3_ACCESS_KEY", ""),
		S3SecretKey:            r.string("S3_SECRET_KEY", ""),
		S3Endpoint:             r.string("S3_ENDPOINT", ""),
		S3PresignExpiryPublic:  s3PresignExpiryPublic,
		S3PresignExpiryPrivate: s3PresignExpiryPrivate,
	}
	// A self-hosted control plane is private: closed sign-up and no search
	// engines, unless its env says otherwise.
	cfg.SignupsOpen = r.boolean("SIGNUPS_OPEN", cfg.Billing)
	cfg.RobotsNoIndex = r.boolean("ROBOTS_NOINDEX", !cfg.Billing)
	cfg.BackupDatabase = r.boolean("BACKUP_DATABASE", cfg.IsProduction())
	cfg.UptimeChecks = r.boolean("UPTIME_CHECKS", cfg.IsProduction())
	cfg.APIURL = r.string("API_URL", cfg.AppURL)
	cfg.GitHubClientID = r.string("GITHUB_CLIENT_ID", "")
	cfg.GitHubClientSecret = r.string("GITHUB_CLIENT_SECRET", "")
	cfg.GitHubAppID = r.string("GITHUB_APP_ID", "")
	cfg.GitHubAppSlug = r.string("GITHUB_APP_SLUG", "")
	cfg.GitHubAppPrivateKey = r.string("GITHUB_APP_PRIVATE_KEY", "")
	// A key with many lines reads better from a file than from an env var.
	if file := r.string("GITHUB_APP_PRIVATE_KEY_FILE", ""); file != "" && cfg.GitHubAppPrivateKey == "" {
		key, err := os.ReadFile(file)
		if err != nil {
			r.errors = append(r.errors, fmt.Errorf("GITHUB_APP_PRIVATE_KEY_FILE: %w", err))
		}
		cfg.GitHubAppPrivateKey = string(key)
	}
	cfg.GitHubAppWebhookSecret = r.string("GITHUB_APP_WEBHOOK_SECRET", "")
	cfg.GitHubAppClientID = r.string("GITHUB_APP_CLIENT_ID", "")
	cfg.GitHubAppClientSecret = r.string("GITHUB_APP_CLIENT_SECRET", "")
	cfg.GitHubAPIURL = r.string("GITHUB_API_URL", "https://api.github.com")
	cfg.GitHubURL = r.string("GITHUB_URL", "https://github.com")
	cfg.CloudflareAPIURL = r.string("CLOUDFLARE_API_URL", "https://api.cloudflare.com/client/v4")
	cfg.SecretsKey = secretsKey(r.string("SECRETS_KEY", ""), cfg.IsDevelopment())

	validationErrors := append(r.errors, cfg.validate()...)
	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(validationErrors...))
	}

	return cfg, nil
}

// secretsKey decodes SECRETS_KEY. Development without one gets a fixed key,
// so env vars survive restarts; it protects nothing.
func secretsKey(raw string, development bool) []byte {
	if raw == "" && development {
		key := sha256.Sum256([]byte("ops development secrets key"))
		return key[:]
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil
	}
	return key
}

func (c *Config) validate() []error {
	var errs []error

	if c.AppEnv != EnvironmentDevelopment && c.AppEnv != EnvironmentProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q", EnvironmentDevelopment, EnvironmentProduction))
	}
	if err := validateAppURL(c.AppURL); err != nil {
		errs = append(errs, err)
	}
	if err := validatePort(c.Port); err != nil {
		errs = append(errs, err)
	}
	if c.DBConnection == "" {
		errs = append(errs, errors.New("DB_CONNECTION is required"))
	}
	if c.SessionExpiry <= 0 {
		errs = append(errs, errors.New("SESSION_EXPIRY must be greater than zero"))
	}
	if c.APIURL != c.AppURL {
		apiURLErr := validateAPIURL(c.APIURL)
		if apiURLErr != nil {
			errs = append(errs, apiURLErr)
		}
	}
	errOAuthPair := func(provider, clientID, clientSecret string) {
		if (clientID == "") != (clientSecret == "") {
			errs = append(errs, fmt.Errorf("%s OAuth requires both client ID and client secret", provider))
		}
	}
	errOAuthPair("GitHub", c.GitHubClientID, c.GitHubClientSecret)

	if c.EmailFrom == "" {
		errs = append(errs, errors.New("EMAIL_FROM is required"))
	} else if _, err := mail.ParseAddress(c.EmailFrom); err != nil {
		errs = append(errs, errors.New("EMAIL_FROM must be a valid email address"))
	}
	if (c.CloudflareAccountID == "") != (c.CloudflareAPIToken == "") {
		errs = append(errs, errors.New("email delivery requires both CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN"))
	} else if c.CloudflareAccountID == "" && !c.IsDevelopment() {
		errs = append(errs, errors.New("CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN are required for email delivery outside development"))
	}

	if c.Billing {
		errs = append(errs, c.validatePayments()...)
	}
	if len(c.SecretsKey) != 32 {
		errs = append(errs, errors.New("SECRETS_KEY must be 32 bytes in base64, for example from `openssl rand -base64 32`"))
	}
	{
		required := []struct {
			key   string
			value string
		}{
			{key: "S3_REGION", value: c.S3Region},
			{key: "S3_BUCKET", value: c.S3Bucket},
		}
		for _, field := range required {
			if field.value == "" {
				errs = append(errs, fmt.Errorf("%s is required when storage is included", field.key))
			}
		}
		if (c.S3AccessKey == "") != (c.S3SecretKey == "") {
			errs = append(errs, errors.New("storage credentials require both S3_ACCESS_KEY and S3_SECRET_KEY"))
		}
	}

	return errs
}

func (c *Config) validatePayments() []error {
	var errs []error
	require := func(key, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s is required for the selected payment provider", key))
		}
	}
	require("STRIPE_SECRET_KEY", c.StripeSecretKey)
	require("STRIPE_WEBHOOK_SECRET", c.StripeWebhookSecret)
	if c.IsProduction() {
		require("STRIPE_PRICE_ID_PRO_MONTHLY", c.StripePriceIDProMonthly)
		require("STRIPE_PRICE_ID_PRO_YEARLY", c.StripePriceIDProYearly)
	}

	return errs
}

func validateAppURL(value string) error {
	if value == "" {
		return errors.New("APP_URL is required")
	}
	return validateAbsoluteHTTPURL("APP_URL", value)
}

// validateAPIURL checks an explicit API_URL, the public origin of this server.
// OAuth providers return to API_URL/auth/{provider}/callback, so it carries no path.
func validateAPIURL(value string) error {
	err := validateAbsoluteHTTPURL("API_URL", value)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return errors.New("API_URL must be an origin without a path, for example https://api.example.com")
	}
	return nil
}

func validateAbsoluteHTTPURL(key, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", key)
	}
	return nil
}

func validatePort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("PORT must be a number between 1 and 65535")
	}
	return nil
}

type envReader struct {
	errors []error
}

func (r *envReader) string(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func (r *envReader) boolean(key string, fallback bool) bool {
	value := r.string(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		r.errors = append(r.errors, fmt.Errorf("%s must be a boolean: %w", key, err))
		return fallback
	}
	return parsed
}

func (r *envReader) duration(key string, fallback time.Duration) time.Duration {
	value := r.string(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		r.errors = append(r.errors, fmt.Errorf("%s must be a Go duration: %w", key, err))
		return fallback
	}
	return parsed
}

func (r *envReader) integer(key string, fallback int) int {
	value := r.string(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		r.errors = append(r.errors, fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return parsed
}

func (r *envReader) prefixes(key string, fallback []netip.Prefix) []netip.Prefix {
	prefixes := append([]netip.Prefix(nil), fallback...)
	value := r.string(key, "")
	if value == "" {
		return prefixes
	}

	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		prefix, err := netip.ParsePrefix(candidate)
		if err != nil {
			r.errors = append(r.errors, fmt.Errorf("%s must contain comma-separated IP CIDRs: %q", key, candidate))
			continue
		}
		prefixes = append(prefixes, prefix.Masked())
	}

	return prefixes
}

func (r *envReader) emails(key string) []string {
	value := r.string(key, "")
	if value == "" {
		return nil
	}

	emails := make([]string, 0)
	seen := make(map[string]struct{})
	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		address, err := mail.ParseAddress(candidate)
		if err != nil || !strings.EqualFold(address.Address, candidate) {
			r.errors = append(r.errors, fmt.Errorf("%s must contain comma-separated email addresses: %q", key, candidate))
			continue
		}
		normalized := strings.ToLower(address.Address)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		emails = append(emails, normalized)
	}
	return emails
}

func (c *Config) IsDevelopment() bool {
	return c.AppEnv == EnvironmentDevelopment
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == EnvironmentProduction
}

func (c *Config) Render() RenderConfig {
	return RenderConfig{
		AppEnv:            c.AppEnv,
		AppURL:            c.AppURL,
		SupportEmail:      c.SupportEmail,
		RobotsNoIndex:     c.RobotsNoIndex,
		UmamiWebsiteID:    c.UmamiWebsiteID,
		UmamiHost:         c.UmamiHost,
		PlausibleDomain:   c.PlausibleDomain,
		PlausibleHost:     c.PlausibleHost,
		GoogleAnalyticsID: c.GoogleAnalyticsID,
		Billing:           c.Billing,
		SignupsOpen:       c.SignupsOpen,
		GitHubLogin:       c.GitHubClientID != "" && c.GitHubClientSecret != "",
	}
}

func (c RenderConfig) IsProduction() bool {
	return c.AppEnv == EnvironmentProduction
}
