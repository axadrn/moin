package service

// How backups stand: what the daemon says, and what the hub tells people.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// backupProblems are the short sentences for what the daemon and its
// tools said. The first row with a matching phrase wins.
var backupProblems = []struct {
	sentence string
	phrases  []string
}{
	{"Deploy the app once more to start its backups.", []string{"once more to start its backups"}},
	{"The copy stopped: no data moved.", []string{"no data moved"}},
	{"The server's disk is full.", []string{"no space left"}},
	{"The bucket does not exist.", []string{"nosuchbucket", "bucket does not exist", "specified bucket does not exist"}},
	{"The bucket refuses the keys.", []string{"accessdenied", "access denied", "invalidaccesskeyid", "signaturedoesnotmatch", "forbidden", "statuscode: 403"}},
	{"The bucket can't be reached.", []string{"no such host", "connection refused", "i/o timeout", "dial tcp", "deadline exceeded", "tls:", "x509", "connection reset"}},
	{"The database is not running.", []string{"is not running"}},
	{"The app's bucket can't be opened: check its keys under Data.", []string{"cannot be opened"}},
	{"The copies are late.", []string{"copies are late"}},
	{"The daily check could not restore the newest state.", []string{"daily check failed"}},
	{"Every change is not written down.", []string{"every change"}},
	{"moin can't back up this database.", []string{"cannot back up"}},
	{"The daemon on the server is too old: update it.", []string{"too old for this"}},
}

// BackupProblem says in one short sentence why a backup does not work.
// It is "" when it works.
func BackupProblem(raw string) string {
	if raw == "" {
		return ""
	}
	text := strings.ToLower(raw)
	for _, problem := range backupProblems {
		for _, phrase := range problem.phrases {
			if strings.Contains(text, phrase) {
				return problem.sentence
			}
		}
	}
	return "The backup does not work."
}

// Backups asks the daemon how the databases of an app are backed up and
// adds the moments each can go back to.
func (s *AppService) Backups(ctx context.Context, workspaceID, idOrName string) (*model.App, []DatabaseBackups, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	databases, err := s.backups(ctx, app)
	return app, databases, err
}

func (s *AppService) backups(ctx context.Context, app *model.App) ([]DatabaseBackups, error) {
	if _, err := s.AppBucket(app); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	var backups protocol.Backups
	err := s.servers.Request(ctx, app.ServerID, protocol.TypeBackupList, protocol.BackupRequest{App: app.Folder}, &backups)
	if err != nil {
		return nil, err
	}
	s.storeBackupStatus(app, backups)
	s.applyRestoreStates(app, backups.Restores)
	now := s.now().UTC()
	remembered, err := s.apps.BackupMoments(app.ID, now.Add(-keepFor(app)-24*time.Hour))
	if err != nil {
		return nil, err
	}
	databases := make([]DatabaseBackups, 0, len(backups.Databases))
	for _, backup := range backups.Databases {
		problem := BackupProblem(restingError(app, backup.Error))
		if problem == "" && late(app, backup.CopiedAt, now) {
			problem = "The copies are late."
		}
		databases = append(databases, DatabaseBackups{DatabaseBackup: backup, Problem: problem, Moments: Moments(backup, remembered, now)})
	}
	return databases, nil
}

// late says the newest copy is older than twice how often the app is
// copied, and than one more copy may take (an hour): the copies stopped
// coming.
func late(app *model.App, copiedAt *time.Time, now time.Time) bool {
	every := time.Duration(scheduleOf(app).Every) * time.Minute
	return !app.Stopped && copiedAt != nil && now.Sub(*copiedAt) > max(2*every, every+time.Hour)
}

// restingError drops the complaints about a database whose app the user
// stopped: it is meant to rest.
func restingError(app *model.App, raw string) string {
	if app.Stopped && (strings.Contains(raw, "is not running") || strings.Contains(raw, "copies are late")) {
		return ""
	}
	return raw
}

// BackupStatus is what the daemon last reported about the app's databases.
func (s *AppService) BackupStatus(workspaceID, idOrName string) ([]model.BackupStatus, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.apps.BackupStatus(app.ID)
}

// receiveBackupStatus stores a status the daemon sent on its own.
func (s *AppService) receiveBackupStatus(serverID string, backups protocol.Backups) {
	workspaceID, ok := s.servers.UserOf(serverID)
	if !ok {
		return
	}
	app, err := s.apps.ByFolderForWorkspace(backups.App, workspaceID)
	if err != nil || app.ServerID != serverID {
		return
	}
	if backups.Detected != nil {
		s.storeDetected(app, *backups.Detected)
	}
	s.storeBackupStatus(app, backups)
	s.applyRestoreStates(app, backups.Restores)
	s.notify(workspaceID)
}

// storeDetected replaces what the live deploy of an app has in it.
func (s *AppService) storeDetected(app *model.App, detected protocol.Detected) {
	deploy, err := s.apps.Deploy(app.ID, app.LiveNumber)
	if err != nil {
		return
	}
	body, err := json.Marshal(detected)
	if err == nil {
		err = s.apps.SetDeployDetected(deploy.ID, string(body))
	}
	if err != nil {
		slog.Error("store what the daemon found failed", "app_id", app.ID, "error", fmt.Sprint(err))
	}
}

// storeBackupStatus stores the backup status of each database. Backups
// that start failing or work again, a restore check that fails or passes
// again, and each passed check go into the activity as moin itself; the
// changes alert. The backups' own error and the check's are apart: a
// daemon from before check_error sends the check's failure as Error alone.
func (s *AppService) storeBackupStatus(app *model.App, backups protocol.Backups) {
	appID := app.ID
	before := map[string]model.BackupStatus{}
	if statuses, err := s.apps.BackupStatus(appID); err == nil {
		for _, status := range statuses {
			before[status.Database] = status
		}
	}
	for _, database := range backups.Databases {
		previous, known := before[database.Database]
		status := model.BackupStatus{
			AppID:      appID,
			Database:   database.Database,
			CopiedAt:   database.CopiedAt,
			Since:      database.Since,
			Until:      database.Until,
			CheckedAt:  database.CheckedAt,
			Error:      clipText(restingError(app, database.Error), 2000),
			CheckError: clipText(database.CheckError, 2000),
			UpdatedAt:  s.now().UTC(),
		}
		if database.CheckedAt == nil {
			// No check since the daemon started: the last one still stands.
			status.CheckError = previous.CheckError
		}
		for _, change := range backupChanges(previous, known, status) {
			s.servers.record(System(app.WorkspaceID), change.action, "app", app.Name, change.details)
		}
		err := s.apps.SetBackupStatus(status)
		if err != nil {
			slog.Error("store backup status failed", "app_id", appID, "error", fmt.Sprint(err))
		}
	}
}

// backupChange is one action of the activity a backup status brings.
type backupChange struct {
	action  string
	details map[string]string
}

// backupChanges compares a database's backup status with the one before
// and says what changed: backups that fail or work again, a restore check
// that fails or passes again, and a passed check. Only a change counts, so
// a daemon that tells the same failure every minute alerts once.
func backupChanges(previous model.BackupStatus, known bool, now model.BackupStatus) []backupChange {
	changes := []backupChange{}
	add := func(action string, details map[string]string) {
		details["database"] = now.Database
		changes = append(changes, backupChange{action: action, details: details})
	}
	backupError, before := now.BackupError(), previous.BackupError()
	switch {
	case backupError != "" && (!known || before == ""):
		// The one short sentence: the raw output stays on the database page.
		add("backup.failed", map[string]string{"error": BackupProblem(backupError)})
	case backupError == "" && known && before != "":
		add("backup.recovered", map[string]string{})
	}
	if now.CheckedAt == nil {
		return changes
	}
	switch {
	case now.CheckError != "" && (!known || previous.CheckError == ""):
		add("check.failed", map[string]string{"error": BackupProblem(now.CheckError)})
	case now.CheckError == "" && known && previous.CheckError != "":
		add("check.recovered", map[string]string{})
	}
	if now.CheckError == "" && (!known || previous.CheckedAt == nil || now.CheckedAt.After(*previous.CheckedAt)) {
		add("backup.checked", map[string]string{"checked_at": now.CheckedAt.UTC().Format(time.RFC3339)})
	}
	return changes
}
