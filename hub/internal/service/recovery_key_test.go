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

// A recovery key is the way back from a lost or stolen GitHub account: it
// disconnects GitHub, and the old GitHub account does not get in again,
// not even by its email. The settings connect the new one.
func TestRecoveryKeyDisconnectsGitHub(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	old := GitHubAccount{ID: 1, Login: "old", Email: "ada@example.com"}
	if user, err := environment.auth.SignInWithGitHub(old, ""); err != nil || user.ID != "ada" {
		t.Fatalf("first sign-in = %v, %v", user, err)
	}
	key, err := environment.auth.CreateRecoveryKey("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}

	user, err := environment.auth.SignInWithRecoveryKey(key)
	if err != nil || user.GitHubID != nil || user.GitHubLogin != "" || user.GitHubDisconnectedAt == nil {
		t.Fatalf("after the key = %+v, %v, want GitHub disconnected", user, err)
	}
	if _, err := environment.auth.SignInWithGitHub(old, ""); !errors.Is(err, ErrGitHubEmailTaken) {
		t.Fatalf("the old GitHub account: error = %v, want ErrGitHubEmailTaken", err)
	}
	fresh := GitHubAccount{ID: 2, Login: "new", Email: "other@example.com"}
	if err := environment.auth.ConnectGitHub("ada", fresh); err != nil {
		t.Fatalf("ConnectGitHub() error = %v", err)
	}
	if user, err := environment.auth.SignInWithGitHub(fresh, ""); err != nil || user.ID != "ada" {
		t.Fatalf("the new GitHub account = %v, %v", user, err)
	}
}

// A recovery key drops an email change asked for before it: its link no
// longer moves the account to the other address.
func TestRecoveryKeyDropsAPendingEmailChange(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	if err := environment.auth.RequestEmailChange("ada", "mallory@example.com"); err != nil {
		t.Fatal(err)
	}
	var link string
	for _, message := range environment.mailer.Messages() {
		if _, token, found := strings.Cut(message.Text, "/auth/verify-email-change/"); found && message.To == "mallory@example.com" {
			link = strings.Fields(token)[0]
		}
	}
	if link == "" {
		t.Fatal("no email change link was sent")
	}
	key, err := environment.auth.CreateRecoveryKey("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := environment.auth.SignInWithRecoveryKey(key); err != nil {
		t.Fatal(err)
	}

	if _, err := environment.auth.verifyEmailChange(link); err == nil {
		t.Fatal("the link of the email change still works")
	}
	user, err := environment.userRepository.ByID("ada")
	if err != nil || user.Email != "ada@example.com" || user.PendingEmail != nil {
		t.Fatalf("after the key = %+v, %v, want ada@example.com and no pending email", user, err)
	}
}
