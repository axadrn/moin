// Package workspace keeps the workspaces of moin cloud in the database:
// who signed in, their sessions, the logins that wait, and the servers
// that report. A workspace's mail is its account.
package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

// ErrNotFound is a token, code or server that is not there, ran out, or
// was used.
var ErrNotFound = errors.New("not found")

// Workspace is one customer of moin cloud.
type Workspace struct {
	ID   string
	Mail string
	// Status is trial, paid or ended.
	Status string
	// TrialDays is how long the trial lasts: 30, or an early backer's
	// free year.
	TrialDays int
	// TrialEnds is zero until the first report starts the trial.
	TrialEnds time.Time
	PaidUntil time.Time
	Coupon    string
	// StripeCustomer and StripeSubscription are the customer and the
	// subscription in Stripe, once there are.
	StripeCustomer     string
	StripeSubscription string
}

// AlertsOn says whether alerts go out at now: when paid, and during the
// trial. After the trial reports still count, but nobody is told.
func (w Workspace) AlertsOn(now time.Time) bool {
	switch w.Status {
	case "paid":
		return true
	case "trial":
		return w.TrialEnds.IsZero() || now.Before(w.TrialEnds)
	}
	return false
}

// TrialOver says whether the trial ran out and nothing is paid.
func (w Workspace) TrialOver(now time.Time) bool {
	return w.Status != "paid" && !w.AlertsOn(now)
}

// Store reads and writes the workspaces.
type Store struct {
	db *sql.DB
}

// NewStore keeps the workspaces in db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CleanMail is a mail as moin cloud keeps it: trimmed and lowercase. It
// refuses what is not a plain address.
func CleanMail(text string) (string, error) {
	address := strings.ToLower(strings.TrimSpace(text))
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address || len(address) > 254 {
		return "", fmt.Errorf("%q is not an email address", text)
	}
	return address, nil
}

// SignIn is the workspace of mail. The first sign-in with a mail makes it.
func (s *Store) SignIn(mail string) (Workspace, error) {
	mail, err := CleanMail(mail)
	if err != nil {
		return Workspace{}, err
	}
	_, err = s.db.Exec(`INSERT INTO workspaces (id, mail, created) VALUES (?, ?, ?) ON CONFLICT (mail) DO NOTHING`,
		newToken(), mail, time.Now().Unix())
	if err != nil {
		return Workspace{}, err
	}
	return s.one(`SELECT `+columns+` FROM workspaces WHERE mail = ?`, mail)
}

// Workspace is the workspace with id.
func (s *Store) Workspace(id string) (Workspace, error) {
	return s.one(`SELECT `+columns+` FROM workspaces WHERE id = ?`, id)
}

// columns are what one reads into a Workspace.
const columns = `workspaces.id, workspaces.mail, workspaces.status, workspaces.trial_days, coalesce(workspaces.trial_ends, 0),
	coalesce(workspaces.paid_until, 0), coalesce(workspaces.coupon, ''),
	coalesce(workspaces.stripe_customer, ''), coalesce(workspaces.stripe_subscription, '')`

// one reads one workspace.
func (s *Store) one(query string, args ...any) (Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

// scanWorkspace reads the columns of a workspace, and then more.
func scanWorkspace(row interface{ Scan(...any) error }, more ...any) (Workspace, error) {
	var w Workspace
	var trialEnds, paidUntil int64
	err := row.Scan(append([]any{&w.ID, &w.Mail, &w.Status, &w.TrialDays, &trialEnds, &paidUntil, &w.Coupon, &w.StripeCustomer, &w.StripeSubscription}, more...)...)
	w.TrialEnds, w.PaidUntil = unix(trialEnds), unix(paidUntil)
	return w, err
}

// StartTrial starts the trial of a workspace that has none yet, at start:
// the first report of its first server does. It lasts the workspace's
// trial days.
func (s *Store) StartTrial(id string, start time.Time) error {
	_, err := s.db.Exec(`UPDATE workspaces SET trial_ends = ? + trial_days * 86400 WHERE id = ? AND status = 'trial' AND trial_ends IS NULL`, start.Unix(), id)
	return err
}

// DeleteWorkspace deletes a workspace with its sessions, servers and
// their tokens, and the logins of its mail.
func (s *Store) DeleteWorkspace(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var mail string
	if err := tx.QueryRow(`DELETE FROM workspaces WHERE id = ? RETURNING mail`, id).Scan(&mail); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(`DELETE FROM logins WHERE mail = ?`, mail); err != nil {
		return err
	}
	return tx.Commit()
}

// TrialMail is a mail about the trial that is due.
type TrialMail struct {
	Workspace Workspace
	// Day is 25, five days before the end, or 30, at the end.
	Day int
}

// TrialMailsDue are the trial mails due at now, at most one per workspace.
func (s *Store) TrialMailsDue(now time.Time) ([]TrialMail, error) {
	rows, err := s.db.Query(`SELECT `+columns+`, CASE WHEN trial_ends <= ? THEN 30 ELSE 25 END FROM workspaces
		WHERE status = 'trial' AND trial_ends IS NOT NULL
		AND ((trial_ends <= ? AND trial_mails < 30) OR (trial_ends - ? <= ? AND trial_mails < 25))`,
		now.Unix(), now.Unix(), int64((5 * 24 * time.Hour).Seconds()), now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var due []TrialMail
	for rows.Next() {
		var m TrialMail
		var err error
		if m.Workspace, err = scanWorkspace(rows, &m.Day); err != nil {
			return nil, err
		}
		due = append(due, m)
	}
	return due, rows.Err()
}

// TrialMailSent notes that the trial mail of day went out.
func (s *Store) TrialMailSent(id string, day int) error {
	_, err := s.db.Exec(`UPDATE workspaces SET trial_mails = ? WHERE id = ?`, day, id)
	return err
}

// WatcherState is what the watcher of a workspace saved.
func (s *Store) WatcherState(id string) ([]byte, error) {
	var state []byte
	err := s.db.QueryRow(`SELECT watcher_state FROM workspaces WHERE id = ?`, id).Scan(&state)
	return state, err
}

// SaveWatcherState keeps what the watcher of a workspace knows.
func (s *Store) SaveWatcherState(id string, state []byte) error {
	_, err := s.db.Exec(`UPDATE workspaces SET watcher_state = ? WHERE id = ?`, state, id)
	return err
}

// newToken is 128 random bits and more, as text.
func newToken() string {
	return rand.Text()
}

// hash is what the database keeps of a token: its SHA-256, in hex.
func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// unix is a time of the database; 0 is the zero time.
func unix(seconds int64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}
