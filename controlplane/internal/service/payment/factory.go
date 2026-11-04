package payment

import (
	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/service"
)

func NewProvider(cfg *config.Config, subscriptions *service.SubscriptionService) (Provider, error) {
	return NewStripeProvider(cfg, subscriptions), nil
}
