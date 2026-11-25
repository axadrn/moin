package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/axadrn/moin/internal/link"
)

// TestDownloadComposeFile: compose.yml downloads the compose file of the
// live deploy, answers why there is none as the API does, and finds no app
// of another workspace.
func TestDownloadComposeFile(t *testing.T) {
	s := newSwitches(t)
	base := s.ada.Workspace.Path()
	download := func(ref string) (int, string, http.Header) {
		answer := s.request("GET", "/app/{workspaceSlug}/apps/{app}/compose.yml", base+"/apps/"+ref+"/compose.yml", nil, s.handler.ComposeFile)
		return answer.Code, answer.Body.String(), answer.Header()
	}
	if code, body, _ := download("blog"); code != http.StatusConflict || !strings.Contains(body, "no live deploy") {
		t.Fatalf("before a deploy: %d %q", code, body)
	}
	text := "name: blog\nservices:\n  web:\n    image: ghcr.io/me/blog:1\n"
	s.database.MustExec(`INSERT INTO deploys (id, app_id, number, status, compose_file) VALUES ('d1', $1, 1, 'succeeded', $2)`, s.app().ID, text)
	code, body, header := download("blog")
	if code != http.StatusOK || body != text || header.Get("Content-Disposition") != `attachment; filename="compose.yml"` || !strings.HasPrefix(header.Get("Content-Type"), "application/yaml") {
		t.Fatalf("download = %d %v %q", code, header, body)
	}

	bob := testutil.SeedUser(t, s.database, testutil.UserOptions{ID: "bob", Name: "Bob", Verified: true})
	actor := service.Actor{WorkspaceID: bob.Workspace.ID, UserID: bob.User.ID, Source: service.Source{Via: service.ViaWeb}}
	server, _, err := s.servers.Create(actor, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	shop, err := s.apps.Create(actor, "shop", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.database.MustExec(`INSERT INTO deploys (id, app_id, number, status, compose_file) VALUES ('d2', $1, 1, 'succeeded', 'name: shop')`, shop.ID)
	for _, ref := range []string{"shop", link.ID(shop.ID)} {
		if code, body, _ := download(ref); code != http.StatusNotFound || strings.Contains(body, "name: shop") {
			t.Errorf("another workspace's %s = %d %q", ref, code, body)
		}
	}

	// A database that fails is the hub's error, not the user's.
	s.database.MustExec(`ALTER TABLE deploys RENAME TO deploys_gone`)
	if code, _, _ := download("blog"); code != http.StatusInternalServerError {
		t.Errorf("a failing database = %d", code)
	}
}
