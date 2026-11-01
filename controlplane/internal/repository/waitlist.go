package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

type WaitlistRepository interface {
	// Join adds an address; joining twice keeps the first date.
	Join(email string, now time.Time) error
	// LetIn adds the address if needed and lets it in.
	LetIn(email string, foreverFree bool, now time.Time) error
	// Get returns the entry of an address, nil when it is not on the list.
	Get(email string) (*model.WaitlistEntry, error)
	List() ([]model.WaitlistEntry, error)
	// HasPendingInvitation reports whether a workspace invited the address
	// and the invitation is still open.
	HasPendingInvitation(email string, now time.Time) (bool, error)
}

type waitlistRepository struct {
	db *sqlx.DB
}

func NewWaitlistRepository(db *sqlx.DB) WaitlistRepository {
	return &waitlistRepository{db: db}
}

func (r *waitlistRepository) Join(email string, now time.Time) error {
	_, err := r.db.Exec(`INSERT INTO waitlist (email, created_at) VALUES ($1, $2) ON CONFLICT (email) DO NOTHING`, email, now)
	return err
}

func (r *waitlistRepository) LetIn(email string, foreverFree bool, now time.Time) error {
	_, err := r.db.Exec(`
		INSERT INTO waitlist (email, created_at, let_in_at, forever_free) VALUES ($1, $2, $2, $3)
		ON CONFLICT (email) DO UPDATE SET let_in_at = COALESCE(waitlist.let_in_at, excluded.let_in_at), forever_free = excluded.forever_free
	`, email, now, foreverFree)
	return err
}

func (r *waitlistRepository) Get(email string) (*model.WaitlistEntry, error) {
	var entry model.WaitlistEntry
	err := r.db.Get(&entry, `SELECT * FROM waitlist WHERE email = $1`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *waitlistRepository) List() ([]model.WaitlistEntry, error) {
	entries := []model.WaitlistEntry{}
	err := r.db.Select(&entries, `SELECT * FROM waitlist ORDER BY let_in_at IS NOT NULL, created_at`)
	return entries, err
}

func (r *waitlistRepository) HasPendingInvitation(email string, now time.Time) (bool, error) {
	var count int
	err := r.db.Get(&count, `
		SELECT COUNT(*) FROM workspace_invitations
		WHERE LOWER(email) = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2
	`, email, now)
	return count > 0, err
}
