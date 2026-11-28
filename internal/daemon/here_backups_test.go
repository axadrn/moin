package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/protocol"
)

// A restore names a copy by its ID or by a time; anything else is an
// error that says what it may be.
func TestCopyFor(t *testing.T) {
	b := newBackups(newApps(t.TempDir()))
	database := protocol.Database{Path: "/data/app.db"}
	if id, err := b.copyFor(context.Background(), "shop", database, "20261127T140000Z-482913"); err != nil || id != "20261127T140000Z-482913" {
		t.Fatalf("a copy ID: %q, %v", id, err)
	}
	for _, wrong := range []string{"yesterday", "../x", "v3"} {
		if _, err := b.copyFor(context.Background(), "shop", database, wrong); err == nil || !strings.Contains(err.Error(), "2026-11-27T14:07") {
			t.Errorf("%q: %v", wrong, err)
		}
	}
}

// An alert destination is kept only when a test alert reached it, listed
// without its secret, and removed by its number.
func TestAlertsHere(t *testing.T) {
	useConfig(t, "name: prod\n")
	hook := &webhook{}
	server := httptest.NewServer(hook)
	defer server.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	if err := AddAlertHere(context.Background(), alert.Destination{Kind: "webhook", URL: closed.URL}); err == nil {
		t.Fatal("kept a destination nothing reaches")
	}
	if err := AddAlertHere(context.Background(), alert.Destination{Kind: "webhook", URL: server.URL + "/secret-path"}); err != nil {
		t.Fatal(err)
	}
	if got := hook.titles(); got != "a test alert from prod" {
		t.Fatalf("test alert = %q", got)
	}
	names, err := AlertsHere()
	if err != nil || len(names) != 1 || strings.Contains(names[0], "secret-path") || !strings.HasPrefix(names[0], "webhook ") {
		t.Fatalf("names = %q, %v", names, err)
	}
	if err := SendTestAlertHere(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAlertHere(2); err == nil {
		t.Fatal("removed an alert that is not there")
	}
	if err := RemoveAlertHere(1); err != nil {
		t.Fatal(err)
	}
	if names, _ := AlertsHere(); len(names) != 0 {
		t.Fatalf("names after rm = %q", names)
	}
}

// A watcher is added once per address, with its newest token, and removed
// by its address.
func TestWatchersHere(t *testing.T) {
	useConfig(t, "name: prod\n")
	for _, wrong := range []Watcher{{URL: "watcher.example.com", Token: "t"}, {URL: "https://watcher.example.com"}} {
		if err := ConnectWatcherHere(wrong); err == nil {
			t.Errorf("took %+v", wrong)
		}
	}
	for _, token := range []string{"old", "new"} {
		if err := ConnectWatcherHere(Watcher{URL: "https://watcher.example.com", Token: token}); err != nil {
			t.Fatal(err)
		}
	}
	config, _ := LoadConfig()
	if len(config.Watchers) != 1 || config.Watchers[0].Token != "new" {
		t.Fatalf("watchers = %+v", config.Watchers)
	}
	if err := DisconnectWatcherHere("https://other.example.com"); err == nil {
		t.Fatal("removed a watcher that is not there")
	}
	if err := DisconnectWatcherHere("https://watcher.example.com"); err != nil {
		t.Fatal(err)
	}
	if addresses, _ := WatchersHere(); len(addresses) != 0 {
		t.Fatalf("addresses = %q", addresses)
	}
}

// A server keeps the name of its first setup: its backups live under it.
func TestNameServer(t *testing.T) {
	useConfig(t, "")
	if err := nameServer("user@host"); err == nil {
		t.Fatal("took a name with @")
	}
	if err := nameServer("prod"); err != nil {
		t.Fatal(err)
	}
	if err := nameServer("other"); err != nil {
		t.Fatal(err)
	}
	if config, _ := LoadConfig(); config.Name != "prod" {
		t.Fatalf("name = %q", config.Name)
	}
}
