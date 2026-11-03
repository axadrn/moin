package tui

import (
	"strings"
	"testing"

	"github.com/axadrn/ops/internal/client"
)

func appOrder(m model) string {
	var names []string
	for _, app := range m.apps {
		names = append(names, app.Name)
	}
	return strings.Join(names, ",")
}

// K and J move the selected app or server, and the selection goes with it.
func TestMoveKeys(t *testing.T) {
	m := press(frames()["apps"], "j", "K")
	if appOrder(m) != "blog,hello,shop,notes" || m.appSelected != 0 {
		t.Errorf("K: %s, selected %d", appOrder(m), m.appSelected)
	}
	if m = press(m, "K"); appOrder(m) != "blog,hello,shop,notes" {
		t.Errorf("K on the first app: %s", appOrder(m))
	}
	if m = press(m, "J", "J"); appOrder(m) != "hello,shop,blog,notes" || m.appSelected != 2 {
		t.Errorf("J J: %s, selected %d", appOrder(m), m.appSelected)
	}
	if m = command(m, "move up"); appOrder(m) != "hello,blog,shop,notes" || m.appSelected != 1 {
		t.Errorf(":move up: %s, selected %d", appOrder(m), m.appSelected)
	}
	if m = command(m, "move down"); appOrder(m) != "hello,shop,blog,notes" {
		t.Errorf(":move down: %s", appOrder(m))
	}
	if m = command(m, "move sideways"); m.err == nil {
		t.Error(":move sideways says nothing")
	}

	servers := press(frames()["servers"], "J")
	if servers.servers[1].Name != "dock-1" || servers.selected != 1 {
		t.Errorf("J on servers: %v, selected %d", servers.servers, servers.selected)
	}
	// An open app has no order to change: K and J do nothing there.
	open := frames()["apps"]
	detail := client.AppDetail{App: open.apps[0]}
	open.detail = &detail
	if open = press(open, "J"); appOrder(open) != "hello,blog,shop,notes" {
		t.Errorf("J in an open app: %s", appOrder(open))
	}
}
