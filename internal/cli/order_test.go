package cli

import (
	"strings"
	"testing"
)

func TestMoveSendsTheWholeOrder(t *testing.T) {
	apps := `{"items":[{"name":"shop"},{"name":"blog"},{"name":"api"}]}`
	for _, test := range []struct {
		args []string
		sent string
	}{
		{[]string{"apps", "move", "api", "up"}, `{"order":["shop","api","blog"]}`},
		{[]string{"apps", "move", "shop", "down"}, `{"order":["blog","shop","api"]}`},
		{[]string{"apps", "move", "api", "top"}, `{"order":["api","shop","blog"]}`},
		{[]string{"apps", "move", "shop", "bottom"}, `{"order":["blog","api","shop"]}`},
		{[]string{"apps", "move", "shop", "2"}, `{"order":["blog","shop","api"]}`},
	} {
		api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps": apps, "PUT /api/v1/apps/order": `{"items":[{"name":"blog"},{"name":"shop"},{"name":"api"}]}`}}
		code, stdout, stderr := runCLI(t, api, test.args...)
		if code != 0 || !api.seen("PUT /api/v1/apps/order "+test.sent) || !strings.Contains(stdout, "Moved") {
			t.Errorf("%v: code %d, stdout %q, stderr %q, requests %v", test.args, code, stdout, stderr, api.requests)
		}
	}

	api := &fakeAPI{answers: map[string]string{"GET /api/v1/apps": apps}}
	code, stdout, _ := runCLI(t, api, "apps", "move", "shop", "up")
	if code != 0 || !strings.Contains(stdout, "nothing changed") || api.seen("PUT") {
		t.Errorf("the first app up: code %d, stdout %q", code, stdout)
	}
	code, _, stderr := runCLI(t, api, "apps", "move", "shop", "sideways")
	if code != 1 || !strings.Contains(stderr, "up|down|top|bottom|<position>") || api.seen("PUT") {
		t.Errorf("a wrong place: code %d, stderr %q", code, stderr)
	}
	code, _, stderr = runCLI(t, api, "apps", "move", "nope", "up")
	if code != 1 || !strings.Contains(stderr, "no nope") {
		t.Errorf("an unknown app: code %d, stderr %q", code, stderr)
	}

	servers := &fakeAPI{answers: map[string]string{
		"GET /api/v1/servers":       `{"items":[{"name":"web"},{"name":"db"}]}`,
		"PUT /api/v1/servers/order": `{"items":[{"name":"db"},{"name":"web"}]}`,
	}}
	code, stdout, stderr = runCLI(t, servers, "servers", "move", "db", "up")
	if code != 0 || !servers.seen(`PUT /api/v1/servers/order {"order":["db","web"]}`) || !strings.Contains(stdout, "Moved db to 1 of 2") {
		t.Errorf("servers move: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
