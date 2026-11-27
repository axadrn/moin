package watchers

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	ws := New(store, mails, "https://cloud.example.com/")
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
	ws.save()
	if saved, _, _ := store.WatcherState(space.ID); !strings.Contains(string(saved), `"prod"`) {
		t.Errorf("saved = %s", saved)
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
	ws := New(store, mails, "https://cloud.example.com")
	space, _ := store.SignIn("ada@example.com")
	if err := store.StartTrial(space.ID, time.Now().Add(4*24*time.Hour)); err != nil {
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
