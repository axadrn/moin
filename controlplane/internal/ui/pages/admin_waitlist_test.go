package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
)

func TestAdminWaitlistShowsThePlanOfEveryoneWhoIsIn(t *testing.T) {
	in := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	trialEnds := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	entries := []model.WaitlistEntry{
		{Email: "waits@example.com", CreatedAt: in},
		{Email: "trial@example.com", CreatedAt: in, LetInAt: &in, Plan: model.WaitlistTrial, TrialEndsAt: &trialEnds},
		{Email: "free@example.com", CreatedAt: in, LetInAt: &in, ForeverFree: true, Plan: model.WaitlistFreeForever},
		{Email: "payer@example.com", CreatedAt: in, LetInAt: &in, Plan: model.WaitlistPaying},
	}
	var page bytes.Buffer
	if err := AdminWaitlist(entries).Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	html := page.String()
	for _, want := range []string{"trial until 2026-12-02", "free forever", "paying", "Make free forever", "End free forever", "/admin/waitlist/forever-free"} {
		if !strings.Contains(html, want) {
			t.Errorf("the page does not show %q", want)
		}
	}
}
