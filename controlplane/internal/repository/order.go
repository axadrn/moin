package repository

import (
	"errors"

	"github.com/jmoiron/sqlx"
)

// ErrOrder means an order does not name every app or server of the
// workspace exactly once.
var ErrOrder = errors.New("the order must name every item of the workspace once")

// setOrder gives the rows of table in a workspace the positions of ids, in
// one transaction. ids must be every row of the workspace, each once.
func setOrder(db *sqlx.DB, table, workspaceID string, ids []string) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	err = tx.Get(&count, `SELECT COUNT(*) FROM `+table+` WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return err
	}
	if count != len(ids) {
		return ErrOrder
	}
	seen := make(map[string]bool, len(ids))
	for position, id := range ids {
		if seen[id] {
			return ErrOrder
		}
		seen[id] = true
		result, err := tx.Exec(`UPDATE `+table+` SET position = $1 WHERE id = $2 AND workspace_id = $3`, position, id, workspaceID)
		if err != nil {
			return err
		}
		if rows, err := result.RowsAffected(); err != nil || rows != 1 {
			return ErrOrder
		}
	}
	return tx.Commit()
}

// SetOrder puts the apps of a workspace in the order of ids.
func (r *appRepository) SetOrder(workspaceID string, ids []string) error {
	return setOrder(r.db, "apps", workspaceID, ids)
}

// SetOrder puts the servers of a workspace in the order of ids.
func (r *serverRepository) SetOrder(workspaceID string, ids []string) error {
	return setOrder(r.db, "servers", workspaceID, ids)
}
