package workspace

import (
	"database/sql"
	"errors"
	"time"
)

// Copy is one copy of a database that a server sent to moin cloud. It
// lies in the bucket at Key, encrypted on the server: moin cloud can't
// read it.
type Copy struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"-"`
	Server      string    `json:"server"`
	App         string    `json:"app"`
	Database    string    `json:"database"`
	MadeAt      time.Time `json:"made_at"`
	Size        int64     `json:"size"`
	// State is uploading or done.
	State string `json:"-"`
}

// Key is where the copy lies in the bucket.
func (c Copy) Key() string {
	return c.WorkspaceID + "/" + c.ID + ".age"
}

// NewCopy writes down a copy a server is about to upload, and gives it
// its ID.
func (s *Store) NewCopy(c Copy) (Copy, error) {
	c.ID, c.State = newToken(), "uploading"
	_, err := s.db.Exec(`INSERT INTO copies (id, workspace_id, server, app, database, made_at, size, state, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.WorkspaceID, c.Server, c.App, c.Database, c.MadeAt.Unix(), c.Size, c.State, time.Now().Unix())
	return c, err
}

// CopyOf is the copy id of a workspace.
func (s *Store) CopyOf(workspaceID, id string) (Copy, error) {
	c, err := scanCopy(s.db.QueryRow(`SELECT `+copyColumns+` FROM copies WHERE workspace_id = ? AND id = ?`, workspaceID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// CopyDone notes that a copy is in the bucket, with the bytes it has there.
func (s *Store) CopyDone(id string, size int64) error {
	_, err := s.db.Exec(`UPDATE copies SET state = 'done', size = ? WHERE id = ?`, size, id)
	return err
}

// Copies are the copies of a workspace that are done, newest first: of
// one server and one app, or of all when they are "".
func (s *Store) Copies(workspaceID, server, app string) ([]Copy, error) {
	rows, err := s.db.Query(`SELECT `+copyColumns+` FROM copies WHERE workspace_id = ? AND state = 'done'
		AND (? = '' OR server = ?) AND (? = '' OR app = ?) ORDER BY made_at DESC, id`,
		workspaceID, server, server, app, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	copies := []Copy{}
	for rows.Next() {
		c, err := scanCopy(rows)
		if err != nil {
			return nil, err
		}
		copies = append(copies, c)
	}
	return copies, rows.Err()
}

// Storage is how many bytes the copies of a workspace take, those that
// still upload too.
func (s *Store) Storage(workspaceID string) (int64, error) {
	var used int64
	err := s.db.QueryRow(`SELECT coalesce(sum(size), 0) FROM copies WHERE workspace_id = ?`, workspaceID).Scan(&used)
	return used, err
}

// CopiesSince counts the copies a workspace started since a time: of one
// database, or of all when server is "".
func (s *Store) CopiesSince(workspaceID, server, app, database string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM copies WHERE workspace_id = ? AND created > ?
		AND (? = '' OR (server = ? AND app = ? AND database = ?))`,
		workspaceID, since.Unix(), server, server, app, database).Scan(&n)
	return n, err
}

// Due are the copies whose time is over at now, oldest first:
//   - a done copy older than keep, but never the newest of its database
//   - an upload that never said done, a day after it started
//   - every copy of a workspace that ended more than grace ago, or is gone
func (s *Store) Due(now time.Time, keep, grace time.Duration) ([]Copy, error) {
	rows, err := s.db.Query(`SELECT `+copyColumns+` FROM copies
		LEFT JOIN workspaces ON workspaces.id = copies.workspace_id
		WHERE workspaces.id IS NULL
		OR (workspaces.status = 'ended' AND workspaces.ended <= ?)
		OR (copies.state = 'uploading' AND copies.created <= ?)
		OR (copies.state = 'done' AND copies.created <= ? AND copies.id != (
			SELECT newest.id FROM copies AS newest
			WHERE newest.workspace_id = copies.workspace_id AND newest.server = copies.server
			AND newest.app = copies.app AND newest.database = copies.database AND newest.state = 'done'
			ORDER BY newest.made_at DESC, newest.id LIMIT 1))
		ORDER BY copies.created`,
		now.Add(-grace).Unix(), now.Add(-24*time.Hour).Unix(), now.Add(-keep).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	due := []Copy{}
	for rows.Next() {
		c, err := scanCopy(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, c)
	}
	return due, rows.Err()
}

// DeleteCopy forgets a copy, once it is gone from the bucket.
func (s *Store) DeleteCopy(id string) error {
	_, err := s.db.Exec(`DELETE FROM copies WHERE id = ?`, id)
	return err
}

// StorageMailDue says whether the mail about a full storage may go out to
// a workspace at now, one a day at most, and notes it as sent when so.
func (s *Store) StorageMailDue(workspaceID string, now time.Time) (bool, error) {
	result, err := s.db.Exec(`UPDATE workspaces SET storage_mail = ? WHERE id = ? AND storage_mail <= ?`,
		now.Unix(), workspaceID, now.Add(-24*time.Hour).Unix())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// copyColumns are what one reads into a Copy.
const copyColumns = `copies.id, copies.workspace_id, copies.server, copies.app, copies.database, copies.made_at, copies.size, copies.state`

func scanCopy(row interface{ Scan(...any) error }) (Copy, error) {
	var c Copy
	var madeAt int64
	err := row.Scan(&c.ID, &c.WorkspaceID, &c.Server, &c.App, &c.Database, &madeAt, &c.Size, &c.State)
	c.MadeAt = time.Unix(madeAt, 0).UTC()
	return c, err
}
