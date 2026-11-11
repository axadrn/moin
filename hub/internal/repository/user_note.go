package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// UserNote is what admins wrote about the user; nil when nothing.
func (r *Admin) UserNote(userID string) (*model.UserNote, error) {
	var note model.UserNote
	err := r.db.Get(&note, `
		SELECT user_notes.*, COALESCE(users.email, '') AS updated_by_email
		FROM user_notes LEFT JOIN users ON users.id = user_notes.updated_by
		WHERE user_notes.user_id = $1
	`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &note, nil
}

// SetUserNote keeps the note on the user, or removes it when it is empty.
func (r *Admin) SetUserNote(userID, note, updatedBy string, now time.Time) error {
	if note == "" {
		_, err := r.db.Exec(`DELETE FROM user_notes WHERE user_id = $1`, userID)
		return err
	}
	_, err := r.db.Exec(`
		INSERT INTO user_notes (user_id, note, updated_by, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET note = excluded.note, updated_by = excluded.updated_by, updated_at = excluded.updated_at
	`, userID, note, updatedBy, now)
	return err
}
