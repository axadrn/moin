package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// The steps of a daemon update.
//
// Mostly an update is one step: the daemon swaps its binary, and the apps
// keep running. A release whose update needs more, such as stopping apps
// first, adds an entry here. Entries stay forever, like migrations: a daemon
// that is not older than Before skips them. No customer ever does a step by
// hand.
//
// The rules for an entry:
//   - Its steps run top to bottom, updateDaemon among them once.
//   - Before updateDaemon the old daemon answers: use only messages it
//     knows, and only stop apps, so moin can always start them again.
//   - After updateDaemon the new daemon answers. These steps may run twice:
//     when the hub or the server stopped halfway, the next connect of the
//     daemon runs them again.
//   - An entry stands alone: it does not count on another one.
//
// A daemon that skipped several releases gets every entry it is older than,
// as one recipe (plan): what comes before updateDaemon in each, oldest
// first, then updateDaemon once, then what comes after it, oldest first.
var daemonSteps = []daemonStep{
	{
		Before: "v1.0.0-beta.26",
		Why:    "The old backup process can damage a SQLite database that is written while it exits.",
		Steps: []step{
			stopSQLiteApps,
			updateDaemon,
			startStoppedApps,
		},
	},
}

// daemonStep is one entry: a daemon older than Before runs Steps.
type daemonStep struct {
	Before string
	Why    string
	Steps  []step
}

// step is one thing an update does. Name is what the activity says when it
// fails.
type step struct {
	Name string
	Run  func(ctx context.Context, run *daemonRun) error
}

// updateDaemon swaps the daemon's binary and waits until it is back.
var updateDaemon = step{Name: "update the daemon", Run: func(ctx context.Context, run *daemonRun) error {
	updated, err := run.servers.swapDaemon(ctx, run.actor, run.server)
	if err != nil {
		return err
	}
	run.server = updated
	return nil
}}

// stopSQLiteApps stops the running apps of the server that have a SQLite
// database.
var stopSQLiteApps = step{Name: "stop the apps with SQLite", Run: func(ctx context.Context, run *daemonRun) error {
	return run.stopApps(ctx, func(detected *protocol.Detected) bool {
		return slices.ContainsFunc(detected.Databases, func(database protocol.Database) bool {
			return database.Kind != "postgres"
		})
	})
}}

// startStoppedApps starts the apps this update stopped.
var startStoppedApps = step{Name: "start the stopped apps again", Run: func(ctx context.Context, run *daemonRun) error {
	return run.startApps()
}}

// plan is the recipe for a daemon on version: the steps of every entry it
// is older than, merged around one updateDaemon. A version moin cannot read,
// such as a development build, is older than every entry: stopping an app
// too many is better than a damaged database.
func plan(version string) []step {
	var before, after []step
	for _, entry := range daemonSteps {
		if parseVersion(version) != nil && !NewerVersion(entry.Before, version) {
			continue
		}
		i := slices.IndexFunc(entry.Steps, isUpdateDaemon)
		before = appendNew(before, entry.Steps[:i]...)
		after = appendNew(after, entry.Steps[i+1:]...)
	}
	return append(append(before, updateDaemon), after...)
}

// afterUpdate is the part of a recipe after updateDaemon.
func afterUpdate(steps []step) []step {
	return steps[slices.IndexFunc(steps, isUpdateDaemon)+1:]
}

func isUpdateDaemon(s step) bool { return s.Name == updateDaemon.Name }

// appendNew appends the steps list does not have yet: two entries that both
// start the stopped apps start them once.
func appendNew(list []step, steps ...step) []step {
	for _, add := range steps {
		if !slices.ContainsFunc(list, func(s step) bool { return s.Name == add.Name }) {
			list = append(list, add)
		}
	}
	return list
}

// StepError is a failed step of a daemon update. The activity has it, and
// it alerts.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

// daemonRun is one daemon update as it runs, with its note in the database.
type daemonRun struct {
	servers *ServerService
	actor   Actor
	server  *model.Server
	note    model.DaemonUpdate
}

// runDaemonUpdate runs the recipe for the server's daemon. An update without
// steps is the swap alone. With steps the hub keeps a note until it is done.
// A step that fails before the swap starts the stopped apps again, so all is
// as before; one that fails after it stays in the note, and the next connect
// of the daemon runs what comes after the swap again.
func (s *ServerService) runDaemonUpdate(ctx context.Context, actor Actor, server *model.Server) (*model.Server, error) {
	if !s.startUpdating(server.ID) {
		return nil, ErrDaemonUpdating
	}
	defer s.stopUpdating(server.ID)
	steps := plan(server.DaemonVersion)
	if len(steps) == 1 {
		return s.swapDaemon(ctx, actor, server)
	}
	run := &daemonRun{servers: s, actor: actor, server: server, note: model.DaemonUpdate{
		ServerID: server.ID, FromVersion: server.DaemonVersion, StoppedApps: "[]", StartedAt: s.now().UTC(),
	}}
	if err := s.servers.SaveDaemonUpdate(run.note); err != nil {
		return nil, err
	}
	swapped := false
	for _, step := range steps {
		if err := step.Run(ctx, run); err != nil {
			if !swapped && run.startApps() == nil {
				run.done()
			}
			return nil, run.failed(step, err)
		}
		swapped = swapped || isUpdateDaemon(step)
	}
	run.done()
	return run.server, nil
}

// ErrDaemonUpdating means an update of this daemon runs already.
var ErrDaemonUpdating = errors.New("the daemon of this server is updating")

// finishDaemonUpdate finishes an update the hub or the server stopped
// halfway, when its daemon connects: one that never swapped starts the
// stopped apps again; one that swapped runs what comes after the swap.
func (s *ServerService) finishDaemonUpdate(workspaceID, serverID string) {
	if note, err := s.servers.DaemonUpdate(serverID); err != nil || note == nil {
		return
	}
	if !s.startUpdating(serverID) {
		return // it runs right now and waits for this connect
	}
	defer s.stopUpdating(serverID)
	note, err := s.servers.DaemonUpdate(serverID)
	if err != nil || note == nil {
		return
	}
	server, err := s.Get(workspaceID, serverID)
	if err != nil {
		return
	}
	run := &daemonRun{servers: s, actor: System(server.WorkspaceID), server: server, note: *note}
	steps := []step{startStoppedApps}
	if server.DaemonVersion != note.FromVersion {
		steps = afterUpdate(plan(note.FromVersion))
	}
	for _, step := range steps {
		if err := step.Run(context.Background(), run); err != nil {
			run.failed(step, err)
			return
		}
	}
	run.done()
}

// startUpdating marks an update of the server's daemon as running, false
// when one runs already.
func (s *ServerService) startUpdating(serverID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updating[serverID] {
		return false
	}
	s.updating[serverID] = true
	return true
}

func (s *ServerService) stopUpdating(serverID string) {
	s.mu.Lock()
	delete(s.updating, serverID)
	s.mu.Unlock()
}

// stopApps stops the running apps of the server that pick says, and writes
// each into the note before it stops it.
func (r *daemonRun) stopApps(ctx context.Context, pick func(*protocol.Detected) bool) error {
	apps := r.servers.apps
	if apps == nil {
		return nil
	}
	list, err := apps.apps.ListByWorkspaceID(r.server.WorkspaceID)
	if err != nil {
		return err
	}
	for _, app := range list {
		if app.ServerID != r.server.ID || app.LiveNumber == 0 || app.Stopped {
			continue
		}
		detected := apps.liveDetected(&app)
		if detected == nil || !pick(detected) {
			continue
		}
		stopped := append(r.stopped(), app.ID)
		body, _ := json.Marshal(stopped)
		r.note.StoppedApps = string(body)
		if err := r.servers.servers.SaveDaemonUpdate(r.note); err != nil {
			return err
		}
		if err := apps.Stop(ctx, System(r.server.WorkspaceID), app.ID); err != nil {
			return fmt.Errorf("stop %s: %w", app.Name, err)
		}
	}
	return nil
}

// startApps starts the apps the update stopped. One that runs already, or is
// gone, is fine.
func (r *daemonRun) startApps() error {
	apps := r.servers.apps
	if apps == nil {
		return nil
	}
	for _, id := range r.stopped() {
		_, err := apps.Start(System(r.server.WorkspaceID), id)
		if err != nil && !errors.Is(err, ErrRunning) && !errors.Is(err, repository.ErrAppNotFound) {
			return fmt.Errorf("start %s: %w", id, err)
		}
	}
	return nil
}

func (r *daemonRun) stopped() []string {
	var ids []string
	_ = json.Unmarshal([]byte(r.note.StoppedApps), &ids)
	return ids
}

// done removes the note: the update is over.
func (r *daemonRun) done() {
	if err := r.servers.servers.DeleteDaemonUpdate(r.server.ID); err != nil {
		slog.Error("remove the note of a daemon update failed", "server_id", r.server.ID, "error", err)
	}
}

// failed writes the failed step to the activity, which alerts, and returns
// it as a StepError.
func (r *daemonRun) failed(failed step, err error) error {
	r.servers.record(System(r.server.WorkspaceID), "server.daemon_update_failed", "server", r.server.Name, map[string]string{
		"from": r.note.FromVersion, "to": build.Version, "step": failed.Name, "error": clipText(err.Error(), 300),
	})
	return &StepError{Step: failed.Name, Err: err}
}
