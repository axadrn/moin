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
	// TrialEnds is zero until the first report starts the trial.
	TrialEnds time.Time
	PaidUntil time.Time
	Coupon    string
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
const columns = `id, mail, status, coalesce(trial_ends, 0), coalesce(paid_until, 0), coalesce(coupon, '')`

// one reads one workspace.
func (s *Store) one(query string, args ...any) (Workspace, error) {
	var w Workspace
	var trialEnds, paidUntil int64
	err := s.db.QueryRow(query, args...).Scan(&w.ID, &w.Mail, &w.Status, &trialEnds, &paidUntil, &w.Coupon)
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	w.TrialEnds, w.PaidUntil = unix(trialEnds), unix(paidUntil)
	return w, err
}

// StartTrial starts the trial of a workspace that has none yet: the first
// report of its first server does.
func (s *Store) StartTrial(id string, ends time.Time) error {
	_, err := s.db.Exec(`UPDATE workspaces SET trial_ends = ? WHERE id = ? AND status = 'trial' AND trial_ends IS NULL`, ends.Unix(), id)
	return err
}

// WatcherState is what the watcher of a workspace saved, and where its
// alerts go besides the mail, as JSON.
func (s *Store) WatcherState(id string) (state []byte, alerts string, err error) {
	err = s.db.QueryRow(`SELECT watcher_state, alerts FROM workspaces WHERE id = ?`, id).Scan(&state, &alerts)
	return state, alerts, err
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
