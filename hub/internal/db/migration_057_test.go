package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/pressly/goose/v3"
)

// TestMigration057LetsTheTrialWaitForTheFirstServer runs 057 over seven
// workspaces. The two trials without a server ever, one running and one
// run out, wait for the first server. A trial with a server, one whose
// server was removed, an offer, a paid plan and the free plan stay.
func TestMigration057LetsTheTrialWaitForTheFirstServer(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 56); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	future, past := now.AddDate(0, 0, 10), now.AddDate(0, 0, -10)
	rows := []struct {
		workspace, plan, status string
		ends                    *time.Time
		provider, coupon        *string
		wantWaits               bool
	}{
		{"running", "pro", "trialing", &future, nil, nil, true},
		{"run-out", "pro", "trialing", &past, nil, nil, true},
		{"server", "pro", "trialing", &future, nil, nil, false},
		{"removed", "pro", "trialing", &past, nil, nil, false},
		{"offer", "pro", "trialing", &future, nil, ptr("half"), false},
		{"paid", "pro", "active", &future, ptr("sub_1"), nil, false},
		{"free", "free", "active", nil, nil, nil, false},
	}
	for _, row := range rows {
		database.MustExec(`INSERT INTO workspaces (id, name, slug, created_at, updated_at) VALUES ($1, $1, $1, $2, $2)`, row.workspace, now)
		database.MustExec(`INSERT INTO subscriptions (id, workspace_id, plan_id, status, current_period_end, provider_subscription_id, coupon_id) VALUES ($1, $1, $2, $3, $4, $5, $6)`,
			row.workspace, row.plan, row.status, row.ends, row.provider, row.coupon)
	}
	database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('server-1', 'server', 'web-1', $1)`, now)
	database.MustExec(`INSERT INTO activity_events (id, workspace_id, action, target_type, target_id, created_at) VALUES ('event-1', 'removed', 'server.installed', 'server', 'web-1', $1)`, now)

	if err := goose.UpTo(database.DB, ".", 57); err != nil {
		t.Fatal(err)
	}

	for _, row := range rows {
		var ends *time.Time
		if err := database.Get(&ends, `SELECT current_period_end FROM subscriptions WHERE workspace_id = $1`, row.workspace); err != nil {
			t.Fatal(err)
		}
		if waits := ends == nil && row.status == "trialing"; waits != row.wantWaits {
			t.Errorf("%s: trial ends %v, want waiting %v", row.workspace, ends, row.wantWaits)
		}
		if !row.wantWaits && (ends == nil) != (row.ends == nil) {
			t.Errorf("%s: end changed to %v", row.workspace, ends)
		}
	}
}

func ptr(value string) *string { return &value }
