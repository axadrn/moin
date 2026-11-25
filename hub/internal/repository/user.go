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
	ErrUserNotFound   = errors.New("user not found")
	ErrDuplicateEmail = errors.New("email already exists")
	// ErrGitHubTaken says another user signs in with this GitHub account,
	// or the user has one already.
	ErrGitHubTaken = errors.New("github account already connected")
)

// Users stores the user accounts.
type Users struct {
	db *sqlx.DB
}

func NewUsers(db *sqlx.DB) *Users {
	return &Users{db: db}
}

func (r *Users) Create(user *model.User) error {
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = user.CreatedAt
	}
	query := `INSERT INTO users (id, email, email_verified_at, name, github_id, github_login, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, err := r.db.Exec(query, user.ID, user.Email, user.EmailVerifiedAt, user.Name, user.GitHubID, user.GitHubLogin, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE constraint failed: users.github_id") {
			return ErrGitHubTaken
		}
		if strings.Contains(errStr, "UNIQUE constraint failed") {
			return ErrDuplicateEmail
		}
		return err
	}

	return nil
}

// userColumns are the columns of a model.User: the row and how many
// passkeys the user has.
const userColumns = `users.*, (SELECT COUNT(*) FROM passkeys WHERE passkeys.user_id = users.id) AS passkeys`

func (r *Users) ByID(id string) (*model.User, error) {
	user := &model.User{}
	query := `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	err := r.db.Get(user, query, id)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}

	return user, err
}

func (r *Users) ByEmail(email string) (*model.User, error) {
	user := &model.User{}
	query := `SELECT ` + userColumns + ` FROM users WHERE email = $1`

	err := r.db.Get(user, query, email)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}

	return user, err
}

// ByGitHubID is the user who signs in with the GitHub account id.
func (r *Users) ByGitHubID(id int64) (*model.User, error) {
	user := &model.User{}
	err := r.db.Get(user, `SELECT `+userColumns+` FROM users WHERE github_id = $1`, id)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	return user, err
}

func (r *Users) Update(user *model.User) error {
	query := `UPDATE users SET email = $1, pending_email = $2, email_verified_at = $3, name = $4, updated_at = $5 WHERE id = $6`

	user.UpdatedAt = time.Now()
	_, err := r.db.Exec(query, user.Email, user.PendingEmail, user.EmailVerifiedAt, user.Name, user.UpdatedAt, user.ID)
	return err
}

// ConnectGitHub stores the GitHub account of a user who has none yet. It
// is ErrGitHubTaken when the user has one already or another user signs in
// with this one.
func (r *Users) ConnectGitHub(userID string, githubID int64, login string) error {
	result, err := r.db.Exec(`UPDATE users SET github_id = $1, github_login = $2, updated_at = $3 WHERE id = $4 AND github_id IS NULL`, githubID, login, time.Now(), userID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrGitHubTaken
		}
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return ErrGitHubTaken
	}
	return nil
}

// SetGitHubLogin keeps the name of the user's GitHub account fresh: it may
// change on GitHub, the id never does.
func (r *Users) SetGitHubLogin(userID, login string) error {
	_, err := r.db.Exec(`UPDATE users SET github_login = $1 WHERE id = $2`, login, userID)
	return err
}

// SetRecoveryKey stores the hash of a new recovery key of the user, in
// place of one the user had.
func (r *Users) SetRecoveryKey(userID, hash string, expiresAt time.Time) error {
	result, err := r.db.Exec(`UPDATE users SET recovery_key_hash = $1, recovery_key_expires_at = $2 WHERE id = $3`, hash, expiresAt, userID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// Recovered is what a recovery key ended besides the sessions and the
// passkeys: the connections and CI tokens of its user, for the activity.
type Recovered struct {
	UserID      string
	Connections []model.Connection
	CITokens    []model.CIToken
}

// UseRecoveryKey clears the recovery key with this hash, if it has not
// expired at now, disconnects the user's GitHub account, ends their
// sessions and connections, deletes their passkeys and CI tokens and
// returns what it ended. One transaction, so a key works only once, a lost
// or hijacked GitHub account or passkey no longer signs in, nothing it
// let in keeps working, and no old session adds a passkey after it
// (Passkeys.Create). An OAuth grant is a connection: it ends with it.
func (r *Users) UseRecoveryKey(hash string, now time.Time) (Recovered, error) {
	var recovered Recovered
	tx, err := r.db.Beginx()
	if err != nil {
		return recovered, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	err = tx.Get(&recovered.UserID, `UPDATE users SET recovery_key_hash = NULL, recovery_key_expires_at = NULL, github_id = NULL, github_login = '', github_disconnected_at = $2 WHERE recovery_key_hash = $1 AND recovery_key_expires_at > $2 RETURNING id`, hash, now)
	if err == sql.ErrNoRows {
		return recovered, ErrUserNotFound
	}
	if err != nil {
		return recovered, err
	}
	userID := recovered.UserID
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = $1 OR impersonator_user_id = $1`, userID); err != nil {
		return recovered, err
	}
	if _, err := tx.Exec(`DELETE FROM passkeys WHERE user_id = $1`, userID); err != nil {
		return recovered, err
	}
	if err := tx.Select(&recovered.Connections, `SELECT * FROM connections WHERE user_id = $1 AND ended_at IS NULL`, userID); err != nil {
		return recovered, err
	}
	if _, err := tx.Exec(`DELETE FROM connection_tokens WHERE connection_id IN (SELECT id FROM connections WHERE user_id = $1)`, userID); err != nil {
		return recovered, err
	}
	if _, err := tx.Exec(`UPDATE connections SET ended_at = $2 WHERE user_id = $1 AND ended_at IS NULL`, userID, now); err != nil {
		return recovered, err
	}
	if err := tx.Select(&recovered.CITokens, ciTokenSelect+` WHERE t.user_id = $1`, userID); err != nil {
		return recovered, err
	}
	if _, err := tx.Exec(`DELETE FROM ci_tokens WHERE user_id = $1`, userID); err != nil {
		return recovered, err
	}
	return recovered, tx.Commit()
}

func (r *Users) UpdateName(userID, name string, updatedAt time.Time) error {
	result, err := r.db.Exec(`UPDATE users SET name = $1, updated_at = $2 WHERE id = $3`, name, updatedAt, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetEmailAlerts turns the email alerts of the user on or off.
func (r *Users) SetEmailAlerts(userID string, on bool) error {
	result, err := r.db.Exec(`UPDATE users SET email_alerts = $1 WHERE id = $2`, on, userID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *Users) Delete(id string) error {
	query := `DELETE FROM users WHERE id = $1`

	result, err := r.db.Exec(query, id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrUserNotFound
	}

	return nil
}
