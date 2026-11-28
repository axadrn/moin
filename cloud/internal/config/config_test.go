package config

import (
	"slices"
	"testing"
)

func TestMissing(t *testing.T) {
	for _, key := range Needed {
		t.Setenv(key, "set")
	}
	if missing := Missing(); len(missing) != 0 {
		t.Errorf("Missing() = %v, want none", missing)
	}
	t.Setenv("STRIPE_PRODUCT_ID_PRO", " ")
	if missing := Missing(); !slices.Equal(missing, []string{"STRIPE_PRODUCT_ID_PRO"}) {
		t.Errorf("Missing() = %v, want STRIPE_PRODUCT_ID_PRO", missing)
	}
}

// moin.run needs a real SECRETS_KEY; development has a test key.
func TestSecretsKey(t *testing.T) {
	t.Setenv("APP_URL", "https://cloud.example.com")
	t.Setenv("APP_ENV", "development")
	t.Setenv("SECRETS_KEY", "")
	if cfg, err := Load(); err != nil || cfg.SecretsKey == [32]byte{} {
		t.Errorf("development without a key: %v", err)
	}
	t.Setenv("APP_ENV", "production")
	for _, bad := range []string{"", "not base64", "c2hvcnQ="} {
		t.Setenv("SECRETS_KEY", bad)
		if _, err := Load(); err == nil {
			t.Errorf("SECRETS_KEY=%q was taken", bad)
		}
	}
	t.Setenv("SECRETS_KEY", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	if cfg, err := Load(); err != nil || cfg.SecretsKey[0] != 1 {
		t.Errorf("a good key: %v", err)
	}
}
