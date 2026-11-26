package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/mcp"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/link"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// dataReadTools read app data, as strong as secrets: they change nothing
// but need env.
var dataReadTools = map[string]bool{"download_backup": true, "query_database": true}

func TestToolsMatchTheSelection(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "lister")
	token, _ := createToken(t, env, owner)
	session := connectMemory(t, env.server, owner, token)

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		if tool.OutputSchema == nil || tool.InputSchema == nil {
			t.Errorf("%s has no input or output schema", tool.Name)
		}
		rights := toolRights(t, tool)
		if !strings.HasSuffix(tool.Description, " "+needsSentence(rights)) {
			t.Errorf("%s description does not end with its right: %s", tool.Name, tool.Description)
		}
		readOnly := rights == model.RightRead || dataReadTools[tool.Name]
		if dataReadTools[tool.Name] && rights != model.RightEnv {
			t.Errorf("%s reads app data with %s, want env", tool.Name, rights)
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s read-only hint does not match its right %s", tool.Name, rights)
		}
	}
	slices.Sort(names)
	want := []string{"add_alert_destination", "add_bucket", "add_server", "check_domains", "create_app", "create_deploy_hook", "deploy_image", "deploy_ref", "download_backup", "edit_bucket", "exec_command", "execute_sql", "get_app", "get_backups", "get_billing", "get_catalog_app", "get_compose_file", "get_deploy", "get_github", "get_instant", "get_integrations", "get_logs", "get_metrics", "get_status", "get_uptime", "get_version", "get_workspace", "install_catalog_app", "invite_member", "link_repo", "list_activity", "list_alert_destinations", "list_apps", "list_branches", "list_buckets", "list_catalog", "list_env", "list_registries", "list_repos", "list_servers", "list_tables", "list_workspaces", "new_install_command", "pin_app", "query_database", "redeploy_app", "remove_alert_destination", "remove_app", "remove_bucket", "remove_deploy_hook", "remove_password", "remove_registry", "remove_server", "rename_app", "rename_server", "rename_workspace", "replace_env", "resend_invitation", "restart_app", "restore_app", "revoke_invitation", "rollback_app", "set_app_bucket", "set_backups", "set_bucket_key", "set_daemon_updates", "set_default_bucket", "set_domains", "set_email_alerts", "set_env", "set_instant", "set_integrations", "set_password", "set_registry", "set_server_security", "set_uptime", "start_app", "stop_app", "test_alert_destination", "unlink_repo", "update_app", "update_daemon", "update_hub", "whoami"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
}

func TestWhoAmIReturnsTheTokenUser(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "ada")
	seedOwner(t, env, "other")
	_, raw := createToken(t, env, owner)
	session := connectHTTP(t, env, raw)

	result := call(t, session, "whoami", nil)
	whoami := output[mcp.WhoAmI](t, result)
	if whoami.User.ID != owner.user.ID || whoami.User.Email != "ada@example.com" || whoami.Connection.Name != "Claude" || whoami.Connection.Kind != "ci_token" || strings.Join(whoami.Connection.Rights, ",") != "read" {
		t.Fatalf("whoami = %#v", whoami)
	}
	if whoami.Workspace == nil || whoami.Workspace.Workspace.ID != link.ID(owner.workspace.ID) || whoami.Workspace.Role != "owner" {
		t.Fatalf("workspace = %#v", whoami.Workspace)
	}
	if !strings.HasPrefix(text(result), "Signed in as Ada (ada@example.com)") || !strings.Contains(text(result), `"email":"ada@example.com"`) {
		t.Fatalf("text = %q", text(result))
	}
}

func TestMCPRequiresAConnectionOrCIToken(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "guest")
	token, raw := createToken(t, env, owner)
	err := env.ciTokens.Remove(&model.WorkspaceMember{WorkspaceID: owner.workspace.ID, UserID: owner.user.ID, Role: model.WorkspaceRoleOwner}, token.CITokenID, service.Source{})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for name, authorization := range map[string]string{
		"no header":        "",
		"unknown token":    "Bearer pat_unknown",
		"unknown access":   "Bearer at_unknown",
		"revoked token":    "Bearer " + raw,
		"non-token bearer": "Bearer " + strings.Repeat("s", 43),
	} {
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		recorder := httptest.NewRecorder()
		env.handler.ServeHTTP(recorder, request)
		// The 401 tells an MCP client where to learn how to sign in (RFC
		// 9728), and that a token it sent did not work.
		challenge := `Bearer resource_metadata="` + testResourceMetadata + `"`
		if authorization != "" {
			challenge += `, error="invalid_token"`
		}
		if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("WWW-Authenticate") != challenge {
			t.Fatalf("%s: status = %d, headers %v, body %s", name, recorder.Code, recorder.Header(), recorder.Body.String())
		}
		var envelope api.ErrorBody
		err := json.Unmarshal(recorder.Body.Bytes(), &envelope)
		if err != nil || envelope.Error.Code != "unauthenticated" {
			t.Fatalf("%s: body = %s", name, recorder.Body.String())
		}
	}
}

func TestMCPIsStateless(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "stateless")
	_, raw := createToken(t, env, owner)
	request := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+raw)
	recorder := httptest.NewRecorder()
	env.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET status = %d, Allow %q", recorder.Code, recorder.Header().Get("Allow"))
	}

	session := connectHTTP(t, env, raw)
	if session.ID() != "" {
		t.Fatalf("session id = %q, want none", session.ID())
	}
	for range 2 {
		output[mcp.WhoAmI](t, call(t, session, "whoami", nil))
	}
}

func TestUnknownToolIsAProtocolError(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "unknown")
	_, raw := createToken(t, env, owner)
	session := connectHTTP(t, env, raw)
	_, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "delete_account"})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("CallTool(delete_account) error = %v", err)
	}
}

// addMember adds userID to the owner's workspace with role and returns a
// token of that member bound to the workspace.
func addMember(t *testing.T, env *environment, owner owner, id, role string, rights ...string) string {
	t.Helper()
	member := seedOwner(t, env, id)
	_, err := env.database.Exec(`
		INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, "extra-"+id, owner.workspace.ID, member.user.ID, role, time.Now())
	if err != nil {
		t.Fatalf("add %s: %v", id, err)
	}
	member.workspace = owner.workspace
	_, raw := createToken(t, env, member, rights...)
	return raw
}

func upgrade(t *testing.T, env *environment, owner owner) {
	t.Helper()
	_, err := env.database.Exec(`UPDATE subscriptions SET plan_id = $1 WHERE workspace_id = $2`, model.SubscriptionPlanPro, owner.workspace.ID)
	if err != nil {
		t.Fatalf("upgrade workspace: %v", err)
	}
}

func TestWorkspaceToolsStayInTheBoundWorkspace(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "bound")
	stranger := seedOwner(t, env, "stranger")
	second, err := env.services.Workspaces.Create(owner.user.ID, "Second Workspace")
	if err != nil {
		t.Fatal(err)
	}
	_, raw := createToken(t, env, owner)
	session := connectHTTP(t, env, raw)

	list := output[mcp.WorkspaceList](t, call(t, session, "list_workspaces", nil))
	if len(list.Items) != 1 || list.Items[0].Workspace.ID != link.ID(owner.workspace.ID) || list.Items[0].Role != model.WorkspaceRoleOwner {
		t.Fatalf("list_workspaces = %#v", list)
	}
	for _, arguments := range []map[string]any{nil, {"workspace": owner.workspace.Slug}} {
		detail := output[mcp.WorkspaceDetail](t, call(t, session, "get_workspace", arguments))
		if detail.Workspace.ID != link.ID(owner.workspace.ID) || detail.Role != model.WorkspaceRoleOwner || len(detail.Members) != 1 || detail.Seats.Used != 1 || !detail.Permissions.ManageMembers {
			t.Fatalf("get_workspace(%v) = %#v", arguments, detail)
		}
	}
	for _, slug := range []string{second.Slug, stranger.workspace.Slug, "missing"} {
		for _, tool := range []string{"get_workspace", "list_activity", "get_billing"} {
			toolError(t, call(t, session, tool, map[string]any{"workspace": slug}), "not_found")
		}
	}
}

func TestToolsNeedTheirRight(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "writer")
	upgrade(t, env, owner)
	_, readOnly := createToken(t, env, owner)
	_, readWrite := createToken(t, env, owner, allRights...)

	reader := connectHTTP(t, env, readOnly)
	for tool, arguments := range map[string]map[string]any{
		"rename_workspace": {"name": "Renamed"},
		"invite_member":    {"email": "new@example.com", "role": "member"},
	} {
		detail := toolError(t, call(t, reader, tool, arguments), "missing_right")
		if detail.Right != "servers" || detail.Message != "This CI token may not manage servers and settings. Change it in Settings, Connections." {
			t.Fatalf("%s error = %#v", tool, detail)
		}
	}
	if len(env.mailer.Messages()) != 0 {
		t.Fatal("a read-only token sent an invitation")
	}

	writer := connectHTTP(t, env, readWrite)
	renamed := output[mcp.RenamedWorkspace](t, call(t, writer, "rename_workspace", map[string]any{"name": "  Renamed Team  "}))
	if renamed.Workspace.Name != "Renamed Team" || renamed.Workspace.Slug != "renamed-team" {
		t.Fatalf("rename_workspace = %#v", renamed)
	}
	fields := toolError(t, call(t, writer, "rename_workspace", map[string]any{"name": "x"}), "validation_failed").Fields
	if fields["name"] == "" {
		t.Fatalf("fields = %v", fields)
	}

	// A token invites members only, whatever its rights: an admin is
	// invited in the web.
	toolError(t, call(t, writer, "invite_member", map[string]any{"email": "boss@example.com", "role": "admin"}), "forbidden")
	invited := output[mcp.Invitation](t, call(t, writer, "invite_member", map[string]any{"email": " New@Example.com ", "role": "member"}))
	if invited.Email != "new@example.com" || invited.Role != "member" || invited.Workspace.ID != link.ID(owner.workspace.ID) {
		t.Fatalf("invite_member = %#v", invited)
	}
	if len(env.mailer.Messages()) != 1 {
		t.Fatalf("emails = %d, want 1", len(env.mailer.Messages()))
	}
	detail := output[mcp.WorkspaceDetail](t, call(t, writer, "get_workspace", nil))
	if len(detail.Invitations) != 1 || detail.Invitations[0].Email != "new@example.com" {
		t.Fatalf("invitations = %#v", detail.Invitations)
	}
	fields = toolError(t, call(t, writer, "invite_member", map[string]any{"email": "boss@example.com", "role": "owner"}), "validation_failed").Fields
	if fields["role"] == "" {
		t.Fatalf("fields = %v", fields)
	}
	toolError(t, call(t, writer, "invite_member", map[string]any{"email": "writer@example.com", "role": "member"}), "already_member")

	resent := output[mcp.InvitationChange](t, call(t, writer, "resend_invitation", map[string]any{"invitation": "NEW@example.com"}))
	if resent.Email != "new@example.com" || resent.Revoked || len(env.mailer.Messages()) != 2 {
		t.Fatalf("resend_invitation = %#v, emails = %d", resent, len(env.mailer.Messages()))
	}
	detail = output[mcp.WorkspaceDetail](t, call(t, writer, "get_workspace", nil))
	revoked := output[mcp.InvitationChange](t, call(t, writer, "revoke_invitation", map[string]any{"invitation": detail.Invitations[0].ID}))
	if revoked.Email != "new@example.com" || !revoked.Revoked {
		t.Fatalf("revoke_invitation = %#v", revoked)
	}
	toolError(t, call(t, writer, "revoke_invitation", map[string]any{"invitation": "new@example.com"}), "not_found")
}

func TestInviteMemberSharesTheInvitationLimit(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "inviter")
	upgrade(t, env, owner)
	_, raw := createToken(t, env, owner, allRights...)
	for range 30 {
		env.services.Invitations.Allow(owner.workspace.ID + ":" + owner.user.ID)
	}

	toolError(t, call(t, connectHTTP(t, env, raw), "invite_member", map[string]any{"email": "new@example.com", "role": "member"}), "rate_limited")
	if len(env.mailer.Messages()) != 0 {
		t.Fatalf("emails = %d, want 0", len(env.mailer.Messages()))
	}
}

// TestInviteMemberRefusesAdmins: an AI agent invites members only, so a
// leaked token cannot make an admin.
func TestInviteMemberRefusesAdmins(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "inviter")
	upgrade(t, env, owner)
	_, raw := createToken(t, env, owner, allRights...)
	session := connectHTTP(t, env, raw)
	toolError(t, call(t, session, "invite_member", map[string]any{"email": "new@example.com", "role": "admin"}), "forbidden")
	if len(env.mailer.Messages()) != 0 {
		t.Fatalf("emails = %d, want 0", len(env.mailer.Messages()))
	}
	output[mcp.Invitation](t, call(t, session, "invite_member", map[string]any{"email": "new@example.com", "role": "member"}))
}

func TestRolesStillApplyToRights(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "boss")
	upgrade(t, env, owner)
	member := connectHTTP(t, env, addMember(t, env, owner, "worker", model.WorkspaceRoleMember, allRights...))
	admin := connectHTTP(t, env, addMember(t, env, owner, "helper", model.WorkspaceRoleAdmin))

	toolError(t, call(t, member, "rename_workspace", map[string]any{"name": "Taken Over"}), "forbidden")
	toolError(t, call(t, member, "invite_member", map[string]any{"email": "friend@example.com", "role": "member"}), "forbidden")
	toolError(t, call(t, member, "revoke_invitation", map[string]any{"invitation": "friend@example.com"}), "forbidden")
	output[mcp.Activity](t, call(t, member, "list_activity", nil))
	toolError(t, call(t, member, "get_billing", nil), "forbidden")
	toolError(t, call(t, admin, "get_billing", nil), "forbidden")
	detail := output[mcp.WorkspaceDetail](t, call(t, member, "get_workspace", nil))
	if detail.Role != model.WorkspaceRoleMember || len(detail.Invitations) != 0 || len(detail.Members) != 3 {
		t.Fatalf("member view = %#v", detail)
	}
	output[mcp.Activity](t, call(t, admin, "list_activity", nil))
}

func TestListActivityPagesWithTheCursor(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "watcher")
	_, raw := createToken(t, env, owner, allRights...)
	session := connectHTTP(t, env, raw)
	for _, name := range []string{"First Name", "Second Name", "Third Name"} {
		output[mcp.RenamedWorkspace](t, call(t, session, "rename_workspace", map[string]any{"name": name}))
	}

	first := output[mcp.Activity](t, call(t, session, "list_activity", map[string]any{"limit": 2}))
	if len(first.Items) != 2 || first.NextCursor == nil || first.Items[0].Action != "workspace.updated" || !strings.Contains(first.Items[0].Metadata, "Third Name") {
		t.Fatalf("first page = %#v", first)
	}
	second := output[mcp.Activity](t, call(t, session, "list_activity", map[string]any{"limit": 2, "cursor": *first.NextCursor}))
	if len(second.Items) == 0 || second.Items[0].ID == first.Items[1].ID {
		t.Fatalf("second page = %#v", second)
	}
	fields := toolError(t, call(t, session, "list_activity", map[string]any{"limit": 500}), "validation_failed").Fields
	if fields["limit"] != api.ActivityLimitMessage {
		t.Fatalf("fields = %v", fields)
	}
	toolError(t, call(t, session, "list_activity", map[string]any{"cursor": "not a cursor"}), "invalid_cursor")
}

func TestGetBillingReturnsThePlanWithoutLinks(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "payer")
	_, raw := createToken(t, env, owner)
	session := connectHTTP(t, env, raw)

	result := call(t, session, "get_billing", nil)
	billing := output[mcp.Billing](t, result)
	if billing.Access == nil || billing.Access.PlanID != model.SubscriptionPlanFree || len(billing.Plans) == 0 {
		t.Fatalf("billing = %#v", billing)
	}
	if strings.Contains(text(result), "http") {
		t.Fatalf("billing text contains a link: %s", text(result))
	}
}

func TestTokenOfAFormerMemberReachesNothing(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "keeper")
	raw := addMember(t, env, owner, "leaver", model.WorkspaceRoleMember)
	_, err := env.database.Exec(`DELETE FROM workspace_members WHERE id = $1`, "extra-leaver")
	if err != nil {
		t.Fatal(err)
	}
	session := connectHTTP(t, env, raw)
	whoami := output[mcp.WhoAmI](t, call(t, session, "whoami", nil))
	if whoami.User.Email != "leaver@example.com" || whoami.Workspace != nil {
		t.Fatalf("whoami = %#v", whoami)
	}
	toolError(t, call(t, session, "get_workspace", nil), "not_found")
	list := output[mcp.WorkspaceList](t, call(t, session, "list_workspaces", nil))
	if len(list.Items) != 0 {
		t.Fatalf("list_workspaces = %#v", list)
	}
}

func TestOpsToolsActAsMembersAndStopAfterRemoval(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "lead")
	upgrade(t, env, owner)
	member := connectHTTP(t, env, addMember(t, env, owner, "dev", model.WorkspaceRoleMember, allRights...))

	output[mcp.CreatedServer](t, call(t, member, "add_server", map[string]any{"server": "mcp-1"}))
	activity := output[mcp.Activity](t, call(t, member, "list_activity", nil))
	if len(activity.Items) == 0 || activity.Items[0].Summary != "added the server mcp-1" || activity.Items[0].Via != "mcp" {
		t.Fatalf("activity = %#v", activity.Items)
	}

	_, err := env.database.Exec(`DELETE FROM workspace_members WHERE id = $1`, "extra-dev")
	if err != nil {
		t.Fatal(err)
	}
	toolError(t, call(t, member, "list_servers", nil), "not_found")
}

func TestOnlyQueryDatabaseTakesALimit(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "sql")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, allRights...)
	session := connectMemory(t, env.server, owner, token)

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	properties := map[string]map[string]any{}
	for _, tool := range result.Tools {
		schema, _ := json.Marshal(tool.InputSchema)
		var decoded struct {
			Properties map[string]any `json:"properties"`
		}
		_ = json.Unmarshal(schema, &decoded)
		properties[tool.Name] = decoded.Properties
	}
	for _, name := range []string{"query_database", "execute_sql"} {
		for _, field := range []string{"app", "database", "sql", "params"} {
			if _, ok := properties[name][field]; !ok {
				t.Errorf("%s has no %s", name, field)
			}
		}
	}
	if _, ok := properties["query_database"]["limit"]; !ok {
		t.Error("query_database has no limit")
	}
	if _, ok := properties["execute_sql"]["limit"]; ok {
		t.Error("execute_sql takes a limit")
	}
	// Out of range is refused the same way as the tail of get_logs.
	limit := toolError(t, call(t, session, "query_database", map[string]any{"app": "blog", "sql": "SELECT 1", "limit": 5000}), "validation_failed").Fields
	tail := toolError(t, call(t, session, "get_logs", map[string]any{"app": "blog", "tail": 5000}), "validation_failed").Fields
	if limit["limit"] != "Choose between 1 and 1000 rows." || tail["tail"] != "Choose between 1 and 1000 lines." {
		t.Errorf("limit %v, tail %v", limit, tail)
	}
}

// TestAppAndServerParameters: tools name the app app and the server
// server; the old spelling name is gone.
func TestAppAndServerParameters(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "names")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, allRights...)
	session := connectMemory(t, env.server, owner, token)

	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-1"}))
	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "box-2"}))
	output[mcp.CreatedApp](t, call(t, session, "create_app", map[string]any{"app": "shop", "server": "box-1"}))
	output[mcp.CreatedApp](t, call(t, session, "create_app", map[string]any{"app": "blog", "server": "box-1"}))
	output[mcp.AppDetail](t, call(t, session, "get_app", map[string]any{"app": "shop"}))
	if result := call(t, session, "get_app", map[string]any{"name": "blog"}); !result.IsError {
		t.Fatal("get_app took the old field name")
	}
	install := output[mcp.NewInstall](t, call(t, session, "new_install_command", map[string]any{"server": "box-2"}))
	if !strings.Contains(install.InstallCommand, install.InstallToken) {
		t.Fatalf("new_install_command = %#v", install)
	}
	renamed := output[mcp.ServerOutput](t, call(t, session, "rename_server", map[string]any{"server": "box-2", "name": "box-3"}))
	if renamed.Server.Name != "box-3" {
		t.Fatalf("rename_server = %#v", renamed)
	}
	toolError(t, call(t, session, "remove_server", map[string]any{"server": "box-3", "confirm": "box"}), "confirm_mismatch")
	// box-3 never connected: no daemon to uninstall there.
	if removed := output[mcp.RemovedServer](t, call(t, session, "remove_server", map[string]any{"server": "box-3", "confirm": "box-3"})); removed.UninstallCommand != "" || !strings.Contains(removed.Message, "never connected") {
		t.Fatalf("removed a server that never connected = %+v", removed)
	}
	toolError(t, call(t, session, "remove_app", map[string]any{"app": "shop", "confirm": "blog"}), "confirm_mismatch")
	toolError(t, call(t, session, "restore_app", map[string]any{"app": "shop", "moment": "before-v12", "confirm": "blog"}), "confirm_mismatch")
	if schedule := output[mcp.ScheduleOutput](t, call(t, session, "set_backups", map[string]any{"app": "shop", "every_minutes": 15, "keep_days": 30})); schedule.EveryMinutes != 15 || schedule.KeepDays != 30 {
		t.Fatalf("set_backups = %+v", schedule)
	}
	toolError(t, call(t, session, "set_backups", map[string]any{"app": "shop", "every_minutes": 5, "keep_days": 30}), "validation_failed")
	output[mcp.Done](t, call(t, session, "remove_deploy_hook", map[string]any{"app": "shop"}))
	// Env values go to the server, which must be online.
	toolError(t, call(t, session, "replace_env", map[string]any{"app": "shop", "env": map[string]any{"KEEP": "1"}}), "server_offline")
	offline := online(t, env, owner, "box-1")
	output[mcp.EnvReplaced](t, call(t, session, "replace_env", map[string]any{"app": "shop", "env": map[string]any{"KEEP": "1", "EDIT": "1", "GONE": "1"}}))
	replaced := output[mcp.EnvReplaced](t, call(t, session, "replace_env", map[string]any{"app": "shop", "env": map[string]any{"KEEP": "1", "EDIT": "2", "NEW": "1"}}))
	if strings.Join(replaced.Added, ",") != "NEW" || strings.Join(replaced.Replaced, ",") != "EDIT,KEEP" || strings.Join(replaced.Removed, ",") != "GONE" || len(replaced.Items) != 3 {
		t.Fatalf("replace_env = %+v", replaced)
	}
	// Values are write-only: list_env answers keys and dates, never one.
	listed := output[mcp.Env](t, call(t, session, "list_env", map[string]any{"app": "shop"}))
	if len(listed.Items) != 3 || listed.Items[0].Key != "EDIT" || listed.Items[0].UpdatedAt.IsZero() {
		t.Fatalf("list_env = %+v", listed)
	}
	offline()
	// box-1 is offline: its apps go with it.
	if removed := output[mcp.RemovedServer](t, call(t, session, "remove_server", map[string]any{"server": "box-1", "confirm": "box-1"})); strings.Join(removed.Apps, ",") != "blog,shop" || !strings.Contains(removed.Message, "apps went with it") {
		t.Fatalf("removed an offline server with apps = %+v", removed)
	}
}

// TestExecCommandIsForOwnersAndAdmins: a member's token may not run a
// command, and a command a shell would need quoting for is refused.
func TestExecCommandIsForOwnersAndAdmins(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "execs")
	upgrade(t, env, owner)
	member := connectHTTP(t, env, addMember(t, env, owner, "runner", model.WorkspaceRoleMember, allRights...))
	output[mcp.CreatedServer](t, call(t, member, "add_server", map[string]any{"server": "exec-1"}))
	toolError(t, call(t, member, "exec_command", map[string]any{"app": "blog", "command": "ls /data"}), "forbidden")
	token, _ := createToken(t, env, owner, allRights...)
	session := connectMemory(t, env.server, owner, token)
	fields := toolError(t, call(t, session, "exec_command", map[string]any{"app": "blog", "command": `echo "open`}), "validation_failed").Fields
	if fields["command"] == "" {
		t.Errorf("fields = %v", fields)
	}
}

// TestSecretsGoToServers: create_app shows the app's password once,
// remove_password and set_password without a password make another;
// a registry login, a bucket key and
// the Tailscale key go to the server named, and the lists say which
// servers keep them and which do not.
func TestSecretsGoToServers(t *testing.T) {
	env := newEnvironment(t)
	owner := seedOwner(t, env, "keys")
	upgrade(t, env, owner)
	token, _ := createToken(t, env, owner, allRights...)
	session := connectMemory(t, env.server, owner, token)
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer bucket.Close()

	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "server-1"}))
	online(t, env, owner, "server-1")
	created := output[mcp.CreatedApp](t, call(t, session, "create_app", map[string]any{"app": "shop", "server": "server-1"}))
	if created.Password == "" || !created.App.Password {
		t.Fatalf("create_app = %+v", created)
	}
	if off := output[mcp.Change](t, call(t, session, "remove_password", map[string]any{"app": "shop"})); off.App.Password {
		t.Fatalf("remove_password = %+v", off)
	}
	fresh := output[mcp.NewPassword](t, call(t, session, "set_password", map[string]any{"app": "shop"}))
	if fresh.Password == "" || fresh.Password == created.Password || !fresh.App.Password || strings.Contains(fresh.Summary(), fresh.Password) {
		t.Fatalf("set_password = %+v", fresh)
	}

	output[mcp.CreatedServer](t, call(t, session, "add_server", map[string]any{"server": "server-2"}))
	toolError(t, call(t, session, "set_registry", map[string]any{"host": "ghcr.io", "username": "ada", "password": "token"}), "validation_failed")
	logins := output[mcp.RegistryList](t, call(t, session, "set_registry", map[string]any{"host": "ghcr.io", "username": "ada", "password": "token", "server": "server-1"}))
	if len(logins.Items) != 1 || len(logins.Items[0].Servers) != 1 || strings.Join(logins.Items[0].MissingOn, ",") != "server-2" {
		t.Fatalf("set_registry = %+v", logins)
	}
	added := output[mcp.Bucket](t, call(t, session, "add_bucket", map[string]any{"name": "main", "endpoint": bucket.URL, "bucket": "main", "access_key": "k", "secret_key": "s", "server": "server-1"}))
	if len(added.Servers) != 1 || added.Servers[0].Server != "server-1" || strings.Join(added.MissingOn, ",") != "server-2" {
		t.Fatalf("add_bucket = %+v", added)
	}
	toolError(t, call(t, session, "set_bucket_key", map[string]any{"name": "main", "server": "server-2", "secret_key": "s"}), "server_offline")
	integrations := output[mcp.Integrations](t, call(t, session, "set_integrations", map[string]any{"tailscale_key": "tskey-auth-1", "server": "server-1"}))
	if len(integrations.Tailscale.Servers) != 1 || strings.Join(integrations.Tailscale.MissingOn, ",") != "server-2" {
		t.Fatalf("set_integrations = %+v", integrations)
	}
}
