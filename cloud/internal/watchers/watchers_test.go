package watchers

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

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

	send := ws.sender("ada@example.com")
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
