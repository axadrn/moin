package workspace

import (
	"time"
)

// How long a session lasts: a browser a month, a CLI a year.
const (
	browserSession = 30 * 24 * time.Hour
	cliSession     = 365 * 24 * time.Hour
)

// StartSession signs a browser or a CLI (kind) in to a workspace, and
// returns its token.
func (s *Store) StartSession(workspaceID, kind string) (string, error) {
	lasts := browserSession
	if kind == "cli" {
		lasts = cliSession
	}
	token := newToken()
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, workspace_id, kind, expires) VALUES (?, ?, ?, ?)`,
		hash(token), workspaceID, kind, time.Now().Add(lasts).Unix())
	return token, err
}

// SessionWorkspace is the workspace a session token signed in to.
func (s *Store) SessionWorkspace(token string) (Workspace, error) {
	if token == "" {
		return Workspace{}, ErrNotFound
	}
	return s.one(`SELECT `+prefixed+` FROM sessions JOIN workspaces ON workspaces.id = sessions.workspace_id
		WHERE sessions.token_hash = ? AND sessions.expires > ?`, hash(token), time.Now().Unix())
}

// prefixed are the columns of a Workspace in a query with a join.
const prefixed = `workspaces.id, workspaces.mail, workspaces.status, coalesce(workspaces.trial_ends, 0), coalesce(workspaces.paid_until, 0), coalesce(workspaces.coupon, '')`

// EndSession signs a session out.
func (s *Store) EndSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash(token))
	return err
}
