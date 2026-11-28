package workspace

import (
	"time"
)

// How long a session lasts: a browser a month, a CLI a year.
const (
	browserSession = 30 * 24 * time.Hour
	cliSession     = 365 * 24 * time.Hour
)

// usedEvery is how often a session writes down that it was used: once a
// minute at most, not on every request.
const usedEvery = time.Minute

// Session is a browser or a CLI (Kind) signed in to a workspace. ID names
// it on the settings page: the hash of its token, which signs nothing in.
// This is the session that asks.
type Session struct {
	ID       string
	Kind     string
	Created  time.Time
	LastUsed time.Time
	This     bool
}

// StartSession signs a browser or a CLI (kind) in to a workspace, and
// returns its token.
func (s *Store) StartSession(workspaceID, kind string) (string, error) {
	lasts := browserSession
	if kind == "cli" {
		lasts = cliSession
	}
	token := newToken()
	now := time.Now()
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, workspace_id, kind, expires, created, last_used) VALUES (?, ?, ?, ?, ?, ?)`,
		hash(token), workspaceID, kind, now.Add(lasts).Unix(), now.Unix(), now.Unix())
	return token, err
}

// SessionWorkspace is the workspace a session token signed in to. It
// writes down that the session was used.
func (s *Store) SessionWorkspace(token string) (Workspace, error) {
	if token == "" {
		return Workspace{}, ErrNotFound
	}
	now := time.Now()
	space, err := s.one(`SELECT `+columns+` FROM sessions JOIN workspaces ON workspaces.id = sessions.workspace_id
		WHERE sessions.token_hash = ? AND sessions.expires > ?`, hash(token), now.Unix())
	if err != nil {
		return space, err
	}
	_, err = s.db.Exec(`UPDATE sessions SET last_used = ? WHERE token_hash = ? AND last_used <= ?`,
		now.Unix(), hash(token), now.Add(-usedEvery).Unix())
	return space, err
}

// Sessions are the sessions of a workspace that have not ended, the one
// used last first. current is the token of the session that asks.
func (s *Store) Sessions(workspaceID, current string) ([]Session, error) {
	rows, err := s.db.Query(`SELECT token_hash, kind, created, last_used FROM sessions
		WHERE workspace_id = ? AND expires > ? ORDER BY last_used DESC, created DESC`, workspaceID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []Session{}
	for rows.Next() {
		var session Session
		var created, used int64
		if err := rows.Scan(&session.ID, &session.Kind, &created, &used); err != nil {
			return nil, err
		}
		session.Created, session.LastUsed = unix(created), unix(used)
		session.This = session.ID == hash(current)
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// EndSession signs a session out.
func (s *Store) EndSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash(token))
	return err
}

// EndSessionOf signs out the session id of a workspace. A session of
// another workspace stays.
func (s *Store) EndSessionOf(workspaceID, id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE workspace_id = ? AND token_hash = ?`, workspaceID, id)
	return err
}

// EndSessions signs out every browser and every CLI of a workspace.
func (s *Store) EndSessions(workspaceID string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE workspace_id = ?`, workspaceID)
	return err
}
