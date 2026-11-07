package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/internal/build"
)

func TestEnvSetSummary(t *testing.T) {
	cases := []struct {
		set, removed int
		missing      []string
		want         string
	}{
		{1, 0, nil, "Set 1 env var. Restart the app to apply it: restart_app."},
		{3, 0, nil, "Set 3 env vars. Restart the app to apply them: restart_app."},
		{0, 1, nil, "Removed 1 env var. Restart the app to apply it: restart_app."},
		{2, 1, nil, "Set 2 env vars and removed 1 env var. Restart the app to apply them: restart_app."},
		{0, 0, nil, "Nothing changed."},
		{0, 0, []string{"NOT_THERE"}, "Nothing changed. Not set, so not removed: NOT_THERE."},
		{0, 1, []string{"NOT_THERE"}, "Removed 1 env var. Not set, so not removed: NOT_THERE. Restart the app to apply it: restart_app."},
	}
	for _, c := range cases {
		if got := (EnvSet{set: c.set, removed: c.removed, missing: c.missing}).Summary(); got != c.want {
			t.Errorf("set %d removed %d missing %v: %q, want %q", c.set, c.removed, c.missing, got, c.want)
		}
	}
}

func TestSummariesReadWell(t *testing.T) {
	one := ServerList{api.ServerList{Items: []api.Server{{Name: "web-1", Online: true}}}}
	for got, want := range map[string]string{
		one.Summary(): "1 server, 1 online.",
		(Deploy{api.Deploy{Number: 1, Status: "succeeded"}}).Summary():                                                             "Deploy v1 succeeded.",
		(Deploy{api.Deploy{Number: 2, Status: "running"}}).Summary():                                                               "Deploy v2 is running.",
		(Logs{api.Logs{Lines: []string{"listening"}}}).Summary():                                                                   "1 log line.",
		(LinkedApp{api.App{Name: "shop", Repo: "me/shop", Branch: "main"}}).Summary():                                              "shop follows me/shop on main. Deploy it with deploy_ref.",
		(LinkedApp{api.App{Name: "shop", Repo: "me/shop", Branch: "main", AutoDeploy: true}}).Summary():                            "shop follows me/shop on main. Every push to main deploys it; deploy_ref deploys it now.",
		(DomainChecks{api.DomainChecks{Items: []api.DomainCheck{{Domain: "shop.de", Points: true, FromCompose: true}}}}).Summary(): "shop.de points at the server. x-moin.domains in compose.yml sets these domains: change them there and deploy, set_domains cannot.",
	} {
		if got != want {
			t.Errorf("%q, want %q", got, want)
		}
	}
}

// TestActivitySaysHowLikeTheWeb: list_activity names how a change came in
// as the web and the CLI do.
func TestActivitySaysHowLikeTheWeb(t *testing.T) {
	at := time.Date(2026, 11, 3, 14, 2, 0, 0, time.UTC)
	activity := Activity{api.ActivityPage{Items: []api.WorkspaceEvent{
		{Actor: "ada@example.com", Summary: "deployed shop v3", Via: "cli", Token: "ci", CreatedAt: at},
		{Actor: "moin", Summary: "found shop.de of shop down: HTTP 502", Via: "system", CreatedAt: at},
	}}}
	want := "2 events, newest first. This is the last page.\n" +
		"2026-11-03T14:02:00Z ada@example.com deployed shop v3 (CLI, token ci).\n" +
		"2026-11-03T14:02:00Z moin found shop.de of shop down: HTTP 502 (" + build.Name + ")."
	if got := activity.Summary(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestGetBillingWithBillingOff(t *testing.T) {
	_, err := builtinTools{services: Services{BillingOff: true}}.getBilling(context.Background(), Caller{}, WorkspaceInput{})
	if err == nil || !strings.Contains(err.Error(), "Billing is off") {
		t.Errorf("err = %v", err)
	}
}
