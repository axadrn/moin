package payment

import (
	"github.com/axadrn/ops/controlplane/internal/config"
	"github.com/axadrn/ops/controlplane/internal/service"
)

func NewProvider(cfg *config.Config, subscriptions *service.SubscriptionService) (Provider, error) {
	return NewStripeProvider(cfg, subscriptions), nil
}
