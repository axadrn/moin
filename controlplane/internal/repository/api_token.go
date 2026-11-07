package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

var ErrAPITokenNotFound = errors.New("api token not found")

// APITokens stores the personal and device API tokens.
type APITokens struct {
	db *sqlx.DB
}

func NewAPITokens(db *sqlx.DB) *APITokens {
	return &APITokens{db: db}
}

func (r *APITokens) Create(token *model.APIToken) error {
	_, err := r.db.Exec(`
		INSERT INTO api_tokens (
			id, user_id, workspace_id, name, token_hash, prefix, scopes, kind, last_used_at, expires_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		token.ID,
		token.UserID,
		token.WorkspaceID,
		token.Name,
		token.TokenHash,
		token.Prefix,
		token.Scopes,
		token.Kind,
		token.LastUsedAt,
		token.ExpiresAt,
		token.CreatedAt,
	)
	return err
}

func (r *APITokens) ByTokenHash(tokenHash string) (*model.APIToken, error) {
	token := &model.APIToken{}
	err := r.db.Get(token, `SELECT * FROM api_tokens WHERE token_hash = $1`, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPITokenNotFound
	}
	if err != nil {
		return nil, err
	}
	return token, nil
}

// ListByUserID returns the tokens of a user, newest first.
func (r *APITokens) ListByUserID(userID string) ([]model.APIToken, error) {
	tokens := []model.APIToken{}
	err := r.db.Select(&tokens, `
		SELECT * FROM api_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
	`, userID)
	return tokens, err
}

func (r *APITokens) CountByUserID(userID string) (int, error) {
	var count int
	err := r.db.Get(&count, `SELECT COUNT(*) FROM api_tokens WHERE user_id = $1`, userID)
	return count, err
}

// TouchLastUsed records a use. A non-nil expiresAt also moves the
// expiry, for device tokens.
func (r *APITokens) TouchLastUsed(id string, lastUsedAt time.Time, expiresAt *time.Time) error {
	if expiresAt != nil {
		_, err := r.db.Exec(`UPDATE api_tokens SET last_used_at = $1, expires_at = $2 WHERE id = $3`, lastUsedAt, *expiresAt, id)
		return err
	}
	_, err := r.db.Exec(`UPDATE api_tokens SET last_used_at = $1 WHERE id = $2`, lastUsedAt, id)
	return err
}

// DeleteByIDForUser revokes a token. Another user's token is not found.
func (r *APITokens) DeleteByIDForUser(id, userID string) error {
	result, err := r.db.Exec(`DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

// DeleteByUserID revokes every token of a user.
func (r *APITokens) DeleteByUserID(userID string) error {
	_, err := r.db.Exec(`DELETE FROM api_tokens WHERE user_id = $1`, userID)
	return err
}
