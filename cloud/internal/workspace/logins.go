package workspace

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"time"
)

// How long a login waits: a mail link, and a sign-in of the CLI.
const (
	linkLasts     = 15 * time.Minute
	cliLoginLasts = 10 * time.Minute
)

// NewMailLink is the token of a sign-in link for mail, used once.
func (s *Store) NewMailLink(mail string) (string, error) {
	mail, err := CleanMail(mail)
	if err != nil {
		return "", err
	}
	token := newToken()
	_, err = s.db.Exec(`INSERT INTO logins (token_hash, mail, kind, expires) VALUES (?, ?, 'browser', ?)`,
		hash(token), mail, time.Now().Add(linkLasts).Unix())
	return token, err
}

// UseMailLink uses a sign-in link, once, and says which mail it signs in.
func (s *Store) UseMailLink(token string) (string, error) {
	var mail string
	err := s.db.QueryRow(`UPDATE logins SET used = 1
		WHERE token_hash = ? AND kind = 'browser' AND used = 0 AND expires > ? RETURNING mail`,
		hash(token), time.Now().Unix()).Scan(&mail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return mail, err
}

// NewCLILogin starts a sign-in of the CLI. The CLI keeps the secret and
// waits with it; the code is what the terminal and the page both show.
func (s *Store) NewCLILogin() (secret, code string, err error) {
	secret, code = newToken(), newCode()
	_, err = s.db.Exec(`INSERT INTO logins (token_hash, code, kind, expires) VALUES (?, ?, 'cli', ?)`,
		hash(secret), code, time.Now().Add(cliLoginLasts).Unix())
	return secret, code, err
}

// CLILoginWaits says whether a sign-in of the CLI with code waits for a
// browser to allow it.
func (s *Store) CLILoginWaits(code string) bool {
	var found int
	err := s.db.QueryRow(`SELECT 1 FROM logins WHERE code = ? AND kind = 'cli' AND mail = '' AND used = 0 AND expires > ?`,
		code, time.Now().Unix()).Scan(&found)
	return err == nil
}

// AllowCLILogin lets the sign-in of the CLI with code into the workspace
// of mail.
func (s *Store) AllowCLILogin(code, mail string) error {
	result, err := s.db.Exec(`UPDATE logins SET mail = ? WHERE code = ? AND kind = 'cli' AND mail = '' AND used = 0 AND expires > ?`,
		mail, code, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// ErrWaiting is a sign-in of the CLI that no browser allowed yet.
var ErrWaiting = errors.New("waiting for the browser")

// FinishCLILogin is the session token of a sign-in of the CLI a browser
// allowed, handed out once. Before that it is ErrWaiting.
func (s *Store) FinishCLILogin(secret string) (string, error) {
	now := time.Now().Unix()
	var mail string
	err := s.db.QueryRow(`UPDATE logins SET used = 1
		WHERE token_hash = ? AND kind = 'cli' AND mail != '' AND used = 0 AND expires > ? RETURNING mail`,
		hash(secret), now).Scan(&mail)
	if errors.Is(err, sql.ErrNoRows) {
		var waiting int
		if s.db.QueryRow(`SELECT 1 FROM logins WHERE token_hash = ? AND kind = 'cli' AND used = 0 AND expires > ?`, hash(secret), now).Scan(&waiting) == nil {
			return "", ErrWaiting
		}
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	workspace, err := s.SignIn(mail)
	if err != nil {
		return "", err
	}
	return s.StartSession(workspace.ID, "cli")
}

// codeLetters are the letters of a code: no vowels, so a code never
// spells a word, and none that looks like a digit.
const codeLetters = "BCDFGHJKLMNPQRSTVWXZ"

// newCode is a code to compare by eye, like KTRW-PZHM.
func newCode() string {
	random := make([]byte, 8)
	_, _ = rand.Read(random)
	code := make([]byte, 0, 9)
	for i, b := range random {
		if i == 4 {
			code = append(code, '-')
		}
		code = append(code, codeLetters[int(b)%len(codeLetters)])
	}
	return string(code)
}

// DeleteOld removes the logins and sessions that ran out.
func (s *Store) DeleteOld() error {
	now := time.Now().Unix()
	if _, err := s.db.Exec(`DELETE FROM logins WHERE expires <= ?`, now); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires <= ?`, now)
	return err
}
