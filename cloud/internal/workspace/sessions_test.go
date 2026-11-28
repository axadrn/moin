package workspace

import (
	"errors"
	"testing"
	"time"
)

// The settings page lists the sessions of a workspace, the one used last
// first, and signs one or all of them out. Another workspace's session
// stays.
func TestSessionList(t *testing.T) {
	s := testStore(t)
	space, _ := s.SignIn("ada@example.com")
	other, _ := s.SignIn("grace@example.com")
	browser, _ := s.StartSession(space.ID, "browser")
	cli, _ := s.StartSession(space.ID, "cli")
	stranger, _ := s.StartSession(other.ID, "browser")
	if _, err := s.db.Exec(`UPDATE sessions SET last_used = ? WHERE token_hash = ?`, time.Now().Add(-time.Hour).Unix(), hash(browser)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionWorkspace(browser); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.Sessions(space.ID, browser)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions = %+v, %v", sessions, err)
	}
	if first := sessions[0]; first.Kind != "browser" || !first.This || time.Since(first.LastUsed) > time.Minute || first.Created.IsZero() {
		t.Errorf("the browser that was just used is not first: %+v", sessions)
	}
	if sessions[1].Kind != "cli" || sessions[1].This {
		t.Errorf("the CLI: %+v", sessions[1])
	}
	if err := s.EndSessionOf(other.ID, sessions[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionWorkspace(cli); err != nil {
		t.Errorf("another workspace signed the CLI out: %v", err)
	}
	if err := s.EndSessionOf(space.ID, sessions[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionWorkspace(cli); !errors.Is(err, ErrNotFound) {
		t.Errorf("the CLI signed out: %v", err)
	}
	if err := s.EndSessions(space.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionWorkspace(browser); !errors.Is(err, ErrNotFound) {
		t.Errorf("the browser after sign out everywhere: %v", err)
	}
	if _, err := s.SessionWorkspace(stranger); err != nil {
		t.Errorf("another workspace's session: %v", err)
	}
}
