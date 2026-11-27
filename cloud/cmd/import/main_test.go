package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// oldDatabase is a database of the old hub with fake rows:
//
//   - paid: a subscription in Stripe, owned by Ada.
//   - backer: no subscription, but its owner has an offer.
//   - coupon: a subscription row with a coupon only.
//   - free: nothing to import.
//   - second: a second paying workspace of Ada; her mail is taken.
//   - an offer of Eve, never claimed.
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
	('s2', 'w-second', 'pro', 'active', 'cus_ada2', 'sub_ada2', '2027-01-02 00:00:00', 'month', NULL),
	('s3', 'w-coupon', 'pro', 'trialing', NULL, NULL, NULL, NULL, 'BACKER'),
	('s4', 'w-free', 'free', 'active', NULL, NULL, NULL, NULL, NULL);
INSERT INTO offers (email, trial_days, coupon_id, created_at, claimed_at, claimed_workspace_id) VALUES
	('bob@example.com', 30, 'EARLY', '2026-03-01 00:00:00', '2026-03-02 03:04:05', 'w-backer'),
	('eve@example.com', 30, 'EARLY', '2026-03-01 00:00:00', NULL, NULL);`
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
	if got != (result{imported: 3, sameMail: 1, unclaimed: 1}) {
		t.Errorf("result = %+v", got)
	}

	database, err := sql.Open("sqlite", newPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	type row struct {
		id, mail, status, customer, subscription, coupon string
		paidUntil                                        int64
	}
	want := map[string]row{
		"w-paid":   {"w-paid", "ada@example.com", "paid", "cus_ada", "sub_ada", "", 1798848000},
		"w-backer": {"w-backer", "bob@example.com", "trial", "", "", "EARLY", 0},
		"w-coupon": {"w-coupon", "cy@example.com", "trial", "", "", "BACKER", 0},
	}
	rows, err := database.Query(`SELECT id, mail, status, coalesce(stripe_customer, ''), coalesce(stripe_subscription, ''), coalesce(coupon, ''), coalesce(paid_until, 0) FROM workspaces`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.mail, &r.status, &r.customer, &r.subscription, &r.coupon, &r.paidUntil); err != nil {
			t.Fatal(err)
		}
		if r != want[r.id] {
			t.Errorf("%s = %+v, want %+v", r.id, r, want[r.id])
		}
		seen++
	}
	if seen != len(want) {
		t.Errorf("%d workspaces, want %d", seen, len(want))
	}

	// Twice adds nothing, and the old file never changes.
	if again, err := importOld(t.Context(), oldPath, newPath); err != nil || again.imported != 0 {
		t.Errorf("again = %+v, %v", again, err)
	}
	if after, _ := os.ReadFile(oldPath); string(after) != string(before) {
		t.Error("the old database changed")
	}
	if _, err := importOld(t.Context(), filepath.Join(t.TempDir(), "missing.db"), newPath); err == nil {
		t.Error("imported a database that is not there")
	}
}
