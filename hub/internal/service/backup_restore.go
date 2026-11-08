package service

// Going back to a moment.

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/protocol"
)

// RestoreTarget is where a restore goes: Moment, the ID or label of one
// of the moments, or At, a time in the history. Database names the
// database when the app has several.
type RestoreTarget struct {
	Database string
	Moment   string
	At       *time.Time
	// Confirm is the app's name as the user typed it: a restore replaces
	// the live data, so every surface asks, and the service checks.
	Confirm string
}

// Restore is a restore the daemon runs. It runs on its own, as a big
// database takes longer than a request lasts, and across sessions and
// restarts of the hub: RestoreOf says how it stands. It is kept
// in the database a week.
type Restore = model.BackupRestore

// Restore states. Unknown is a restore whose server went offline during
// it: the daemon tells how it ended when it is back, and until then no
// other restore of the app starts.
const (
	RestoreRunning = model.RestoreRunning
	RestoreUnknown = model.RestoreUnknown
	RestoreDone    = model.RestoreDone
	RestoreFailed  = model.RestoreFailed
)

// keepRestores is how long a restore's record stays.
const keepRestores = 7 * 24 * time.Hour

// StartRestore puts a database of an app back to a moment. The daemon
// builds the restored database beside the live one and switches in one
// step; the state before stays as long as the app keeps its backups: going
// back to it undoes the restore. It reserves the restore, checks the
// moment, then the daemon runs the restore on its own; RestoreOf follows
// it.
func (s *AppService) StartRestore(ctx context.Context, actor Actor, idOrName string, target RestoreTarget) (*Restore, error) {
	if strings.TrimSpace(target.Moment) == "" && target.At == nil {
		return nil, ErrRestoreTarget
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := confirmed(target.Confirm, app.Name); err != nil {
		return nil, err
	}
	restore, err := s.reserveRestore(app, target.Database)
	if err != nil {
		return nil, err
	}
	// From here on it waits for the daemon, which does not know it yet.
	s.setWaiting(restore.ID, true)
	request, label, err := s.restoreRequest(ctx, app, target)
	if err != nil {
		s.setWaiting(restore.ID, false)
		if deleteErr := s.apps.DeleteRestore(restore.ID); deleteErr != nil {
			slog.Error("forget a restore that never started", "id", restore.ID, "error", deleteErr)
		}
		return nil, err
	}
	request.ID = restore.ID
	restore.Database, restore.To = request.Database, label
	if err := s.apps.SetRestore(*restore); err != nil {
		return nil, err
	}
	s.notify(app.WorkspaceID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), restoreTimeout)
		defer cancel()
		err := s.servers.Request(ctx, app.ServerID, protocol.TypeBackupRestore, request, nil)
		s.setWaiting(restore.ID, false)
		switch {
		case errors.Is(err, ErrDaemonGone) || errors.Is(err, context.DeadlineExceeded):
			// The daemon goes on with it: it tells how it ended when it is
			// back (reconcileRestores).
			s.endRestore(actor, app, restore.ID, protocol.RestoreState{State: RestoreUnknown, Error: "the server went offline during the restore; moin asks it how it ended when it is back"})
		case err != nil:
			s.endRestore(actor, app, restore.ID, protocol.RestoreState{State: RestoreFailed, Error: errorText(err)})
		default:
			s.endRestore(actor, app, restore.ID, protocol.RestoreState{State: RestoreDone})
		}
	}()
	return restore, nil
}

// setWaiting notes that the hub waits for a restore's answer, or no more.
func (s *AppService) setWaiting(id string, waiting bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiting == nil {
		s.waiting = map[string]bool{}
	}
	if waiting {
		s.waiting[id] = true
	} else {
		delete(s.waiting, id)
	}
}

// reserveRestore stores a new restore of an app unless one may still run:
// two at once would race for the data.
func (s *AppService) reserveRestore(app *model.App, database string) (*Restore, error) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	restores, err := s.apps.Restores(app.ID)
	if err != nil {
		return nil, err
	}
	for _, restore := range restores {
		if restore.Active() {
			return nil, ErrRestoreRunning
		}
	}
	restore := &Restore{ID: randomToken()[:16], WorkspaceID: app.WorkspaceID, AppID: app.ID, App: app.Name, Database: database, State: RestoreRunning, StartedAt: s.now().UTC()}
	if err := s.apps.AddRestore(*restore, s.now().Add(-keepRestores)); err != nil {
		return nil, err
	}
	return restore, nil
}

// endRestore stores how a restore stands after the daemon said so. A
// restore that ended stays ended: a late word changes nothing.
func (s *AppService) endRestore(actor Actor, app *model.App, id string, state protocol.RestoreState) {
	restore, changed := s.storeRestoreState(app, id, state)
	if !changed {
		return
	}
	if state.State == RestoreDone {
		slog.Info("database restored", "app_id", app.ID, "database", restore.Database, "to", restore.To)
		s.servers.record(actor, "backup.restored", "app", app.Name, map[string]string{"database": restore.Database, "to": restore.To})
	}
	s.notify(app.WorkspaceID)
}

// storeRestoreState writes down how a restore stands and says whether
// that changed anything.
func (s *AppService) storeRestoreState(app *model.App, id string, state protocol.RestoreState) (*Restore, bool) {
	s.restoreMu.Lock()
	defer s.restoreMu.Unlock()
	restore, err := s.apps.Restore(id)
	if err != nil || !restore.Active() || restore.AppID != app.ID {
		return nil, false
	}
	if state.State == RestoreRunning && restore.State == RestoreRunning {
		return nil, false
	}
	restore.State = state.State
	restore.Error = clipText(state.Error, 2000)
	if state.State == RestoreDone || state.State == RestoreFailed {
		finished := s.now().UTC()
		if state.FinishedAt != nil {
			finished = state.FinishedAt.UTC()
		}
		restore.FinishedAt = &finished
	}
	if err := s.apps.SetRestore(*restore); err != nil {
		slog.Error("store how a restore stands failed", "id", id, "error", err)
		return nil, false
	}
	return restore, true
}

// applyRestoreStates takes what a daemon says of the restores of an app:
// those that run, and how the last one of each database ended.
func (s *AppService) applyRestoreStates(app *model.App, states []protocol.RestoreState) {
	for _, state := range states {
		if state.ID == "" {
			continue
		}
		s.endRestore(System(app.WorkspaceID), app, state.ID, state)
	}
}

// RestoreNoWord begins the error of a restore whose daemon does not report
// restores: nobody can say how it ended.
const RestoreNoWord = "Unknown how it ended"

// reconcileRestores asks the daemon of a server how the restores stand that
// the hub lost track of: those whose server went offline during
// them, and those that ran when the hub restarted. It runs when
// the daemon connects and every minute while one may still run
// (RunRestores). It asks with backup.restores, which needs no bucket and
// lists nothing in it, so an app without a bucket or one that is gone
// still answers. The daemon writes a restore down the moment it arrives,
// so one it knows nothing of never reached it. A daemon that reports no
// restores at all cannot say: the restore ends with that, and the app is
// to check.
func (s *AppService) reconcileRestores(serverID string) {
	restores, err := s.apps.ActiveRestoresOn(serverID)
	if err != nil {
		slog.Error("list the restores to reconcile failed", "server_id", serverID, "error", err)
		return
	}
	for _, restore := range restores {
		s.mu.Lock()
		waited := s.waiting[restore.ID]
		s.mu.Unlock()
		if waited {
			continue
		}
		app, err := s.apps.ByID(restore.AppID)
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), backupTimeout)
		var backups protocol.Backups
		err = s.servers.Request(ctx, serverID, protocol.TypeBackupRestores, protocol.BackupRequest{App: app.Folder}, &backups)
		var daemonErr *DaemonError
		if errors.As(err, &daemonErr) {
			// A daemon before backup.restores.
			backups = protocol.Backups{}
			err = s.servers.Request(ctx, serverID, protocol.TypeBackupList, protocol.BackupRequest{App: app.Folder}, &backups)
		}
		cancel()
		if err != nil {
			slog.Warn("ask how a restore stands failed", "id", restore.ID, "error", err)
			continue
		}
		if backups.Restores == nil {
			s.endRestore(System(app.WorkspaceID), app, restore.ID, protocol.RestoreState{State: RestoreFailed, Error: RestoreNoWord + ": the daemon on the server does not report restores. Check the app and its data; update the daemon so moin can tell next time."})
			continue
		}
		s.applyRestoreStates(app, backups.Restores)
		known := slices.ContainsFunc(backups.Restores, func(state protocol.RestoreState) bool { return state.ID == restore.ID })
		if !known {
			s.endRestore(System(app.WorkspaceID), app, restore.ID, protocol.RestoreState{State: RestoreFailed, Error: "the restore never reached the server; the data is as it was"})
		}
	}
}

// restoreAskEvery is how often RunRestores asks the servers of unknown
// restores how they ended.
const restoreAskEvery = time.Minute

// RunRestores asks, every minute, the daemons of restores that may still
// run how they stand: one whose server went offline during it, once the
// server is back, and one that runs while no request waits for it, as
// after a restart of the hub, whose last word may be lost.
func (s *AppService) RunRestores(ctx context.Context) {
	ticker := time.NewTicker(restoreAskEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.askUnknownRestores()
		}
	}
}

// askUnknownRestores asks each online server with a restore that may
// still run: unknown, or running without a request that waits for it, as
// after a restart of the hub. Once one ends it is not asked
// again.
func (s *AppService) askUnknownRestores() {
	servers, err := s.apps.ActiveRestoreServers()
	if err != nil {
		slog.Error("list the servers of unknown restores failed", "error", err)
		return
	}
	for _, serverID := range servers {
		if s.servers.Online(serverID) {
			s.reconcileRestores(serverID)
		}
	}
}

// RestoreOf is a restore of an app by its ID.
func (s *AppService) RestoreOf(workspaceID, idOrName, id string) (*Restore, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	restore, err := s.apps.Restore(id)
	if errors.Is(err, repository.ErrRestoreNotFound) || err == nil && (restore.WorkspaceID != workspaceID || restore.AppID != app.ID) {
		return nil, ErrRestoreNotFound
	}
	return restore, err
}

// LatestRestore is the newest restore of a database of an app from the
// last day, for the page to show while it runs and how it ended; nil
// without one.
func (s *AppService) LatestRestore(workspaceID, idOrName, database string) (*Restore, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	restores, err := s.apps.Restores(app.ID)
	if err != nil {
		return nil, err
	}
	for _, restore := range restores {
		if (database == "" || restore.Database == database || strings.HasPrefix(restore.Database, database+"/")) && (restore.Active() || s.now().Sub(restore.StartedAt) < 24*time.Hour) {
			return &restore, nil
		}
	}
	return nil, nil
}

// pickDatabase is the database a request names, or the app's only one.
func pickDatabase(databases []DatabaseBackups, name string) (*DatabaseBackups, error) {
	for index := range databases {
		if databases[index].Database == name || name == "" && len(databases) == 1 {
			return &databases[index], nil
		}
	}
	if name == "" {
		return nil, ErrRestoreDatabase
	}
	return nil, ErrNoDatabase
}

// restoreRequest checks where a restore goes and says it as people read
// it. A time goes to that minute when Every change reaches it, else to
// the newest copy at or before it (Postgres).
func (s *AppService) restoreRequest(ctx context.Context, app *model.App, target RestoreTarget) (protocol.BackupRestore, string, error) {
	databases, err := s.backups(ctx, app)
	if err != nil {
		return protocol.BackupRestore{}, "", err
	}
	backup, err := pickDatabase(databases, target.Database)
	if err != nil {
		return protocol.BackupRestore{}, "", err
	}
	request := protocol.BackupRestore{App: app.Folder, Database: backup.Database}
	now := s.now().UTC()
	if target.At != nil {
		at := target.At.UTC()
		if since, until, ok := everyChangeReach(backup.DatabaseBackup, now.Add(time.Minute)); ok && !at.Before(since) && !at.After(until) {
			request.At = &at
			return request, at.Format("Jan 2 15:04 UTC"), nil
		}
		for _, moment := range backup.Moments {
			if moment.Copy != "" && !moment.At.After(at) {
				request.Copy = moment.Copy
				return request, moment.Label, nil
			}
		}
		return protocol.BackupRestore{}, "", ErrMomentOutside
	}
	moment, err := findMoment(backup.Moments, target.Moment)
	if err != nil {
		return protocol.BackupRestore{}, "", err
	}
	request.Copy, request.Undo = moment.Copy, moment.Undo
	if moment.Copy == "" && moment.Undo == "" {
		at := moment.At.UTC()
		// "1 hour ago" is an hour before the restore: the list was made a
		// moment earlier.
		for _, quick := range quickMoments {
			if quick.id == moment.ID {
				at = now.Add(-quick.ago)
			}
		}
		request.At = &at
	}
	return request, moment.Label, nil
}
