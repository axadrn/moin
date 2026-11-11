package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/pressly/goose/v3"
)

// TestMigration043FindsWhoGotAnOffer runs 043 over offers claimed before
// it: the account with that address got it.
func TestMigration043FindsWhoGotAnOffer(t *testing.T) {
	database, err := db.Init(filepath.Join(t.TempDir(), "hub.db") + "?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.SetupGoose(); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpTo(database.DB, ".", 42); err != nil {
		t.Fatal(err)
	}

	ada := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "Ada@Example.com", Verified: true})
	now := time.Now().UTC()
	database.MustExec(`INSERT INTO offers (email, trial_days, coupon_id, reason, created_at, claimed_at) VALUES
		('ada@example.com', 365, 'c', '', $1, $1),
		('open@example.com', 365, 'c', '', $1, NULL)`, now)

	if err := goose.UpTo(database.DB, ".", 43); err != nil {
		t.Fatal(err)
	}

	var got struct {
		User      *string `db:"claimed_user_id"`
		Workspace *string `db:"claimed_workspace_id"`
	}
	if err := database.Get(&got, `SELECT claimed_user_id, claimed_workspace_id FROM offers WHERE email = 'ada@example.com'`); err != nil {
		t.Fatal(err)
	}
	if got.User == nil || *got.User != ada.User.ID || got.Workspace == nil || *got.Workspace != ada.Workspace.ID {
		t.Errorf("claimed by %v, %v", got.User, got.Workspace)
	}
	var open *string
	if err := database.Get(&open, `SELECT claimed_user_id FROM offers WHERE email = 'open@example.com'`); err != nil || open != nil {
		t.Errorf("an open offer got %v, %v", open, err)
	}
}
