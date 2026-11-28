package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestComposeKeyAsksTheServer: C asks the app's server for its compose
// file and opens the pager; a refusal shows why.
func TestComposeKeyAsksTheServer(t *testing.T) {
	m := start(t, &fake{})
	asked := ""
	m.cli.Compose = func(ctx context.Context, server, app string, said io.Writer) (string, error) {
		asked = server + "/" + app
		return "name: moin-shop\n", nil
	}
	cmd := press(m, "C")
	if cmd == nil {
		t.Fatal("C did nothing")
	}
	msg, ok := cmd().(composeMsg)
	if !ok || msg.file != "name: moin-shop\n" || asked != "prod/shop" {
		t.Fatalf("msg = %+v, asked %q", msg, asked)
	}
	if _, pager := m.Update(msg); pager == nil || strings.Contains(m.message, "✗") {
		t.Errorf("no pager, message %q", m.message)
	}

	m.cli.Compose = func(ctx context.Context, server, app string, said io.Writer) (string, error) {
		return "", errors.New("it holds the value of API_KEY")
	}
	run(m, press(m, "C"))
	if !strings.Contains(m.message, "API_KEY") {
		t.Errorf("message = %q, want why", m.message)
	}
}
