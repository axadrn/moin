package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

var ErrDeviceLoginNotFound = errors.New("device login not found")

// DeviceLogins stores the pending logins of `moin login` in the browser.
type DeviceLogins struct {
	db *sqlx.DB
}

func NewDeviceLogins(db *sqlx.DB) *DeviceLogins {
	return &DeviceLogins{db: db}
}

// Create stores a new pending login.
func (r *DeviceLogins) Create(login model.DeviceLogin) error {
	_, err := r.db.Exec(`
		INSERT INTO device_logins (id, device_code_hash, user_code_hash, hostname, status, interval_seconds, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, login.ID, login.DeviceCodeHash, login.UserCodeHash, login.Hostname, login.Status, login.IntervalSeconds, login.ExpiresAt, login.CreatedAt)
	return err
}

// ByDeviceCodeHash finds the login the CLI polls for.
func (r *DeviceLogins) ByDeviceCodeHash(hash string) (*model.DeviceLogin, error) {
	return r.one(`SELECT * FROM device_logins WHERE device_code_hash = $1`, hash)
}

// ByUserCodeHash finds the login the user opened on the web.
func (r *DeviceLogins) ByUserCodeHash(hash string) (*model.DeviceLogin, error) {
	return r.one(`SELECT * FROM device_logins WHERE user_code_hash = $1`, hash)
}

func (r *DeviceLogins) one(query, arg string) (*model.DeviceLogin, error) {
	var login model.DeviceLogin
	err := r.db.Get(&login, query, arg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeviceLoginNotFound
	}
	if err != nil {
		return nil, err
	}
	return &login, nil
}

// Polled records a poll of the CLI and the interval it must keep from now on.
func (r *DeviceLogins) Polled(id string, at time.Time, intervalSeconds int) error {
	_, err := r.db.Exec(`UPDATE device_logins SET last_polled_at = $1, interval_seconds = $2 WHERE id = $3`, at, intervalSeconds, id)
	return err
}

// Decide approves or denies a pending login for a user. A login that is no
// longer pending is not found, so a code is decided only once.
func (r *DeviceLogins) Decide(id, status, userID string, workspaceID *string) error {
	result, err := r.db.Exec(`
		UPDATE device_logins SET status = $1, user_id = $2, workspace_id = $3
		WHERE id = $4 AND status = 'pending'
	`, status, userID, workspaceID, id)
	return oneRow(result, err)
}

// Claim deletes a decided login. Only the first of two polls at the same
// time gets it, so an approval gives exactly one token.
func (r *DeviceLogins) Claim(id, status string) error {
	result, err := r.db.Exec(`DELETE FROM device_logins WHERE id = $1 AND status = $2`, id, status)
	return oneRow(result, err)
}

// Delete removes a login.
func (r *DeviceLogins) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM device_logins WHERE id = $1`, id)
	return err
}

// DeleteExpired removes the logins that ran out of time.
func (r *DeviceLogins) DeleteExpired(now time.Time) error {
	_, err := r.db.Exec(`DELETE FROM device_logins WHERE expires_at <= $1`, now)
	return err
}

func oneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrDeviceLoginNotFound
	}
	return nil
}
