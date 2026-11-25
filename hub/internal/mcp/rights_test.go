package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/mcp"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolRights are the rights a tool names in its _meta, where Add puts
// them.
func toolRights(t *testing.T, tool *mcpsdk.Tool) model.Rights {
	t.Helper()
	data, err := json.Marshal(tool.Meta["rights"])
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil || len(names) == 0 {
		t.Fatalf("%s has no rights in its _meta: %s", tool.Name, data)
	}
	rights, ok := model.ParseRights(names...)
	if !strings.Contains(strings.Join(names, ","), "read") {
		rights &^= model.RightRead
	}
	if !ok || rights.String() != strings.Join(names, ",") {
		t.Fatalf("%s names the rights %v", tool.Name, names)
	}
	return rights
}

// needsSentence is how a description names the rights: "Needs the deploy
// right." or "Needs the delete and servers rights."
func needsSentence(rights model.Rights) string {
	names := rights.List()
	if len(names) == 1 {
		return "Needs the " + names[0] + " right."
	}
	return "Needs the " + strings.Join(names, " and ") + " rights."
}

// someInput is an input the schema takes: every required field with a
// value of its type, so the call reaches the tool's right.
func someInput(t *testing.T, tool *mcpsdk.Tool) map[string]any {
	t.Helper()
	data, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type any   `json:"type"`
			Enum []any `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{}
	for _, name := range schema.Required {
		property := schema.Properties[name]
		if len(property.Enum) > 0 {
			input[name] = property.Enum[0]
			continue
		}
		kind, _ := property.Type.(string)
		if kinds, ok := property.Type.([]any); ok {
			for _, each := range kinds {
				if each != "null" {
					kind, _ = each.(string)
				}
			}
		}
		switch kind {
		case "integer", "number":
			input[name] = 1
		case "boolean":
			input[name] = false
		case "array":
			input[name] = []any{}
		case "object":
			input[name] = map[string]any{}
		default:
			input[name] = "x"
		}
	}
	return input
}

// TestEveryToolChecksItsRight goes through every registered tool. Its
// rights in _meta are what the description says, and for every right it
// needs, a CI token with every other right is refused with missing_right
// naming that right, before the tool does anything.
func TestEveryToolChecksItsRight(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "rights")
	upgrade(t, env, owner)
	all := model.RightRead | model.RightDeploy | model.RightEnv | model.RightDelete | model.RightServers
	full, _ := createToken(t, env, owner, allRights...)
	tools, err := connectMemory(t, env.server, owner, full).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sessions := map[model.Rights]*mcpsdk.ClientSession{}
	session := func(rights model.Rights) *mcpsdk.ClientSession {
		if existing, ok := sessions[rights]; ok {
			return existing
		}
		bearer := *full
		bearer.Rights = rights
		sessions[rights] = connectMemory(t, env.server, owner, &bearer)
		return sessions[rights]
	}
	checked := 0
	for _, tool := range tools.Tools {
		rights := toolRights(t, tool)
		for _, right := range model.AllRights[1:] {
			if !rights.Has(right) {
				continue
			}
			detail := toolError(t, call(t, session(all&^right), tool.Name, someInput(t, tool)), "missing_right")
			if detail.Right != right.Name() {
				t.Errorf("%s without %s: %#v", tool.Name, right.Name(), detail)
			}
			checked++
		}
	}
	if checked < 40 {
		t.Fatalf("checked %d refusals; the tools name fewer rights than they should", checked)
	}
}

// TestMCPForAppTokensAndAgents: a CI token for one app does not reach MCP;
// a connection bound to /mcp, as an AI agent's, does, and only there.
func TestMCPForAppTokensAndAgents(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "agents")
	upgrade(t, env, owner)
	server, _, err := env.services.Servers.Create(service.Actor{WorkspaceID: owner.workspace.ID, UserID: owner.user.ID}, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.services.Apps.Create(service.Actor{WorkspaceID: owner.workspace.ID, UserID: owner.user.ID}, "shop", server.Name); err != nil {
		t.Fatal(err)
	}
	_, raw, err := env.ciTokens.Create(service.Actor{UserID: owner.user.ID, WorkspaceID: owner.workspace.ID}, service.NewCIToken{Name: "shop", App: "shop", Rights: model.DefaultRights})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+raw)
	recorder := httptest.NewRecorder()
	env.handler.ServeHTTP(recorder, request)
	var envelope api.ErrorBody
	if recorder.Code != http.StatusForbidden || json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil || envelope.Error.Code != "app_only" {
		t.Fatalf("a CI token for one app: %d %s", recorder.Code, recorder.Body.String())
	}

	connection, err := env.connections.Create(service.NewConnection{UserID: owner.user.ID, WorkspaceID: owner.workspace.ID, ClientName: "Claude", Rights: model.DefaultRights, ClientID: "client-1", Resource: "/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := env.connections.Issue(connection)
	if err != nil {
		t.Fatal(err)
	}
	whoami := output[mcp.WhoAmI](t, call(t, connectHTTP(t, env, tokens.Access), "whoami", nil))
	if whoami.Connection.Kind != "connection" || whoami.Connection.Name != "Claude" || strings.Join(whoami.Connection.Rights, ",") != "read,deploy,env" {
		t.Fatalf("whoami of an agent = %+v", whoami)
	}
	if err := env.connections.Remove(owner.user.ID, connection.ID, service.Source{}); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+tokens.Access)
	recorder = httptest.NewRecorder()
	env.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("a removed agent: %d %s", recorder.Code, recorder.Body.String())
	}
}

// docsRow is one row of the tools table in the docs: `a` · `b` | does | right.
var docsRow = regexp.MustCompile("^\\| (`[a-z_]+`(?: · `[a-z_]+`)*) \\| .* \\| ([a-z, ]+) \\|$")

// TestTheDocsNameEveryToolsRight keeps the Right column of the tools table
// in guides/mcp.md the same as the tools' own rights. On a change it
// prints the column as it must be.
func TestTheDocsNameEveryToolsRight(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "docs")
	full, _ := createToken(t, env, owner, allRights...)
	tools, err := connectMemory(t, env.server, owner, full).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, tool := range tools.Tools {
		right := strings.Join(toolRights(t, tool).List(), ", ")
		if tool.Annotations != nil && tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			right += ", destructive"
		}
		want[tool.Name] = right
	}
	docs, err := os.ReadFile("../../content/docs/guides/mcp.md")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(docs), "\n") {
		match := docsRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		for name := range strings.SplitSeq(match[1], " · ") {
			got[strings.Trim(name, "`")] = match[2]
		}
	}
	var wrong []string
	for name, right := range want {
		if got[name] != right {
			wrong = append(wrong, "`"+name+"`: "+right+" (the docs say "+got[name]+")")
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			wrong = append(wrong, "`"+name+"` is in the docs but no tool")
		}
	}
	sort.Strings(wrong)
	if len(wrong) > 0 {
		t.Fatalf("guides/mcp.md, the Right column of Tools:\n%s", strings.Join(wrong, "\n"))
	}
}
