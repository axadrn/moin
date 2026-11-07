package handler

import (
	"net/url"
	"strings"
	"testing"
)

// A restore from the page carries the name typed into the confirm dialog
// as X-Moin-Confirm; the service refuses one without it, so the page can
// never skip the question.
func TestRestoreNeedsTheTypedName(t *testing.T) {
	s := newSwitches(t)
	form := url.Values{"moment": {"1h"}, "database": {"/data/app.db"}}
	body := s.post("/app/{workspaceSlug}/apps/{app}/backups/restore", "/app/ada/apps/blog/backups/restore", form, s.handler.BackupRestore)
	if !strings.Contains(body, "Type the app") {
		t.Fatalf("a restore without the name: %s", body)
	}
	if activity(t, s, "backup.restored") != 0 {
		t.Fatal("a restore without the name ran")
	}
}

// The rows of how often and how long save the schedule and record it.
func TestBackupScheduleRows(t *testing.T) {
	s := newSwitches(t)
	form := url.Values{"every": {"15"}, "keep": {"30"}, "database": {"/data/app.db"}}
	s.post("/app/{workspaceSlug}/apps/{app}/backups/schedule", "/app/ada/apps/blog/backups/schedule", form, s.handler.BackupSchedule)
	if app := s.app(); app.BackupEvery != 15 || app.BackupKeep != 30 || app.BackupEveryChange {
		t.Fatalf("stored %d %d %v", app.BackupEvery, app.BackupKeep, app.BackupEveryChange)
	}
	if activity(t, s, "backup.scheduled") != 1 {
		t.Fatal("no backup.scheduled in the activity")
	}
	form.Set("every", "5")
	body := s.post("/app/{workspaceSlug}/apps/{app}/backups/schedule", "/app/ada/apps/blog/backups/schedule", form, s.handler.BackupSchedule)
	if !strings.Contains(body, "15 minutes") || s.app().BackupEvery != 15 {
		t.Fatalf("a wrong choice: %s", body)
	}
}
