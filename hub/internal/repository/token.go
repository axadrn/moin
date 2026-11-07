package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

var (
	ErrTokenNotFound = errors.New("token not found")
	ErrTokenExpired  = errors.New("token has expired")
	ErrTokenUsed     = errors.New("token has already been used")
)

// Tokens stores the one-time tokens that email links carry: magic links,
// email verification, password resets and email changes.
type Tokens struct {
	db *sqlx.DB
}

func NewTokens(db *sqlx.DB) *Tokens {
	return &Tokens{db: db}
}

func (r *Tokens) Create(token *model.Token) error {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now()
	}

	query := `
		INSERT INTO tokens (id, user_id, type, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(query,
		token.ID,
		token.UserID,
		token.Type,
		token.TokenHash,
		token.ExpiresAt,
		token.CreatedAt,
	)
	return err
}

// ConsumeToken atomically deletes and returns a valid single-use token.
func (r *Tokens) ConsumeToken(tokenHash string) (*model.Token, error) {
	var t model.Token
	now := time.Now()

	query := `
		DELETE FROM tokens
		WHERE token_hash = $1
		AND expires_at > $2
		RETURNING *
	`

	err := r.db.Get(&t, query, tokenHash, now)
	if err == sql.ErrNoRows {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, err
	}

	return &t, nil
}

func (r *Tokens) DeleteByUserAndType(userID, tokenType string) error {
	query := `DELETE FROM tokens WHERE user_id = $1 AND type = $2`
	_, err := r.db.Exec(query, userID, tokenType)
	return err
}

func (r *Tokens) DeleteExpired(now time.Time) error {
	_, err := r.db.Exec(`DELETE FROM tokens WHERE expires_at <= $1`, now)
	return err
}
