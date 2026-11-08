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

// BackupProblem says in one short sentence why a backup does not work,
// from what the daemon and its tools said. "" when it works.
func BackupProblem(raw string) string {
	text := strings.ToLower(raw)
	switch {
	case raw == "":
		return ""
	case strings.Contains(text, "once more to start its backups"):
		return "Deploy the app once more to start its backups."
	case strings.Contains(text, "no space left"):
		return "The server's disk is full."
	case strings.Contains(text, "nosuchbucket") || strings.Contains(text, "bucket does not exist") || strings.Contains(text, "specified bucket does not exist"):
		return "The bucket does not exist."
	case strings.Contains(text, "accessdenied") || strings.Contains(text, "access denied") || strings.Contains(text, "invalidaccesskeyid") || strings.Contains(text, "signaturedoesnotmatch") || strings.Contains(text, "forbidden") || strings.Contains(text, "statuscode: 403"):
		return "The bucket refuses the keys."
	case strings.Contains(text, "no such host") || strings.Contains(text, "connection refused") || strings.Contains(text, "i/o timeout") || strings.Contains(text, "dial tcp") || strings.Contains(text, "deadline exceeded") || strings.Contains(text, "tls:") || strings.Contains(text, "x509") || strings.Contains(text, "connection reset"):
		return "The bucket can't be reached."
	case strings.Contains(text, "is not running"):
		return "The database is not running."
	case strings.Contains(text, "cannot be opened"):
		return "The app's bucket can't be opened: check its keys under Data."
	case strings.Contains(text, "copies are late"):
		return "The copies are late."
	case strings.Contains(text, "daily check failed"):
		return "The daily check could not restore the newest copy."
	case strings.Contains(text, "every change"):
		return "Every change is not written down."
	case strings.Contains(text, "cannot back up"):
		return "moin can't back up this database."
	case strings.Contains(text, "too old for this"):
		return "The daemon on the server is too old: update it."
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
	remembered, err := s.apps.BackupMoments(app.ID, now.Add(-keepDays(app)-24*time.Hour))
	if err != nil {
		return nil, err
	}
	databases := make([]DatabaseBackups, 0, len(backups.Databases))
	for _, backup := range backups.Databases {
		problem := BackupProblem(backup.Error)
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
	if every <= 0 {
		every = time.Duration(protocol.DefaultSchedule.Every) * time.Minute
	}
	return !app.Stopped && copiedAt != nil && now.Sub(*copiedAt) > max(2*every, every+time.Hour)
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
			Error:      clipText(database.Error, 2000),
			CheckError: clipText(database.CheckError, 2000),
			UpdatedAt:  s.now().UTC(),
		}
		if app.Stopped && (strings.Contains(status.Error, "is not running") || strings.Contains(status.Error, "copies are late")) {
			// The user stopped the app: its database is meant to rest.
			status.Error = ""
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
