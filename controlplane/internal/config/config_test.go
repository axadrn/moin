package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAllowsMissingMailCredentialsOnlyInDevelopment(t *testing.T) {
	setCoreEnvironment(t)
	_, err := Load()
	if err != nil {
		t.Fatalf("development without Cloudflare credentials: Load() error = %v", err)
	}

	t.Setenv("APP_ENV", "production")
	_, err = Load()
	if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_ACCOUNT_ID") {
		t.Fatalf("production without Cloudflare credentials: Load() error = %v", err)
	}
}

// TestSelfHostedStartsWithoutEmailAndBucket: one command installs a
// self-hosted control plane before its email and bucket exist. It starts,
// keeps no backup of itself, and sends mail to the log; a bucket with
// BACKUP_DATABASE=true needs its settings again.
func TestSelfHostedStartsWithoutEmailAndBucket(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("BILLING", "false")
	t.Setenv("SECRETS_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("S3_REGION", "")
	t.Setenv("S3_BUCKET", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BackupDatabase {
		t.Error("BackupDatabase without a bucket")
	}
	t.Setenv("BACKUP_DATABASE", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "S3_BUCKET") {
		t.Errorf("backup without a bucket: Load() error = %v", err)
	}
}

func TestLoadDefaultsSelectedDatabaseConnection(t *testing.T) {
	setCoreEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.Contains(cfg.DBConnection, "_pragma=busy_timeout(5000)") {
		t.Fatalf("DBConnection = %q, want SQLite busy timeout", cfg.DBConnection)
	}
}

func TestLoadAdminEmails(t *testing.T) {
	t.Run("normalizes and deduplicates", func(t *testing.T) {
		setCoreEnvironment(t)
		t.Setenv("ADMIN_EMAILS", "Admin@Example.com, support@example.com, admin@example.com")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(cfg.AdminEmails) != 2 || cfg.AdminEmails[0] != "admin@example.com" || cfg.AdminEmails[1] != "support@example.com" {
			t.Fatalf("AdminEmails = %#v", cfg.AdminEmails)
		}
	})

	t.Run("rejects invalid address", func(t *testing.T) {
		setCoreEnvironment(t)
		t.Setenv("ADMIN_EMAILS", "admin@example.com, not-an-email")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "ADMIN_EMAILS") {
			t.Fatalf("Load() error = %v, want ADMIN_EMAILS validation error", err)
		}
	})
}

func TestLoadValidatesGeneratedMailProvider(t *testing.T) {
	t.Run("Cloudflare credentials come in pairs", func(t *testing.T) {
		setCoreEnvironment(t)
		t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_API_TOKEN") {
			t.Fatalf("Load() error = %v, want the missing token", err)
		}
	})
	t.Run("sender address", func(t *testing.T) {
		setCoreEnvironment(t)
		t.Setenv("EMAIL_FROM", "not an email address")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "EMAIL_FROM") {
			t.Fatalf("Load() error = %v, want sender validation error", err)
		}
	})
}
func TestLoadRequiresStorageConfigurationWhenIncluded(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("S3_REGION", "")
	t.Setenv("S3_BUCKET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want storage validation error")
	}
	for _, key := range []string{"S3_REGION", "S3_BUCKET"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("Load() error = %q, want %s", err, key)
		}
	}
}

func TestLoadRejectsPartialStorageCredentials(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("S3_REGION", "auto")
	t.Setenv("S3_BUCKET", "acme-dev")
	t.Setenv("S3_ACCESS_KEY", "access-key")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "S3_SECRET_KEY") {
		t.Fatalf("Load() error = %v, want incomplete storage credentials error", err)
	}
}

func TestLoadRequiresSelectedPaymentProviderCredentials(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want payment credential validation error")
	}
	if !strings.Contains(err.Error(), "STRIPE_SECRET_KEY") || !strings.Contains(err.Error(), "STRIPE_WEBHOOK_SECRET") {
		t.Fatalf("Load() error = %q, want missing Stripe credentials", err)
	}
	if strings.Contains(err.Error(), "POLAR_") {
		t.Fatalf("Load() error = %q, contains unused Polar configuration", err)
	}
}

func TestLoadRejectsInvalidTypedValues(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("LOG_HTTP", "sometimes")
	t.Setenv("SESSION_EXPIRY", "0s")
	t.Setenv("TRUSTED_PROXY_CIDRS", "not-a-cidr")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want typed value validation error")
	}
	for _, value := range []string{"APP_ENV", "LOG_HTTP", "SESSION_EXPIRY", "TRUSTED_PROXY_CIDRS"} {
		if !strings.Contains(err.Error(), value) {
			t.Errorf("Load() error = %q, want %s", err, value)
		}
	}
}

func TestLoadParsesTrustedProxyCIDRsWithLoopbackDefaults(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 172.16.0.0/12")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, address := range []string{"127.0.0.1", "::1", "10.20.30.40", "172.20.0.1"} {
		parsed := netip.MustParseAddr(address)
		var trusted bool
		for _, prefix := range cfg.TrustedProxyCIDRs {
			if prefix.Contains(parsed) {
				trusted = true
				break
			}
		}
		if !trusted {
			t.Errorf("trusted proxy CIDRs do not contain %s: %v", address, cfg.TrustedProxyCIDRs)
		}
	}
}

func TestLoadDefaultsAPIURLToAppURL(t *testing.T) {
	setCoreEnvironment(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIURL != cfg.AppURL {
		t.Fatalf("APIURL = %q, want APP_URL %q", cfg.APIURL, cfg.AppURL)
	}
}

func TestLoadReadsAPIURL(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("APP_URL", "https://app.example.com")
	t.Setenv("API_URL", "https://api.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.APIURL != "https://api.example.com" || cfg.AppURL != "https://app.example.com" {
		t.Fatalf("APIURL = %q, AppURL = %q", cfg.APIURL, cfg.AppURL)
	}
}

func TestLoadRejectsInvalidAPIURL(t *testing.T) {
	for _, value := range []string{
		"api.example.com",
		"ftp://api.example.com",
		"https://api.example.com/",
		"https://api.example.com/v1",
		"https://api.example.com?x=1",
		"https://user@api.example.com",
	} {
		t.Run(value, func(t *testing.T) {
			setCoreEnvironment(t)
			t.Setenv("API_URL", value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "API_URL") {
				t.Fatalf("Load() error = %v, want API_URL error", err)
			}
		})
	}
}

func TestRenderIncludesOnlyPublicValues(t *testing.T) {
	cfg := &Config{
		AppEnv:            EnvironmentProduction,
		AppURL:            "https://app.example.com",
		SupportEmail:      "support@example.com",
		RobotsNoIndex:     true,
		UmamiWebsiteID:    "website-id",
		UmamiHost:         "analytics.example.com",
		PlausibleDomain:   "app.example.com",
		PlausibleHost:     "plausible.example.com",
		GoogleAnalyticsID: "G-EXAMPLE",
	}

	render := cfg.Render()
	if render.AppEnv != cfg.AppEnv || render.AppURL != cfg.AppURL || render.SupportEmail != cfg.SupportEmail {
		t.Fatalf("Render() core values = %#v", render)
	}
	if !render.RobotsNoIndex || render.UmamiWebsiteID != cfg.UmamiWebsiteID || render.UmamiHost != cfg.UmamiHost {
		t.Fatalf("Render() analytics values = %#v", render)
	}
	if render.PlausibleDomain != cfg.PlausibleDomain || render.PlausibleHost != cfg.PlausibleHost || render.GoogleAnalyticsID != cfg.GoogleAnalyticsID {
		t.Fatalf("Render() analytics values = %#v", render)
	}
	if !render.IsProduction() {
		t.Fatal("Render() lost the production environment")
	}
}

func setCoreEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ENV",
		"APP_URL",
		"API_URL",
		"ADMIN_EMAILS",
		"SESSION_EXPIRY",
		"TRUSTED_PROXY_CIDRS",
		"DB_CONNECTION",
		"LOG_LEVEL",
		"LOG_FORMAT",
		"LOG_HTTP",
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"GITHUB_CLIENT_ID",
		"GITHUB_CLIENT_SECRET",
		"EMAIL_FROM",
		"RESEND_API_KEY",
		"RESEND_AUDIENCE_ID",
		"CLOUDFLARE_ACCOUNT_ID",
		"CLOUDFLARE_API_TOKEN",
		"STRIPE_SECRET_KEY",
		"STRIPE_WEBHOOK_SECRET",
		"S3_REGION",
		"S3_BUCKET",
		"S3_ACCESS_KEY",
		"S3_SECRET_KEY",
		"S3_PRESIGN_EXPIRY_PUBLIC",
		"S3_PRESIGN_EXPIRY_PRIVATE",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_URL", "http://localhost:8090")
	t.Setenv("EMAIL_FROM", "noreply@example.com")
	t.Setenv("STRIPE_SECRET_KEY", "sk_test")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test")
	t.Setenv("S3_REGION", "auto")
	t.Setenv("S3_BUCKET", "acme-dev")
}

func TestLoadWithBillingOffNeedsNoStripe(t *testing.T) {
	setCoreEnvironment(t)
	t.Setenv("BILLING", "false")
	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Billing || cfg.Render().Billing {
		t.Fatal("billing is on")
	}
}

func TestGitHubAppKeyFromFile(t *testing.T) {
	setCoreEnvironment(t)
	file := filepath.Join(t.TempDir(), "app.pem")
	os.WriteFile(file, []byte("-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----\n"), 0o600)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY_FILE", file)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.GitHubAppPrivateKey, "\nabc\n") {
		t.Fatalf("key = %q", cfg.GitHubAppPrivateKey)
	}
}

func TestSelfHostedIsPrivateByDefault(t *testing.T) {
	for _, test := range []struct {
		billing, signupsOpen, noIndex string
		wantOpen, wantNoIndex         bool
	}{
		{billing: "true", wantOpen: true, wantNoIndex: false},
		{billing: "false", wantOpen: false, wantNoIndex: true},
		{billing: "false", signupsOpen: "true", noIndex: "false", wantOpen: true, wantNoIndex: false},
		{billing: "true", signupsOpen: "false", wantOpen: false, wantNoIndex: false},
	} {
		setCoreEnvironment(t)
		t.Setenv("BILLING", test.billing)
		t.Setenv("SIGNUPS_OPEN", test.signupsOpen)
		t.Setenv("ROBOTS_NOINDEX", test.noIndex)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.SignupsOpen != test.wantOpen || cfg.RobotsNoIndex != test.wantNoIndex {
			t.Fatalf("BILLING=%s SIGNUPS_OPEN=%q ROBOTS_NOINDEX=%q: open = %v, noindex = %v", test.billing, test.signupsOpen, test.noIndex, cfg.SignupsOpen, cfg.RobotsNoIndex)
		}
	}
}
