package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// TestBillingSaysWhenTheTrialStarts: a trial that waits says it starts
// with the first server, a running one says when it ends.
func TestBillingSaysWhenTheTrialStarts(t *testing.T) {
	waiting := &model.Subscription{PlanID: model.SubscriptionPlanPro, Status: model.SubscriptionStatusTrialing}
	if got := billingLine(waiting); got != "30 days free, starting when you connect your first server. No card." {
		t.Errorf("waiting trial: %q", got)
	}
	ends := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	running := &model.Subscription{PlanID: model.SubscriptionPlanPro, Status: model.SubscriptionStatusTrialing, CurrentPeriodEnd: &ends}
	if got := billingLine(running); !strings.HasPrefix(got, "Everything works until Dec 25, 2026.") {
		t.Errorf("running trial: %q", got)
	}
}
