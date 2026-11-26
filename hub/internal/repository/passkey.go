package repository

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrPasskeyNotFound = errors.New("passkey not found")
	// ErrPasskeyTaken says the passkey belongs to an account already.
	ErrPasskeyTaken = errors.New("passkey already added")
	// ErrPasskeyLastWayIn refuses to remove the only way the user signs in:
	// their last passkey, without GitHub.
	ErrPasskeyLastWayIn = errors.New("last way in")
	// ErrCeremonyNotFound says a passkey ceremony is unknown, used or
	// expired.
	ErrCeremonyNotFound = errors.New("passkey ceremony not found")
)

// Passkeys stores the passkeys of the users and the ceremonies that add
// them or sign in with them.
type Passkeys struct {
	db *sqlx.DB
}

func NewPasskeys(db *sqlx.DB) *Passkeys {
	return &Passkeys{db: db}
}

// Create stores the passkey while sessionID is a live session of its user.
// One statement, so a recovery key, which ends every session and deletes
// every passkey at once, never leaves a passkey added from an old session:
// such an add stores nothing and is ErrSessionNotFound.
func (r *Passkeys) Create(passkey *model.Passkey, sessionID string) error {
	result, err := r.db.Exec(`INSERT INTO passkeys (id, user_id, credential_id, credential, name, created_at)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE EXISTS (SELECT 1 FROM sessions WHERE id = $7 AND user_id = $2 AND expires_at > $6)`,
		passkey.ID, passkey.UserID, passkey.CredentialID, passkey.Credential, passkey.Name, passkey.CreatedAt, sessionID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrPasskeyTaken
	}
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// ByUser is every passkey of the user, oldest first.
func (r *Passkeys) ByUser(userID string) ([]model.Passkey, error) {
	passkeys := []model.Passkey{}
	err := r.db.Select(&passkeys, `SELECT * FROM passkeys WHERE user_id = $1 ORDER BY created_at, id`, userID)
	return passkeys, err
}

// Used stores the credential after a sign-in, with its new sign counter
// and flags.
func (r *Passkeys) Used(id, credential string, at time.Time) error {
	_, err := r.db.Exec(`UPDATE passkeys SET credential = $1, last_used_at = $2 WHERE id = $3`, credential, at, id)
	return err
}

// Rename names the user's passkey and returns it as it is now.
func (r *Passkeys) Rename(userID, id, name string) (*model.Passkey, error) {
	passkey := &model.Passkey{}
	err := r.db.Get(passkey, `UPDATE passkeys SET name = $1 WHERE id = $2 AND user_id = $3 RETURNING *`, name, id, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPasskeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return passkey, nil
}

// Delete removes the user's passkey, unless it is their last way in: their
// only passkey, without GitHub. One statement, so two removals at once
// never leave the user without one.
func (r *Passkeys) Delete(userID, id string) error {
	result, err := r.db.Exec(`DELETE FROM passkeys WHERE id = $1 AND user_id = $2
		AND (EXISTS (SELECT 1 FROM users WHERE id = $2 AND github_id IS NOT NULL)
			OR (SELECT COUNT(*) FROM passkeys WHERE user_id = $2) > 1)`, id, userID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows > 0 {
		return err
	}
	var found bool
	if err := r.db.Get(&found, `SELECT EXISTS (SELECT 1 FROM passkeys WHERE id = $1 AND user_id = $2)`, id, userID); err != nil {
		return err
	}
	if found {
		return ErrPasskeyLastWayIn
	}
	return ErrPasskeyNotFound
}

// StartCeremony keeps the session data of a ceremony under the hash of
// its id until expiresAt, and drops the ceremonies that expired before
// now.
func (r *Passkeys) StartCeremony(idHash, sessionData string, expiresAt, now time.Time) error {
	if _, err := r.db.Exec(`DELETE FROM passkey_ceremonies WHERE expires_at <= $1`, now); err != nil {
		return err
	}
	_, err := r.db.Exec(`INSERT INTO passkey_ceremonies (id_hash, session_data, expires_at) VALUES ($1, $2, $3)`, idHash, sessionData, expiresAt)
	return err
}

// TakeCeremony deletes the ceremony and returns its session data, if it
// has not expired at now. One statement, so a ceremony works only once.
func (r *Passkeys) TakeCeremony(idHash string, now time.Time) (string, error) {
	var sessionData string
	var expiresAt time.Time
	err := r.db.QueryRowx(`DELETE FROM passkey_ceremonies WHERE id_hash = $1 RETURNING session_data, expires_at`, idHash).Scan(&sessionData, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCeremonyNotFound
	}
	if err != nil {
		return "", err
	}
	if !expiresAt.After(now) {
		return "", ErrCeremonyNotFound
	}
	return sessionData, nil
}
