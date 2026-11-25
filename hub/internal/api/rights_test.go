package api_test

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// streams answer until the client goes; with every right they would never
// end in a test.
var streams = map[string]bool{
	"GET /api/v1/servers/events": true,
	"GET /api/v1/apps/events":    true,
}

// TestEveryRouteChecksItsRight goes through api.Routes, the one place
// that names the rights. For every route and every right it needs, a CI
// token with every other right is refused with missing_right naming that
// right. A token with every right is never refused for a right. A route
// for no token refuses even that one.
func TestEveryRouteChecksItsRight(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	all := model.RightRead | model.RightDeploy | model.RightEnv | model.RightDelete | model.RightServers
	tokens := map[model.Rights]string{}
	token := func(rights model.Rights) string {
		if raw, ok := tokens[rights]; ok {
			return raw
		}
		_, raw, err := environment.ciTokens.Create(service.Actor{UserID: owner.id, WorkspaceID: owner.workspaceID}, service.NewCIToken{Name: "rights", Rights: rights})
		if err != nil {
			t.Fatal(err)
		}
		tokens[rights] = raw
		return raw
	}
	checked := 0
	for _, route := range environment.routes {
		method, path, _ := strings.Cut(route.Pattern, " ")
		path = pathParameter.ReplaceAllString(path, "unknown")
		request := apiRequest{method: method, path: path}
		if method != http.MethodGet {
			request.body = `{}`
		}
		if route.Right == api.NoToken {
			request.bearer = token(all)
			recorder := environment.send(t, request)
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
				t.Errorf("%s: a token with every right got %d %s, want 403 forbidden", route.Pattern, recorder.Code, recorder.Body.String())
			}
			continue
		}
		for _, right := range model.AllRights[1:] {
			if !route.Right.Has(right) {
				continue
			}
			request.bearer = token(all &^ right)
			recorder := environment.send(t, request)
			if recorder.Code != http.StatusForbidden {
				t.Errorf("%s without %s: status %d, body %s", route.Pattern, right.Name(), recorder.Code, recorder.Body.String())
				continue
			}
			if detail := decodeBody[api.ErrorBody](t, recorder).Error; detail.Code != "missing_right" || detail.Right != right.Name() {
				t.Errorf("%s without %s: %#v", route.Pattern, right.Name(), detail)
			}
			checked++
		}
		if streams[route.Pattern] {
			continue
		}
		request.bearer = token(all)
		if recorder := environment.send(t, request); strings.Contains(recorder.Body.String(), `"code":"missing_right"`) {
			t.Errorf("%s: a token with every right was refused: %s", route.Pattern, recorder.Body.String())
		}
	}
	if checked < 50 {
		t.Fatalf("checked %d refusals; the routes name fewer rights than they should", checked)
	}
}

// TestACITokenForOneAppReachesOnlyThatApp: every route outside an app's
// own answers app_only, another app is not found, and the app itself
// passes the gate.
func TestACITokenForOneAppReachesOnlyThatApp(t *testing.T) {
	environment := newAPIEnvironment(t)
	owner := seedTokenOwner(t, environment, "ada")
	now := time.Now()
	environment.database.MustExec(`INSERT INTO servers (id, workspace_id, name, created_at) VALUES ('server-1', $1, 'web-1', $2)`, owner.workspaceID, now)
	environment.database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ('11111111-1111-1111-1111-111111111111', $1, 'server-1', 'shop', 'shop', $2)`, owner.workspaceID, now)
	environment.database.MustExec(`INSERT INTO apps (id, workspace_id, server_id, name, folder, created_at) VALUES ('22222222-2222-2222-2222-222222222222', $1, 'server-1', 'blog', 'blog', $2)`, owner.workspaceID, now)
	created := createCIToken(t, environment, owner, `"name":"shop deploy","rights":["deploy","env","delete","servers"],"app":"shop"`)
	if created.CIToken.App == nil || *created.CIToken.App != "shop" {
		t.Fatalf("app = %v", created.CIToken.App)
	}
	for _, route := range environment.routes {
		if route.Right == api.NoToken || streams[route.Pattern] {
			continue
		}
		method, pattern, _ := strings.Cut(route.Pattern, " ")
		request := apiRequest{method: method, bearer: created.Token}
		if method != http.MethodGet {
			request.body = `{}`
		}
		if !strings.Contains(pattern, "{app}") {
			request.path = pathParameter.ReplaceAllString(pattern, "unknown")
			recorder := environment.send(t, request)
			if route.Pattern == "GET /api/v1/account" || route.Pattern == "GET /api/v1/meta" {
				expectStatus(t, recorder, http.StatusOK)
				continue
			}
			if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"app_only"`) {
				t.Errorf("%s: status %d, body %s, want app_only", route.Pattern, recorder.Code, recorder.Body.String())
			}
			continue
		}
		request.path = pathParameter.ReplaceAllString(strings.ReplaceAll(pattern, "{app}", "blog"), "unknown")
		if recorder := environment.send(t, request); recorder.Code != http.StatusNotFound {
			t.Errorf("%s for blog: status %d, body %s, want 404", route.Pattern, recorder.Code, recorder.Body.String())
		}
		request.path = pathParameter.ReplaceAllString(strings.ReplaceAll(pattern, "{app}", "shop"), "unknown")
		if recorder := environment.send(t, request); strings.Contains(recorder.Body.String(), `"code":"app_only"`) || strings.Contains(recorder.Body.String(), `"code":"missing_right"`) {
			t.Errorf("%s for shop: %s", route.Pattern, recorder.Body.String())
		}
	}
}

// TestTheDocsNameEveryEndpointsRight keeps the table of reference/rights.md
// the same as api.Routes, in the same order. On a change it prints the
// table as it must be.
func TestTheDocsNameEveryEndpointsRight(t *testing.T) {
	environment := newAPIEnvironment(t)
	var want []string
	for _, route := range environment.routes {
		right := strings.Join(route.Right.List(), ", ")
		if route.Right == api.NoToken {
			right = "no token"
		}
		want = append(want, "| `"+route.Pattern+"` | "+right+" |")
	}
	docs, err := os.ReadFile("../../content/docs/reference/rights.md")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for line := range strings.SplitSeq(string(docs), "\n") {
		if strings.HasPrefix(line, "| `") && strings.Contains(line, " /api/v1/") {
			got = append(got, line)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("reference/rights.md, Endpoints, must be:\n| Endpoint | Right |\n| --- | --- |\n%s", strings.Join(want, "\n"))
	}
}
