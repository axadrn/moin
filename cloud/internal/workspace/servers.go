package workspace

import (
	"database/sql"
	"errors"
	"time"
)

// ConnectServer gives a server of a workspace a new token to report with,
// and returns it. A server connected before gets a new one; the old one
// stops working.
func (s *Store) ConnectServer(workspaceID, name string) (string, error) {
	token := newToken()
	_, err := s.db.Exec(`INSERT INTO servers (id, workspace_id, name, token_hash, created) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (workspace_id, name) DO UPDATE SET token_hash = excluded.token_hash`,
		newToken(), workspaceID, name, hash(token), time.Now().Unix())
	return token, err
}

// DisconnectServer forgets a server of a workspace; its token stops working.
func (s *Store) DisconnectServer(workspaceID, name string) error {
	result, err := s.db.Exec(`DELETE FROM servers WHERE workspace_id = ? AND name = ?`, workspaceID, name)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// ServerNames are the names of the servers of a workspace, by name.
func (s *Store) ServerNames(workspaceID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM servers WHERE workspace_id = ? ORDER BY name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ServerByToken is the workspace and the name of the server a token
// belongs to.
func (s *Store) ServerByToken(token string) (workspaceID, name string, err error) {
	err = s.db.QueryRow(`SELECT workspace_id, name FROM servers WHERE token_hash = ?`, hash(token)).Scan(&workspaceID, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return workspaceID, name, err
}

// WatchedWorkspaces are the workspaces that have a server.
func (s *Store) WatchedWorkspaces() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT workspace_id FROM servers ORDER BY workspace_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
