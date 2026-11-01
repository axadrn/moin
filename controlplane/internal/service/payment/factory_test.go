package payment

import (
	"testing"

	"github.com/axadrn/ops/controlplane/internal/config"
	"github.com/axadrn/ops/controlplane/internal/model"
)

func TestNewProviderBuildsSelectedProvider(t *testing.T) {
	cfg := &config.Config{
		StripeSecretKey:     "stripe-secret",
		StripeWebhookSecret: "webhook-secret",
	}
	provider, err := NewProvider(cfg, nil)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider.Name() != model.ProviderStripe {
		t.Fatalf("provider name = %q, want %q", provider.Name(), model.ProviderStripe)
	}
}
