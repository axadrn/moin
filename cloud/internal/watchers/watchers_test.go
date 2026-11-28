package watchers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

	send := ws.mail(space.ID, "ada@example.com")
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
	send := ws.mail(space.ID, "ada@example.com")
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
}

// A saved state that does not open, like a plain one from before states
// were encrypted, does not stop the site: that watcher starts with
// nothing, and the next save is encrypted.
func TestPlainStateDropped(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	space, _ := store.SignIn("ada@example.com")
	token, _ := store.ConnectServer(space.ID, "prod")
	plain := `{"servers":{"prod":{"report":{"server":"prod","version":"v0.9.0"}}}}`
	if err := store.SaveWatcherState(space.ID, []byte(plain)); err != nil {
		t.Fatal(err)
	}
	ws := New(store, counts.New(database), &outbox{}, "https://cloud.example.com", [32]byte{1})
	if err := ws.Load(); err != nil {
		t.Fatalf("a plain state stopped the site: %v", err)
	}
	if view, err := ws.View(space.ID); err != nil || len(view.Servers) != 1 || view.Servers[0].Version != "" {
		t.Errorf("view = %+v, %v", view, err)
	}
	if err := ws.Receive(token, report.Report{Server: "prod", Version: "v1.0.0", EverySeconds: 60}); err != nil {
		t.Fatal(err)
	}
	ws.save()
	sealed, _ := store.WatcherState(space.ID)
	if state, err := ws.open(space.ID, sealed); err != nil || !strings.Contains(string(state), "v1.0.0") {
		t.Errorf("the next save: %s, %v", state, err)
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
	send := ws.reported(space.ID)
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

// together is a mailer where every mail waits for the other one: two
// mails go out only when both are sent at the same time.
type together struct {
	arrived sync.WaitGroup
}

func (m *together) Send(ctx context.Context, _ mailer.Message) error {
	m.arrived.Done()
	both := make(chan struct{})
	go func() { m.arrived.Wait(); close(both) }()
	select {
	case <-both:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("the other mail never started")
	}
}

// Every workspace is ticked at the same time: one whose alerts go out
// slowly does not hold up the alerts of the others.
func TestTickAtOnce(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	mails := &together{}
	mails.arrived.Add(2)
	ws := New(store, counts.New(database), mails, "https://cloud.example.com", [32]byte{1})
	unsent := `{"unsent":[{"to":{"kind":"mail"},"message":{"title":"prod stopped reporting"}}]}`
	for _, mail := range []string{"ada@example.com", "grace@example.com"} {
		space, _ := store.SignIn(mail)
		if _, err := store.ConnectServer(space.ID, "prod"); err != nil {
			t.Fatal(err)
		}
		if err := store.StartTrial(space.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveWatcherState(space.ID, ws.seal(space.ID, []byte(unsent))); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Load(); err != nil {
		t.Fatal(err)
	}
	ws.tick(t.Context())
	for id, w := range ws.all() {
		var saved string
		w.Save(func(state []byte) error { saved = string(state); return nil })
		if strings.Contains(saved, "stopped reporting") {
			t.Errorf("the alert of workspace %s did not go out: one tick waited for the other", id)
		}
	}
}

// A destination moin cloud never sends to is left out when the report
// comes in: a webhook for a watcher on the tailnet, valid for
// `moin alerts add`. So no alert to it is kept and tried again every few
// seconds, in the way of the ones that can go out.
func TestTailnetWebhookLeftOut(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := workspace.NewStore(database)
	ws := New(store, counts.New(database), &outbox{}, "https://cloud.example.com", [32]byte{1})
	space, _ := store.SignIn("ada@example.com")
	token, _ := store.ConnectServer(space.ID, "prod")
	alerts := []report.Alert{
		{Kind: "webhook", URL: "http://100.101.102.103/hook"},
		{Kind: "webhook", URL: "https://100.101.102.103/hook"},
		{Kind: "slack", URL: "https://hooks.example.com/services/x"},
	}
	if err := ws.Receive(token, report.Report{Server: "prod", EverySeconds: 60, Alerts: alerts}); err != nil {
		t.Fatal(err)
	}
	ws.tick(t.Context())
	ws.save()
	sealed, _ := store.WatcherState(space.ID)
	state, err := ws.open(space.ID, sealed)
	if err != nil || strings.Contains(string(state), "100.101.102.103") || !strings.Contains(string(state), "hooks.example.com") {
		t.Errorf("the saved state = %s, %v", state, err)
	}
}
