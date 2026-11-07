package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

var ErrSessionNotFound = errors.New("session not found")

// Sessions stores the sign-in sessions of the web and the API.
type Sessions struct {
	db *sqlx.DB
}

func NewSessions(db *sqlx.DB) *Sessions {
	return &Sessions{db: db}
}

func (r *Sessions) Create(session *model.Session) error {
	_, err := r.db.Exec(
		insertSessionQuery,
		session.ID,
		session.UserID,
		session.ImpersonatorUserID,
		session.TokenHash,
		session.UserAgent,
		session.ExpiresAt,
		session.LastSeenAt,
		session.CreatedAt,
	)
	return err
}

const insertSessionQuery = `
		INSERT INTO sessions (
			id, user_id, impersonator_user_id, token_hash, user_agent, expires_at, last_seen_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

func (r *Sessions) ReplaceAllForUser(session *model.Session) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = $1 OR impersonator_user_id = $1`, session.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		insertSessionQuery,
		session.ID,
		session.UserID,
		session.ImpersonatorUserID,
		session.TokenHash,
		session.UserAgent,
		session.ExpiresAt,
		session.LastSeenAt,
		session.CreatedAt,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Sessions) ByTokenHash(tokenHash string) (*model.Session, error) {
	session := &model.Session{}
	err := r.db.Get(session, `SELECT * FROM sessions WHERE token_hash = $1`, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	return session, err
}

func (r *Sessions) ListByUserID(userID string, now time.Time) ([]model.Session, error) {
	if _, err := r.db.Exec(`DELETE FROM sessions WHERE user_id = $1 AND expires_at <= $2`, userID, now); err != nil {
		return nil, err
	}

	sessions := []model.Session{}
	err := r.db.Select(&sessions, `
		SELECT * FROM sessions
		WHERE user_id = $1
		ORDER BY last_seen_at DESC, created_at DESC
	`, userID)
	return sessions, err
}

func (r *Sessions) UpdateLastSeen(id string, lastSeenAt time.Time) error {
	result, err := r.db.Exec(`UPDATE sessions SET last_seen_at = $1 WHERE id = $2`, lastSeenAt, id)
	if err != nil {
		return err
	}
	return requireSessionRow(result)
}

func (r *Sessions) DeleteByTokenHash(tokenHash string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (r *Sessions) DeleteByIDForUser(id, userID string) error {
	result, err := r.db.Exec(`DELETE FROM sessions WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	return requireSessionRow(result)
}

func (r *Sessions) DeleteOthersForUser(userID, keepSessionID string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE (user_id = $1 OR impersonator_user_id = $1) AND id <> $2`, userID, keepSessionID)
	return err
}

func (r *Sessions) DeleteAllForUser(userID string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE user_id = $1 OR impersonator_user_id = $1`, userID)
	return err
}

func (r *Sessions) DeleteExpired(now time.Time) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE expires_at <= $1`, now)
	return err
}

func requireSessionRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrSessionNotFound
	}
	return nil
}
