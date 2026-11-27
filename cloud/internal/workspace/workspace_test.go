package workspace

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/cloud/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewStore(database)
}

// The first sign-in makes the workspace; the mail is trimmed and lowercase.
func TestSignIn(t *testing.T) {
	s := testStore(t)
	first, err := s.SignIn(" Ada@Example.com")
	if err != nil || first.Mail != "ada@example.com" || first.Status != "trial" || !first.TrialEnds.IsZero() {
		t.Fatalf("first = %+v, %v", first, err)
	}
	again, err := s.SignIn("ada@example.com")
	if err != nil || again.ID != first.ID {
		t.Fatalf("again = %+v, %v", again, err)
	}
	for _, bad := range []string{"", "ada", "Ada <ada@example.com>", "ada@example.com, bob@example.com"} {
		if _, err := s.SignIn(bad); err == nil {
			t.Errorf("signed in %q", bad)
		}
	}
}

// Only the hash of a token is stored; a session ends when it is signed out.
func TestSessions(t *testing.T) {
	s := testStore(t)
	space, _ := s.SignIn("ada@example.com")
	token, err := s.StartSession(space.ID, "browser")
	if err != nil || len(token) < 26 {
		t.Fatalf("token %q, %v", token, err)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil || stored == token || stored != hash(token) {
		t.Errorf("stored %q for token %q", stored, token)
	}
	if got, err := s.SessionWorkspace(token); err != nil || got.ID != space.ID {
		t.Errorf("session = %+v, %v", got, err)
	}
	if _, err := s.SessionWorkspace("wrong"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a wrong token: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET expires = ?`, time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionWorkspace(token); !errors.Is(err, ErrNotFound) {
		t.Errorf("an old session: %v", err)
	}
	token, _ = s.StartSession(space.ID, "cli")
	if err := s.EndSession(token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionWorkspace(token); !errors.Is(err, ErrNotFound) {
		t.Errorf("a session signed out: %v", err)
	}
}

// A mail link works once and not after 15 minutes.
func TestMailLink(t *testing.T) {
	s := testStore(t)
	token, err := s.NewMailLink("Ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if mail, err := s.UseMailLink(token); err != nil || mail != "ada@example.com" {
		t.Fatalf("mail %q, %v", mail, err)
	}
	if _, err := s.UseMailLink(token); !errors.Is(err, ErrNotFound) {
		t.Errorf("used twice: %v", err)
	}
	token, _ = s.NewMailLink("ada@example.com")
	if _, err := s.db.Exec(`UPDATE logins SET expires = ?`, time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UseMailLink(token); !errors.Is(err, ErrNotFound) {
		t.Errorf("an old link: %v", err)
	}
}

// A CLI login waits for a browser, then hands its session out once.
func TestCLILogin(t *testing.T) {
	s := testStore(t)
	secret, code, err := s.NewCLILogin()
	if err != nil || !regexp.MustCompile(`^[B-Z]{4}-[B-Z]{4}$`).MatchString(code) || strings.ContainsAny(code, "AEIOUY") {
		t.Fatalf("code %q, %v", code, err)
	}
	if _, err := s.FinishCLILogin(secret); !errors.Is(err, ErrWaiting) {
		t.Fatalf("before the browser: %v", err)
	}
	if !s.CLILoginWaits(code) || s.CLILoginWaits("BCDF-GHJK") {
		t.Error("waits says wrong")
	}
	if err := s.AllowCLILogin(code, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.AllowCLILogin(code, "eve@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("allowed twice: %v", err)
	}
	token, err := s.FinishCLILogin(secret)
	if err != nil {
		t.Fatal(err)
	}
	if space, err := s.SessionWorkspace(token); err != nil || space.Mail != "ada@example.com" {
		t.Errorf("session = %+v, %v", space, err)
	}
	if _, err := s.FinishCLILogin(secret); !errors.Is(err, ErrNotFound) {
		t.Errorf("handed out twice: %v", err)
	}
}

// A server connected again gets a new token; the old one stops working.
func TestServers(t *testing.T) {
	s := testStore(t)
	space, _ := s.SignIn("ada@example.com")
	first, err := s.ConnectServer(space.ID, "prod")
	if err != nil {
		t.Fatal(err)
	}
	second, _ := s.ConnectServer(space.ID, "prod")
	if _, _, err := s.ServerByToken(first); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old token: %v", err)
	}
	if id, name, err := s.ServerByToken(second); err != nil || id != space.ID || name != "prod" {
		t.Errorf("server = %s %s, %v", id, name, err)
	}
	if names, _ := s.ServerNames(space.ID); len(names) != 1 {
		t.Errorf("names = %v", names)
	}
	if err := s.DisconnectServer(space.ID, "prod"); err != nil {
		t.Fatal(err)
	}
	if err := s.DisconnectServer(space.ID, "prod"); !errors.Is(err, ErrNotFound) {
		t.Errorf("disconnected twice: %v", err)
	}
}
