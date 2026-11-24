package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/db"
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

	now := time.Now().UTC()
	database.MustExec(`INSERT INTO users (id, email, created_at) VALUES ('ada', 'Ada@Example.com', $1)`, now)
	database.MustExec(`INSERT INTO workspaces (id, name, slug, created_by, created_at) VALUES ('workspace-ada', 'Ada', 'ada', 'ada', $1)`, now)
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
	if got.User == nil || *got.User != "ada" || got.Workspace == nil || *got.Workspace != "workspace-ada" {
		t.Errorf("claimed by %v, %v", got.User, got.Workspace)
	}
	var open *string
	if err := database.Get(&open, `SELECT claimed_user_id FROM offers WHERE email = 'open@example.com'`); err != nil || open != nil {
		t.Errorf("an open offer got %v, %v", open, err)
	}
}
