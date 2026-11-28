package watchers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/report"
)

type outbox []mailer.Message

func (o *outbox) Send(_ context.Context, m mailer.Message) error {
	*o = append(*o, m)
	return nil
}

func TestReceiveAndMail(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	mails := &outbox{}
	ws := New(store, counts.New(database), mails, "https://cloud.example.com/", [32]byte{1})
	space, _ := store.SignIn("ada@example.com")
	token, _ := store.ConnectServer(space.ID, "prod")

	if err := ws.Receive("wrong", report.Report{Server: "prod"}); !errors.Is(err, ErrUnknownToken) {
		t.Errorf("a wrong token: %v", err)
	}
	if err := ws.Receive(token, report.Report{Server: "web-1"}); err == nil {
		t.Error("took a report of another server")
	}
	if err := ws.Receive(token, report.Report{Server: "prod", EverySeconds: 60}); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Workspace(space.ID); got.TrialEnds.IsZero() {
		t.Error("the first report did not start the trial")
	}

	send := ws.sender(space.ID, "ada@example.com")
	if err := send(t.Context(), alert.Destination{Kind: mailKind}, alert.Message{Title: "prod stopped reporting", Text: "No report since 10:00."}); err != nil {
		t.Fatal(err)
	}
	if len(*mails) != 1 || (*mails)[0].To != "ada@example.com" || (*mails)[0].Subject != "prod stopped reporting" || !strings.HasSuffix((*mails)[0].Text, "https://cloud.example.com/cloud") {
		t.Errorf("mails = %+v", *mails)
	}
	view, _ := ws.View(space.ID)
	if len(view.Servers) != 1 || view.Servers[0].Name != "prod" {
		t.Errorf("view = %+v", view)
	}
}

// After the trial, unpaid, reports count but alerts are dropped; the
// trial mails go out once each.
func TestTrialOver(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	mails := &outbox{}
	ws := New(store, counts.New(database), mails, "https://cloud.example.com", [32]byte{1})
	space, _ := store.SignIn("ada@example.com")
	if err := store.StartTrial(space.ID, time.Now().Add(-26*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ws.mailTrials(t.Context())
	ws.mailTrials(t.Context())
	if len(*mails) != 1 || !strings.Contains((*mails)[0].Subject, "ends in 5 days") {
		t.Fatalf("mails = %+v", *mails)
	}
	if _, err := database.Exec(`UPDATE workspaces SET trial_ends = ?`, time.Now().Add(-time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	ws.mailTrials(t.Context())
	ws.mailTrials(t.Context())
	if len(*mails) != 2 || !strings.Contains((*mails)[1].Subject, "Alerts are off") {
		t.Fatalf("mails = %+v", *mails)
	}
	send := ws.sender(space.ID, "ada@example.com")
	if err := send(t.Context(), alert.Destination{Kind: mailKind}, alert.Message{Title: "prod stopped reporting"}); err != nil {
		t.Fatal(err)
	}
	if len(*mails) != 2 {
		t.Errorf("an alert went out after the trial: %+v", (*mails)[2])
	}
}

// What a watcher knows holds the alert destinations of its servers. It
// is saved encrypted, opens with the same key only, and only as the
// state of its own workspace.
func TestStateEncrypted(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	ws := New(store, counts.New(database), &outbox{}, "https://cloud.example.com", [32]byte{1})
	space, _ := store.SignIn("ada@example.com")
	token, _ := store.ConnectServer(space.ID, "prod")
	chat := report.Alert{Kind: "telegram", Token: "123:secret-bot-token", ChatID: "42"}
	if err := ws.Receive(token, report.Report{Server: "prod", EverySeconds: 60, Alerts: []report.Alert{chat}}); err != nil {
		t.Fatal(err)
	}
	ws.save()
	sealed, _ := store.WatcherState(space.ID)
	for _, plain := range []string{"secret-bot-token", "telegram", "prod"} {
		if len(sealed) == 0 || strings.Contains(string(sealed), plain) {
			t.Fatalf("the saved state shows %q: %q", plain, sealed)
		}
	}
	state, err := ws.open(space.ID, sealed)
	if err != nil || !strings.Contains(string(state), "secret-bot-token") {
		t.Fatalf("open = %s, %v", state, err)
	}
	if _, err := ws.open("another-workspace", sealed); err == nil {
		t.Error("the state opened as another workspace's")
	}

	// After a restart with the same key the watcher knows its server.
	again := New(store, counts.New(database), &outbox{}, "https://cloud.example.com", [32]byte{1})
	if view, err := again.View(space.ID); err != nil || len(view.Servers) != 1 || view.Servers[0].Name != "prod" {
		t.Errorf("view = %+v, %v", view, err)
	}
	other := New(store, counts.New(database), &outbox{}, "https://cloud.example.com", [32]byte{2})
	if err := other.Load(); err == nil || !strings.Contains(err.Error(), "SECRETS_KEY") {
		t.Errorf("another key: %v", err)
	}
}

// An alert of a report goes to public https addresses only: a webhook at
// a private address is refused before anything is sent, and so is http.
func TestPrivateWebhookRefused(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	ws := New(store, counts.New(database), &outbox{}, "https://cloud.example.com", [32]byte{1})
	space, _ := store.SignIn("ada@example.com")
	if err := store.StartTrial(space.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	called := false
	inside := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer inside.Close()
	ws.public.Transport.(*http.Transport).TLSClientConfig = inside.Client().Transport.(*http.Transport).TLSClientConfig
	send := ws.sender(space.ID, "ada@example.com")
	for _, address := range []string{inside.URL + "/hook", "https://10.0.0.5/hook", "https://[::1]/hook", "http://example.com/hook"} {
		for _, kind := range []string{"webhook", "slack", "discord"} {
			if err := send(t.Context(), alert.Destination{Kind: kind, URL: address}, alert.Message{Title: "prod stopped reporting"}); err == nil {
				t.Errorf("%s to %s went out", kind, address)
			}
		}
	}
	if called {
		t.Error("a webhook at a private address was called")
	}
}
