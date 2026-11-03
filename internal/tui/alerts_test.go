package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/ops/internal/client"
)

// TestAlertDestinationRows: the settings tab lists the alert destinations
// with their hint, events and state; C opens the form, T and enter send a
// test, x asks before it removes, and :alert and :test do the same.
func TestAlertDestinationRows(t *testing.T) {
	m := frames()["settings"]
	m.settings.destinations = []client.AlertDestination{
		{Name: "team", Kind: "slack", Hint: "hooks.slack.com/…WXYZ", Events: []string{"uptime.down", "uptime.up", "server.down", "server.up", "deploy.failed"}},
		{Name: "ci", Kind: "webhook", Hint: "https://ci.example.com/…", Events: []string{"deploy.failed"}, Failing: true, LastError: "HTTP 500"},
	}
	rows := m.settingRows()
	team := slices.IndexFunc(rows, func(row settingRow) bool { return row.destination == "team" })
	ci := slices.IndexFunc(rows, func(row settingRow) bool { return row.destination == "ci" })
	if team < 0 || ci < 0 || rows[team].name != "Alerts to team" || !rows[team].set {
		t.Fatalf("rows %+v", rows)
	}
	if value := strip(rows[team].value); !strings.HasPrefix(value, "slack hooks.slack.com/…WXYZ · every alert") {
		t.Errorf("team shows %q", value)
	}
	if value := strip(rows[ci].value); !strings.Contains(value, "deploy.failed") || !strings.Contains(value, "failing: HTTP 500") {
		t.Errorf("ci shows %q", value)
	}

	next, _ := m.settingKey("C")
	if form := next.(model); form.mode != modeForm || form.form.title != "New alert destination" || len(form.form.fields) != 5 || !form.form.fields[2].secret {
		t.Fatalf("C opened mode %d, form %+v", form.mode, form.form)
	}
	if err := next.(model).form.fields[0].check("pager"); err == nil {
		t.Error("the kind pager passed")
	}
	m.settingSelected = team
	for _, key := range []string{"T", "enter"} {
		if _, cmd := m.settingKey(key); cmd == nil {
			t.Errorf("%s on a destination does nothing", key)
		}
	}
	if removed, _ := m.settingKey("x"); removed.(model).mode != modeConfirm || !strings.Contains(removed.(model).confirm.question, "team") {
		t.Errorf("x did not ask before removing team")
	}
	if _, cmd := m.command("test"); cmd == nil {
		t.Error(":test does nothing")
	}
	if opened, _ := m.command("alert"); opened.(model).mode != modeForm {
		t.Error(":alert does not open the form")
	}
}
