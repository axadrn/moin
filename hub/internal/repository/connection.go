package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrConnectionNotFound = errors.New("connection not found")
	ErrConnectionLimit    = errors.New("connection limit reached")
	// ErrConnectionTokenNotFound is a token that is unknown, expired or of an ended
	// connection.
	ErrConnectionTokenNotFound = errors.New("connection token not found")
	// ErrRefreshTokenUsed is a refresh token that was exchanged already.
	ErrRefreshTokenUsed = errors.New("refresh token used already")
)

// Connections stores the connections and their tokens.
type Connections struct {
	db *sqlx.DB
}

func NewConnections(db *sqlx.DB) *Connections {
	return &Connections{db: db}
}

// Create stores a connection unless the user already holds limit live ones.
// Count and insert are one statement, so two approvals at once cannot pass
// the limit.
func (r *Connections) Create(connection *model.Connection, limit int) error {
	result, err := r.db.Exec(`
		INSERT INTO connections (id, user_id, workspace_id, client_name, rights, client_id, resource, created_at)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8
		WHERE (SELECT COUNT(*) FROM connections WHERE user_id = $2 AND ended_at IS NULL) < $9
	`, connection.ID, connection.UserID, connection.WorkspaceID, connection.ClientName, connection.Rights, connection.ClientID, connection.Resource, connection.CreatedAt, limit)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConnectionLimit
	}
	return nil
}

// ByID finds a live connection.
func (r *Connections) ByID(id string) (*model.Connection, error) {
	var connection model.Connection
	err := r.db.Get(&connection, `SELECT * FROM connections WHERE id = $1 AND ended_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &connection, nil
}

// ListByUser returns the live connections of a user, newest first.
func (r *Connections) ListByUser(userID string) ([]model.Connection, error) {
	connections := []model.Connection{}
	err := r.db.Select(&connections, `
		SELECT * FROM connections WHERE user_id = $1 AND ended_at IS NULL
		ORDER BY created_at DESC, id DESC
	`, userID)
	return connections, err
}

// SetRights changes what a live connection of the user may do.
func (r *Connections) SetRights(id, userID string, rights model.Rights) error {
	result, err := r.db.Exec(`UPDATE connections SET rights = $1 WHERE id = $2 AND user_id = $3 AND ended_at IS NULL`, rights, id, userID)
	return oneRowOr(result, err, ErrConnectionNotFound)
}

// End ends a live connection and deletes its tokens, in one transaction.
// An ended connection is not found.
func (r *Connections) End(id string, at time.Time) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`UPDATE connections SET ended_at = $1 WHERE id = $2 AND ended_at IS NULL`, at, id)
	if err := oneRowOr(result, err, ErrConnectionNotFound); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM connection_tokens WHERE connection_id = $1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Touch records a use of a connection.
func (r *Connections) Touch(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE connections SET last_used_at = $1 WHERE id = $2`, at, id)
	return err
}

// AddToken stores a token of a connection.
func (r *Connections) AddToken(token model.ConnectionToken) error {
	_, err := r.db.Exec(`
		INSERT INTO connection_tokens (id, connection_id, kind, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, token.ID, token.ConnectionID, token.Kind, token.TokenHash, token.ExpiresAt, token.CreatedAt)
	return err
}

// Token finds a token of a live connection by its hash and kind, with its
// connection. Expiry is for the service to check.
func (r *Connections) Token(hash, kind string) (*model.ConnectionToken, *model.Connection, error) {
	var row struct {
		model.ConnectionToken
		Connection model.Connection `db:"connection"`
	}
	err := r.db.Get(&row, `
		SELECT t.*,
			c.id AS "connection.id", c.user_id AS "connection.user_id", c.workspace_id AS "connection.workspace_id",
			c.client_name AS "connection.client_name", c.rights AS "connection.rights", c.client_id AS "connection.client_id",
			c.resource AS "connection.resource", c.created_at AS "connection.created_at",
			c.last_used_at AS "connection.last_used_at", c.ended_at AS "connection.ended_at"
		FROM connection_tokens t JOIN connections c ON c.id = t.connection_id
		WHERE t.token_hash = $1 AND t.kind = $2 AND c.ended_at IS NULL
	`, hash, kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrConnectionTokenNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return &row.ConnectionToken, &row.Connection, nil
}

// UseRefresh marks a refresh token exchanged. Only the first of two
// exchanges at once gets it; the second hears ErrRefreshTokenUsed.
func (r *Connections) UseRefresh(id string, at time.Time) error {
	result, err := r.db.Exec(`UPDATE connection_tokens SET used_at = $1 WHERE id = $2 AND kind = 'refresh' AND used_at IS NULL`, at, id)
	return oneRowOr(result, err, ErrRefreshTokenUsed)
}

// DeleteExpiredTokens removes the tokens of a connection that ran out.
func (r *Connections) DeleteExpiredTokens(connectionID string, now time.Time) error {
	_, err := r.db.Exec(`DELETE FROM connection_tokens WHERE connection_id = $1 AND expires_at <= $2`, connectionID, now)
	return err
}

// LiveByUser returns the IDs of the live connections of a user.
func (r *Connections) LiveByUser(userID string) ([]string, error) {
	ids := []string{}
	err := r.db.Select(&ids, `SELECT id FROM connections WHERE user_id = $1 AND ended_at IS NULL`, userID)
	return ids, err
}

func oneRowOr(result sql.Result, err error, missing error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return missing
	}
	return nil
}
