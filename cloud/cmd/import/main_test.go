package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// oldDatabase is a database of the old service with fake rows:
//
//   - paid: a subscription in Stripe, owned by Ada.
//   - backer: no subscription, but its owner claimed an offer of a free
//     year.
//   - coupon: a subscription row with a coupon only.
//   - free: nothing to import.
//   - second: a second paying workspace of Ada; her mail is taken.
//   - an offer of Eve, a free year, never claimed.
func oldDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "old.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	schema, err := os.ReadFile("testdata/old_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	rows := `
INSERT INTO users (id, email) VALUES ('u-ada', ' Ada@Example.com '), ('u-bob', 'bob@example.com'), ('u-cy', 'cy@example.com'), ('u-dan', 'dan@example.com');
INSERT INTO workspaces (id, name, slug, created_by, created_at) VALUES
	('w-paid', 'Paid', 'paid', 'u-ada', '2026-01-02 03:04:05'),
	('w-second', 'Second', 'second', 'u-ada', '2026-02-02 03:04:05'),
	('w-backer', 'Backer', 'backer', 'u-bob', '2026-03-02 03:04:05'),
	('w-coupon', 'Coupon', 'coupon', 'u-cy', '2026-04-02 03:04:05'),
	('w-free', 'Free', 'free', 'u-dan', '2026-05-02 03:04:05');
INSERT INTO workspace_members (id, workspace_id, user_id, role) VALUES
	('m1', 'w-paid', 'u-ada', 'owner'), ('m2', 'w-second', 'u-ada', 'owner'), ('m3', 'w-backer', 'u-bob', 'owner'),
	('m4', 'w-coupon', 'u-cy', 'owner'), ('m5', 'w-free', 'u-dan', 'owner'), ('m6', 'w-paid', 'u-dan', 'member');
INSERT INTO subscriptions (id, workspace_id, plan_id, status, provider_customer_id, provider_subscription_id, current_period_end, interval, coupon_id) VALUES
	('s1', 'w-paid', 'pro', 'active', 'cus_ada', 'sub_ada', '2027-01-02 00:00:00.123+00:00', 'year', NULL),
	('s2', 'w-second', 'pro', 'canceled', 'cus_ada2', 'sub_ada2', '2026-01-02 00:00:00', 'month', NULL),
	('s3', 'w-coupon', 'pro', 'trialing', NULL, NULL, NULL, NULL, 'BACKER'),
	('s4', 'w-free', 'free', 'active', NULL, NULL, NULL, NULL, NULL);
INSERT INTO offers (email, trial_days, coupon_id, created_at, claimed_at, claimed_workspace_id) VALUES
	('bob@example.com', 365, 'EARLY', '2026-03-01 00:00:00', '2026-03-02 03:04:05', 'w-backer'),
	('eve@example.com', 365, 'EARLY', '2026-03-01 00:00:00', NULL, NULL);
INSERT INTO offers (email, trial_days, coupon_id, created_at, expires_at) VALUES
	('gone@example.com', 30, 'EARLY', '2026-03-01 00:00:00', '2026-04-01 00:00:00');`
	if _, err := database.Exec(string(schema) + rows); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestImport(t *testing.T) {
	oldPath := oldDatabase(t)
	before, _ := os.ReadFile(oldPath)
	newPath := filepath.Join(t.TempDir(), "cloud.db")
	got, err := importOld(t.Context(), oldPath, newPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != (result{imported: 3, sameMail: 1, offers: 1}) {
		t.Errorf("result = %+v", got)
	}

	database, err := sql.Open("sqlite", newPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	type row struct {
		id, mail, status, customer, subscription, coupon string
		paidUntil, trialDays, trialEnds                  int64
	}
	// Bob's free year counts from his old workspace: 2026-03-02 03:04:05.
	want := map[string]row{
		"w-paid":   {"w-paid", "ada@example.com", "paid", "cus_ada", "sub_ada", "", 1798848000, 30, 0},
		"w-backer": {"w-backer", "bob@example.com", "trial", "", "", "EARLY", 0, 365, 1772420645 + 365*86400},
		"w-coupon": {"w-coupon", "cy@example.com", "trial", "", "", "BACKER", 0, 30, 0},
	}
	rows, err := database.Query(`SELECT id, mail, status, coalesce(stripe_customer, ''), coalesce(stripe_subscription, ''), coalesce(coupon, ''),
		coalesce(paid_until, 0), trial_days, coalesce(trial_ends, 0) FROM workspaces`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.mail, &r.status, &r.customer, &r.subscription, &r.coupon, &r.paidUntil, &r.trialDays, &r.trialEnds); err != nil {
			t.Fatal(err)
		}
		if r.mail == "eve@example.com" {
			// A new id: nobody had a workspace for the offer. Her year
			// starts with her first report.
			want[r.id] = row{r.id, "eve@example.com", "trial", "", "", "EARLY", 0, 365, 0}
		}
		if r != want[r.id] {
			t.Errorf("%s = %+v, want %+v", r.id, r, want[r.id])
		}
		seen++
	}
	if seen != 4 {
		t.Errorf("%d workspaces, want 4", seen)
	}

	// Twice adds nothing, and the old file never changes.
	if again, err := importOld(t.Context(), oldPath, newPath); err != nil || again.imported != 0 || again.offers != 0 {
		t.Errorf("again = %+v, %v", again, err)
	}
	if after, _ := os.ReadFile(oldPath); string(after) != string(before) {
		t.Error("the old database changed")
	}
	if _, err := importOld(t.Context(), filepath.Join(t.TempDir(), "missing.db"), newPath); err == nil {
		t.Error("imported a database that is not there")
	}
}

func TestImportKeepsEveryPayingWorkspace(t *testing.T) {
	oldPath := oldDatabase(t)
	old, err := sql.Open("sqlite", oldPath)
	if err != nil {
		t.Fatal(err)
	}
	// Ada pays for a second workspace too.
	if _, err := old.Exec(`UPDATE subscriptions SET status = 'active' WHERE workspace_id = 'w-second'`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	newPath := filepath.Join(t.TempDir(), "cloud.db")
	if _, err := importOld(t.Context(), oldPath, newPath); err == nil || !strings.Contains(err.Error(), "w-second") {
		t.Fatalf("err = %v, want one that names w-second", err)
	}
	database, err := sql.Open("sqlite", newPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var n int
	if err := database.QueryRow(`SELECT count(*) FROM workspaces`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d workspaces after a refused import, %v", n, err)
	}
}
