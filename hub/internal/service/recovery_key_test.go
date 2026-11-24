package service

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/testutil"
)

// A recovery key is 160 random bits in groups of four. It signs in once,
// typed in any case, and a second key replaces the first.
func TestRecoveryKeyWorksOnce(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})

	if _, err := environment.auth.CreateRecoveryKey("nobody@example.com"); err == nil {
		t.Fatal("a key for an address without an account")
	}
	replaced, err := environment.auth.CreateRecoveryKey("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	key, err := environment.auth.CreateRecoveryKey("ADA@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[A-Z2-7]{4}(-[A-Z2-7]{4}){7}$`).MatchString(key) {
		t.Fatalf("key = %q, want 32 base32 letters in groups of four", key)
	}
	if _, err := environment.auth.SignInWithRecoveryKey(replaced); !errors.Is(err, ErrRecoveryKeyInvalid) {
		t.Fatalf("the replaced key: error = %v", err)
	}
	if _, err := environment.auth.SignInWithRecoveryKey("AAAA-BBBB"); !errors.Is(err, ErrRecoveryKeyInvalid) {
		t.Fatalf("a wrong key: error = %v", err)
	}

	user, err := environment.auth.SignInWithRecoveryKey(" " + strings.ToLower(key) + " ")
	if err != nil || user.ID != "ada" {
		t.Fatalf("SignInWithRecoveryKey() = %v, %v", user, err)
	}
	if _, err := environment.auth.SignInWithRecoveryKey(key); !errors.Is(err, ErrRecoveryKeyInvalid) {
		t.Fatalf("the used key: error = %v", err)
	}
}

func TestRecoveryKeyExpiresAfterAnHour(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	start := time.Now()
	environment.auth.now = func() time.Time { return start }
	key, err := environment.auth.CreateRecoveryKey("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}

	environment.auth.now = func() time.Time { return start.Add(time.Hour + time.Second) }
	if _, err := environment.auth.SignInWithRecoveryKey(key); !errors.Is(err, ErrRecoveryKeyInvalid) {
		t.Fatalf("after an hour: error = %v", err)
	}
	environment.auth.now = func() time.Time { return start.Add(59 * time.Minute) }
	if _, err := environment.auth.SignInWithRecoveryKey(key); err != nil {
		t.Fatalf("within the hour: error = %v", err)
	}
}
