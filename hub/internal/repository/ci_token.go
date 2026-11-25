package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrCITokenNotFound = errors.New("ci token not found")
	ErrCITokenLimit    = errors.New("ci token limit reached")
)

// CITokens stores the CI tokens.
type CITokens struct {
	db *sqlx.DB
}

func NewCITokens(db *sqlx.DB) *CITokens {
	return &CITokens{db: db}
}

// ciTokenSelect reads a CI token with the name of its app and its maker.
const ciTokenSelect = `
	SELECT t.*, a.name AS app_name, u.name AS maker_name
	FROM ci_tokens t
	JOIN users u ON u.id = t.user_id
	LEFT JOIN apps a ON a.id = t.app_id`

// Create stores a token unless the user already holds limit tokens. Count
// and insert are one statement, so two creates at once cannot pass the limit.
func (r *CITokens) Create(token *model.CIToken, limit int) error {
	result, err := r.db.Exec(`
		INSERT INTO ci_tokens (id, user_id, workspace_id, name, token_hash, prefix, rights, app_id, expires_at, created_at)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		WHERE (SELECT COUNT(*) FROM ci_tokens WHERE user_id = $2) < $11
	`, token.ID, token.UserID, token.WorkspaceID, token.Name, token.TokenHash, token.Prefix, token.Rights, token.AppID, token.ExpiresAt, token.CreatedAt, limit)
	if err := oneRowOr(result, err, ErrCITokenLimit); err != nil {
		return err
	}
	return nil
}

// ByTokenHash finds a token by the hash of the raw token.
func (r *CITokens) ByTokenHash(hash string) (*model.CIToken, error) {
	return r.one(ciTokenSelect+` WHERE t.token_hash = $1`, hash)
}

// ByID finds a token of a workspace.
func (r *CITokens) ByID(workspaceID, id string) (*model.CIToken, error) {
	return r.one(ciTokenSelect+` WHERE t.id = $1 AND t.workspace_id = $2`, id, workspaceID)
}

func (r *CITokens) one(query string, args ...any) (*model.CIToken, error) {
	var token model.CIToken
	err := r.db.Get(&token, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCITokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// ListByWorkspace returns the tokens of a workspace, newest first: every
// token, or with userID only that user's.
func (r *CITokens) ListByWorkspace(workspaceID, userID string) ([]model.CIToken, error) {
	tokens := []model.CIToken{}
	err := r.db.Select(&tokens, ciTokenSelect+`
		WHERE t.workspace_id = $1 AND ($2 = '' OR t.user_id = $2)
		ORDER BY t.created_at DESC, t.id DESC
	`, workspaceID, userID)
	return tokens, err
}

// ListByUser returns every token of a user, newest first.
func (r *CITokens) ListByUser(userID string) ([]model.CIToken, error) {
	tokens := []model.CIToken{}
	err := r.db.Select(&tokens, ciTokenSelect+` WHERE t.user_id = $1 ORDER BY t.created_at DESC, t.id DESC`, userID)
	return tokens, err
}

// SetRights changes what a token may do.
func (r *CITokens) SetRights(id string, rights model.Rights) error {
	result, err := r.db.Exec(`UPDATE ci_tokens SET rights = $1 WHERE id = $2`, rights, id)
	return oneRowOr(result, err, ErrCITokenNotFound)
}

// SwapRights changes the rights of a token from from to to, only if they
// are still from. It reports whether it changed them.
func (r *CITokens) SwapRights(id string, from, to model.Rights) (bool, error) {
	result, err := r.db.Exec(`UPDATE ci_tokens SET rights = $1 WHERE id = $2 AND rights = $3`, to, id, from)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

// TouchLastUsed records a use.
func (r *CITokens) TouchLastUsed(id string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE ci_tokens SET last_used_at = $1 WHERE id = $2`, at, id)
	return err
}

// Delete removes a token.
func (r *CITokens) Delete(id string) error {
	result, err := r.db.Exec(`DELETE FROM ci_tokens WHERE id = $1`, id)
	return oneRowOr(result, err, ErrCITokenNotFound)
}

// DeleteByUser removes every token of a user.
func (r *CITokens) DeleteByUser(userID string) error {
	_, err := r.db.Exec(`DELETE FROM ci_tokens WHERE user_id = $1`, userID)
	return err
}
