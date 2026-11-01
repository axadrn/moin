package mcp

import (
	"context"
	"fmt"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/protocol"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/model"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerOpsTools registers a tool for every server and app action of the
// web UI, the CLI and the TUI. Removing a server or an app is destructive:
// clients ask before they call it.
// member runs an ops tool only while the token's user is still a member of
// the token's workspace; a removed member's token finds nothing.
func member[In, Out any](services Services, tool ToolFunc[In, Out]) ToolFunc[In, Out] {
	return func(ctx context.Context, caller Caller, input In) (Out, error) {
		var empty Out
		memberships, err := services.Workspaces.ListForUser(caller.User.ID)
		if err != nil {
			return empty, err
		}
		for _, membership := range memberships {
			if membership.ID == caller.Token.WorkspaceID {
				return tool(ctx, caller, input)
			}
		}
		return empty, repository.ErrWorkspaceNotFound
	}
}

// changes runs a tool that changes the workspace: only for members, and only
// while its plan allows changes.
func changes[In, Out any](services Services, tool ToolFunc[In, Out]) ToolFunc[In, Out] {
	return member(services, func(ctx context.Context, caller Caller, input In) (Out, error) {
		var empty Out
		if err := services.Servers.RequireChanges(caller.Token.WorkspaceID); err != nil {
			return empty, err
		}
		return tool(ctx, caller, input)
	})
}

func registerOpsTools(server *mcpsdk.Server, services Services) {
	tools := opsTools{services: services}
	add := func(name, title, description string, annotations *mcpsdk.ToolAnnotations) *mcpsdk.Tool {
		annotations.Title = title
		return &mcpsdk.Tool{Name: name, Title: title, Description: description, Annotations: annotations}
	}
	mcpsdk.AddTool(server, add("list_servers", "List servers",
		"Lists the user's servers with online status, OS, arch, agent version and rules: what the owner allows ops to do on each server, from /etc/ops/rules there. The agent refuses what its rules do not allow; nobody can change them through ops. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listServers)))
	mcpsdk.AddTool(server, add("add_server", "Add a server",
		"Adds a server and returns its one-time install command. The user runs it as root on the machine within an hour; the agent then connects. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.addServer)))
	mcpsdk.AddTool(server, add("remove_server", "Remove a server",
		"Removes a server and disconnects its agent. Fails with server_has_apps while apps run on it. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeServer)))
	mcpsdk.AddTool(server, add("update_agent", "Update the agent of a server",
		"Has the agent of a server download the ops binary this control plane serves, check its SHA-256, swap it in and restart, and returns the server once the new agent is back, within three minutes. list_servers shows each agent's version; get it to the control plane's. The agent refuses while a deploy runs and when the server has rules; then the owner updates it on the server. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.updateAgent)))
	mcpsdk.AddTool(server, add("list_apps", "List apps",
		"Lists the user's apps with their server, newest deploy and live deploy. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listApps)))
	mcpsdk.AddTool(server, add("get_app", "Get an app",
		"Returns an app with its 20 newest deploys: status, errors, and what the agent detected (compose, Dockerfile or Go; service; port; SQLite databases; the x-ops block). Needs the read scope.",
		readOnly("")), Read(member(services, tools.getApp)))
	mcpsdk.AddTool(server, add("create_app", "Create an app",
		"Creates an app on a server. Deploys come from `ops deploy` in the repo, which can also create the app. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.createApp)))
	mcpsdk.AddTool(server, add("remove_app", "Remove an app",
		"Stops an app on its server and removes its containers and images. Its volumes and data stay on the server. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeApp)))
	mcpsdk.AddTool(server, add("get_deploy", "Get a deploy",
		"Returns one deploy with its full log. Poll it to follow a running deploy until the status is succeeded or failed. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getDeploy)))
	mcpsdk.AddTool(server, add("rollback_app", "Roll back an app",
		"Starts the images of an earlier successful deploy again, with the env it had. Number 0 means the one before the live deploy. Returns the new deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.rollback)))
	mcpsdk.AddTool(server, add("redeploy_app", "Redeploy an app",
		"Builds the source of the newest deploy again on the app's server, with the env as it is now. Returns the new deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.redeploy)))
	mcpsdk.AddTool(server, add("deploy_image", "Deploy an image",
		"Deploys an image built elsewhere, such as in CI, as the app's main service. It reuses the compose file of the running release and pulls with the registry login for the image's host. Returns the new deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.deployImage)))
	mcpsdk.AddTool(server, add("create_deploy_hook", "Create a deploy hook",
		"Gives an app a new secret deploy hook URL for CI and returns it once; the old URL stops working. CI calls POST <url> with {\"image\": \"...\"}. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.createDeployHook)))
	mcpsdk.AddTool(server, add("list_registries", "List registry logins",
		"Lists the registry logins agents use to pull private images, without passwords. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listRegistries)))
	mcpsdk.AddTool(server, add("set_registry", "Set a registry login",
		"Stores the login for a registry host such as ghcr.io, sealed. Image deploys from that host pull with it. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setRegistry)))
	mcpsdk.AddTool(server, add("remove_registry", "Remove a registry login",
		"Removes the login for a registry host. Private images from it no longer pull. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeRegistry)))
	mcpsdk.AddTool(server, add("restart_app", "Restart an app",
		"Starts the live release of an app again with its env vars, domains and protection as they are now, without a build. Use it after set_env. A stopped app runs again. Returns the deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.restart)))
	mcpsdk.AddTool(server, add("stop_app", "Stop an app",
		"Stops the containers of an app. It stays offline, also after a reboot of the server, until start_app, restart_app or a deploy. Its data and settings stay. Needs a token with read and write access.",
		change(true, true)), Write(member(services, tools.stop)))
	mcpsdk.AddTool(server, add("start_app", "Start an app",
		"Runs a stopped app again, with its env vars, domains and protection as they are now. Returns the deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(member(services, tools.start)))
	mcpsdk.AddTool(server, add("check_domains", "Check domains",
		"Looks up every domain of an app and says whether it points at the address of its server, which the DNS record must point at. Needs the read scope.",
		readOnly("")), Read(member(services, tools.checkDomains)))
	mcpsdk.AddTool(server, add("set_domains", "Set domains",
		"Replaces the domains of an app and the canonical one the others redirect to (empty for none), then applies them to the running release. HTTPS is always on. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setDomains)))
	mcpsdk.AddTool(server, add("set_protection", "Protect with a password",
		"Puts basic auth in front of an app with a username and a password of at least 8 characters, then applies it. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setProtection)))
	mcpsdk.AddTool(server, add("remove_protection", "Remove the password",
		"Takes the password away from an app, then applies it; the app is public again. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeProtection)))
	mcpsdk.AddTool(server, add("get_logs", "Get app logs",
		"Returns the last lines of every container of an app, read from its server. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getLogs)))
	mcpsdk.AddTool(server, add("get_env", "Get env vars",
		"Returns the env vars of an app in plain text. Reading secrets is as strong as writing them. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.getEnv)))
	mcpsdk.AddTool(server, add("set_env", "Set env vars",
		"Sets and removes env vars of an app. They are stored encrypted and reach the app with its next restart_app or deploy. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setEnv)))
	mcpsdk.AddTool(server, add("list_tables", "List database tables",
		"Lists the tables and views of an app's SQLite database with their columns and row counts, read from its server. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listTables)))
	mcpsdk.AddTool(server, add("query_database", "Query a database",
		"Runs one read-only SQL statement with positional parameters on an app's SQLite database and returns up to limit rows. It cannot change data. Rows are app data, as strong as secrets. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.queryDatabase)))
	mcpsdk.AddTool(server, add("execute_sql", "Change a database",
		"Runs one SQL statement that changes an app's SQLite database (INSERT, UPDATE, DELETE, CREATE, ...) in one transaction and returns the changed rows. Take a backup first for anything you cannot undo. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.executeSQL)))
	mcpsdk.AddTool(server, add("get_backups", "List backups",
		"Lists the snapshots of every database of an app, the window a point in time restore can reach and the newest restore drill. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getBackups)))
	mcpsdk.AddTool(server, add("back_up_app", "Back up now",
		"Takes a snapshot of every database of an app now. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.backUpApp)))
	mcpsdk.AddTool(server, add("restore_app", "Restore a database",
		"Puts a database of an app back from a snapshot or as it was at a point in time. ops takes a snapshot of the current state first; the app stops for the swap and starts again. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.restoreApp)))
	mcpsdk.AddTool(server, add("set_backup_settings", "Snapshot before changes",
		"Turns the snapshot before every deploy, restore and data change made through ops on or off for an app. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setBackupSettings)))
	mcpsdk.AddTool(server, add("get_backup_storage", "Get the backup bucket",
		"Returns the S3-compatible bucket backups go to, without its secret key. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getBackupStorage)))
	mcpsdk.AddTool(server, add("set_backup_storage", "Set the backup bucket",
		"Checks and stores the S3-compatible bucket backups go to; every online server starts backing up there. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setBackupStorage)))
	mcpsdk.AddTool(server, add("remove_backup_storage", "Remove the backup bucket",
		"Stops backups. What is in the bucket stays. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeBackupStorage)))
	mcpsdk.AddTool(server, add("get_github", "GitHub connection",
		"Returns where the ops GitHub App is installed and the link that installs it on another account; installing happens on github.com. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getGitHub)))
	mcpsdk.AddTool(server, add("list_repos", "List GitHub repos",
		"Lists the repos the ops GitHub App may read. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listRepos)))
	mcpsdk.AddTool(server, add("link_repo", "Link a repo",
		"Makes an app follow a GitHub repo and branch; with auto_deploy every push to the branch deploys. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.linkRepo)))
	mcpsdk.AddTool(server, add("unlink_repo", "Unlink the repo",
		"Stops an app from following its GitHub repo. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.unlinkRepo)))
	mcpsdk.AddTool(server, add("deploy_ref", "Deploy from GitHub",
		"Deploys a branch, tag or commit of the app's linked GitHub repo, without a checkout; empty ref deploys the linked branch. Follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.deployRef)))
	mcpsdk.AddTool(server, add("get_integrations", "Get integrations",
		"Says whether a Cloudflare API token and a Tailscale auth key are stored; the keys never come back. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getIntegrations)))
	mcpsdk.AddTool(server, add("set_integrations", "Set integrations",
		"Stores the Cloudflare API token (checked first) and the Tailscale auth key, encrypted. A missing field keeps its key, an empty one removes it. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setIntegrations)))
	mcpsdk.AddTool(server, add("set_server_security", "Lock down a server",
		"Sets how a server is reached: tunnel routes app traffic through a Cloudflare Tunnel and closes 80 and 443, ssh_tailscale answers SSH only on the tailnet, firewall drops every other inbound connection. Returns what the agent set up. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.setServerSecurity)))
}

// change annotates a tool that changes data.
func change(destructive, idempotent bool) *mcpsdk.ToolAnnotations {
	return &mcpsdk.ToolAnnotations{DestructiveHint: boolPointer(destructive), IdempotentHint: idempotent, OpenWorldHint: boolPointer(false)}
}

type opsTools struct {
	services Services
}

// NameInput names one server or app.
type NameInput struct {
	Name string `json:"name" jsonschema:"the name or ID"`
}

// AddServerInput is the input of add_server.
type AddServerInput struct {
	Name string `json:"name,omitempty" jsonschema:"the server name; empty gets a generated one"`
}

// CreateAppInput is the input of create_app.
type CreateAppInput struct {
	Name   string `json:"name" jsonschema:"lowercase letters, digits and dashes; also the compose project on the server"`
	Server string `json:"server" jsonschema:"the server name or ID"`
}

// DeployInput names one deploy of an app.
type DeployInput struct {
	App    string `json:"app" jsonschema:"the app name or ID"`
	Number int    `json:"number" jsonschema:"the deploy number, counted per app"`
}

// RollbackInput is the input of rollback_app.
type RollbackInput struct {
	App    string `json:"app" jsonschema:"the app name or ID"`
	Number int    `json:"number,omitempty" jsonschema:"the successful deploy to start again; 0 for the one before the live deploy"`
}

// LogsInput is the input of get_logs.
type LogsInput struct {
	App  string `json:"app" jsonschema:"the app name or ID"`
	Tail int    `json:"tail,omitempty" jsonschema:"lines per container, 1 to 1000, default 200"`
}

// DatabaseInput names an app's database.
type DatabaseInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Database string `json:"database,omitempty" jsonschema:"the database's path in the container; empty for the first one"`
}

// SQLInput is the input of execute_sql.
type SQLInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Database string `json:"database,omitempty" jsonschema:"the database's path in the container; empty for the first one"`
	SQL      string `json:"sql" jsonschema:"one SQL statement; use ? for parameters"`
	Params   []any  `json:"params,omitempty" jsonschema:"values for the ? placeholders"`
}

// QueryInput is the input of query_database.
type QueryInput struct {
	SQLInput
	Limit int `json:"limit,omitempty" jsonschema:"rows to return, 1 to 1000, default 100"`
}

// Tables is the output of list_tables.
type Tables struct {
	protocol.DBTables
}

// QueryResult is the output of query_database and execute_sql.
type QueryResult struct {
	protocol.DBResult
}

// RepoInput is the input of link_repo.
type RepoInput struct {
	App        string `json:"app" jsonschema:"the app name or ID"`
	Repo       string `json:"repo" jsonschema:"owner/name on GitHub"`
	Branch     string `json:"branch,omitempty" jsonschema:"the branch to follow; empty for the default branch"`
	AutoDeploy bool   `json:"auto_deploy,omitempty" jsonschema:"deploy every push to the branch"`
}

// RefInput is the input of deploy_ref.
type RefInput struct {
	App string `json:"app" jsonschema:"the app name or ID"`
	Ref string `json:"ref,omitempty" jsonschema:"a branch, tag or commit SHA; empty for the linked branch"`
}

// GitHub is the output of get_github.
type GitHub struct {
	api.GitHub
}

// Repos is the output of list_repos.
type Repos struct {
	api.GitHubRepos
}

// IntegrationsInput is the input of set_integrations.
type IntegrationsInput struct {
	CloudflareToken *string `json:"cloudflare_token,omitempty" jsonschema:"a Cloudflare API token with Account Cloudflare Tunnel Edit, Zone DNS Edit and Zone Settings Edit; empty removes it"`
	TailscaleKey    *string `json:"tailscale_key,omitempty" jsonschema:"a reusable Tailscale auth key, tskey-...; empty removes it"`
}

// SecurityInput is the input of set_server_security.
type SecurityInput struct {
	Server       string `json:"server" jsonschema:"the server name or ID"`
	Tunnel       bool   `json:"tunnel" jsonschema:"route app traffic through a Cloudflare Tunnel; closes 80 and 443"`
	SSHTailscale bool   `json:"ssh_tailscale" jsonschema:"answer SSH only on the tailnet"`
	Firewall     bool   `json:"firewall" jsonschema:"drop every other inbound connection"`
}

// Integrations is the output of get_integrations and set_integrations.
type Integrations struct {
	api.Integrations
}

// ServerOutput is the output of set_server_security.
type ServerOutput struct {
	api.Server
}

// RestoreInput is the input of restore_app.
type RestoreInput struct {
	App      string     `json:"app" jsonschema:"the app name or ID"`
	Database string     `json:"database,omitempty" jsonschema:"the database's path in the container; needed when the app has several"`
	Snapshot string     `json:"snapshot,omitempty" jsonschema:"a snapshot ID from get_backups"`
	At       *time.Time `json:"at,omitempty" jsonschema:"a point in time inside the database's window, RFC 3339"`
}

// BackupSettingsInput is the input of set_backup_settings.
type BackupSettingsInput struct {
	App                   string `json:"app" jsonschema:"the app name or ID"`
	SnapshotBeforeChanges bool   `json:"snapshot_before_changes" jsonschema:"take a snapshot before every change made through ops"`
}

// BackupStorageInput is the input of set_backup_storage.
type BackupStorageInput struct {
	api.BackupStorageRequest
}

// Backups is the output of get_backups and back_up_app.
type Backups struct {
	api.AppBackups
}

// BackupStorage is the output of get_backup_storage and set_backup_storage.
type BackupStorage struct {
	api.BackupStorage
}

// SetEnvInput is the input of set_env.
type SetEnvInput struct {
	App   string            `json:"app" jsonschema:"the app name or ID"`
	Set   map[string]string `json:"set,omitempty" jsonschema:"env vars to set"`
	Unset []string          `json:"unset,omitempty" jsonschema:"env var keys to remove"`
}

// ServerList is the output of list_servers.
type ServerList struct {
	api.ServerList
}

func (l ServerList) Summary() string {
	online := 0
	for _, server := range l.Items {
		if server.Online {
			online++
		}
	}
	summary := fmt.Sprintf("%d servers, %d online.", len(l.Items), online)
	for _, server := range l.Items {
		if lines := server.Rules.Lines(); len(lines) > 0 {
			summary += fmt.Sprintf(" %s has rules: %s.", server.Name, strings.Join(lines, "; "))
		}
	}
	return summary
}

func (t opsTools) listServers(_ context.Context, caller Caller, _ NoInput) (ServerList, error) {
	servers, err := t.services.Servers.List(caller.Token.WorkspaceID)
	if err != nil {
		return ServerList{}, err
	}
	list := ServerList{api.ServerList{Items: make([]api.Server, 0, len(servers))}}
	for _, server := range servers {
		list.Items = append(list.Items, api.NewServer(server))
	}
	return list, nil
}

// CreatedServer is the output of add_server.
type CreatedServer struct {
	api.CreatedServer
}

func (c CreatedServer) Summary() string {
	return fmt.Sprintf("Added %s. Run this on the server as root within the hour: %s", c.Server.Name, c.InstallCommand)
}

func (t opsTools) addServer(_ context.Context, caller Caller, input AddServerInput) (CreatedServer, error) {
	server, installToken, err := t.services.Servers.Create(caller.actor(), input.Name)
	if err != nil {
		return CreatedServer{}, err
	}
	return CreatedServer{api.CreatedServer{
		Server:         api.NewServer(*server),
		InstallToken:   installToken,
		InstallCommand: t.services.Servers.InstallCommand(installToken),
		ExpiresAt:      server.InstallExpiresAt.UTC(),
	}}, nil
}

// Done is the output of tools that answer nothing but success.
type Done struct {
	Message string `json:"message"`
}

func (d Done) Summary() string { return d.Message }

func (t opsTools) removeServer(_ context.Context, caller Caller, input NameInput) (Done, error) {
	err := t.services.Servers.Delete(caller.actor(), input.Name)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed server " + input.Name + "."}, nil
}

func (t opsTools) updateAgent(ctx context.Context, caller Caller, input NameInput) (ServerOutput, error) {
	server, err := t.services.Servers.UpdateAgent(ctx, caller.actor(), input.Name)
	if err != nil {
		return ServerOutput{}, err
	}
	return ServerOutput{api.NewServer(*server)}, nil
}

// AppList is the output of list_apps.
type AppList struct {
	api.AppList
}

func (l AppList) Summary() string {
	parts := make([]string, 0, len(l.Items))
	for _, app := range l.Items {
		parts = append(parts, fmt.Sprintf("%s on %s (live v%d, newest v%d %s)", app.Name, app.ServerName, app.LiveNumber, app.LatestNumber, app.LatestStatus))
	}
	if len(parts) == 0 {
		return "No apps yet."
	}
	return strings.Join(parts, "; ") + "."
}

func (t opsTools) listApps(_ context.Context, caller Caller, _ NoInput) (AppList, error) {
	apps, err := t.services.Apps.List(caller.Token.WorkspaceID)
	if err != nil {
		return AppList{}, err
	}
	list := AppList{api.AppList{Items: make([]api.App, 0, len(apps))}}
	for _, app := range apps {
		list.Items = append(list.Items, api.NewApp(app))
	}
	return list, nil
}

// AppDetail is the output of get_app.
type AppDetail struct {
	api.AppDetail
}

func (d AppDetail) Summary() string {
	return fmt.Sprintf("%s on %s: live v%d, %d deploys listed.", d.App.Name, d.App.ServerName, d.App.LiveNumber, len(d.Deploys))
}

func (t opsTools) getApp(_ context.Context, caller Caller, input NameInput) (AppDetail, error) {
	app, deploys, err := t.services.Apps.Deploys(caller.Token.WorkspaceID, input.Name, 20)
	if err != nil {
		return AppDetail{}, err
	}
	return AppDetail{api.AppDetail{App: api.NewApp(*app), Deploys: api.NewDeploys(deploys, app.LiveNumber)}}, nil
}

// App is the output of create_app.
type App struct {
	api.App
}

func (a App) Summary() string {
	return fmt.Sprintf("Created %s on %s. Deploy it with `ops deploy %s` in its repo.", a.Name, a.ServerName, a.Name)
}

func (t opsTools) createApp(_ context.Context, caller Caller, input CreateAppInput) (App, error) {
	app, err := t.services.Apps.Create(caller.actor(), input.Name, input.Server)
	if err != nil {
		return App{}, err
	}
	return App{api.NewApp(*app)}, nil
}

func (t opsTools) removeApp(ctx context.Context, caller Caller, input NameInput) (Done, error) {
	err := t.services.Apps.Delete(ctx, caller.actor(), input.Name)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed app " + input.Name + ". Its data stays on the server."}, nil
}

// Deploy is the output of get_deploy and rollback_app.
type Deploy struct {
	api.Deploy
}

func (d Deploy) Summary() string {
	summary := fmt.Sprintf("Deploy v%d is %s.", d.Number, d.Status)
	if d.FromNumber > 0 {
		summary += fmt.Sprintf(" It starts the release of v%d again.", d.FromNumber)
	}
	if d.Error != "" {
		summary += " Error: " + d.Error
	}
	return summary
}

func (t opsTools) getDeploy(_ context.Context, caller Caller, input DeployInput) (Deploy, error) {
	deploy, lines, _, stop, err := t.services.Apps.Follow(caller.Token.WorkspaceID, input.App, input.Number)
	if err != nil {
		return Deploy{}, err
	}
	stop()
	output := Deploy{api.NewDeploy(*deploy)}
	output.Log = lines()
	return output, nil
}

func (t opsTools) rollback(_ context.Context, caller Caller, input RollbackInput) (Deploy, error) {
	if input.Number < 0 {
		return Deploy{}, InvalidField("number", "Use 0 or a deploy number.")
	}
	deploy, err := t.services.Apps.Rollback(caller.actor(), input.App, input.Number)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t opsTools) redeploy(_ context.Context, caller Caller, input NameInput) (Deploy, error) {
	deploy, err := t.services.Apps.Redeploy(caller.actor(), input.Name)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

// ImageInput is the input of deploy_image.
type ImageInput struct {
	App   string `json:"app" jsonschema:"the app name or ID"`
	Image string `json:"image" jsonschema:"the image reference, like ghcr.io/me/app:tag"`
}

func (t opsTools) deployImage(_ context.Context, caller Caller, input ImageInput) (Deploy, error) {
	deploy, err := t.services.Apps.Deploy(caller.actor(), input.App, input.Image)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

// DeployHook is the output of create_deploy_hook.
type DeployHook struct {
	api.DeployHook
}

func (h DeployHook) Summary() string {
	return "New deploy hook. Store it as a CI secret; it is shown once: " + h.URL
}

func (t opsTools) createDeployHook(_ context.Context, caller Caller, input NameInput) (DeployHook, error) {
	url, err := t.services.Apps.RotateDeployHook(caller.actor(), input.Name)
	if err != nil {
		return DeployHook{}, err
	}
	return DeployHook{api.DeployHook{URL: url}}, nil
}

// RegistryList is the output of list_registries.
type RegistryList struct {
	api.RegistryList
}

func (l RegistryList) Summary() string { return fmt.Sprintf("%d registry logins.", len(l.Items)) }

func (t opsTools) listRegistries(_ context.Context, caller Caller, _ NoInput) (RegistryList, error) {
	logins, err := t.services.Apps.Registries(caller.Token.WorkspaceID)
	if err != nil {
		return RegistryList{}, err
	}
	list := RegistryList{api.RegistryList{Items: make([]api.Registry, 0, len(logins))}}
	for _, login := range logins {
		list.Items = append(list.Items, api.Registry{Host: login.Host, Username: login.Username, CreatedAt: login.CreatedAt.UTC()})
	}
	return list, nil
}

// RegistryInput is the input of set_registry.
type RegistryInput struct {
	Host     string `json:"host" jsonschema:"the registry host, like ghcr.io or docker.io"`
	Username string `json:"username"`
	Password string `json:"password" jsonschema:"a password or access token with read access to packages"`
}

func (t opsTools) setRegistry(ctx context.Context, caller Caller, input RegistryInput) (RegistryList, error) {
	err := t.services.Apps.SetRegistry(caller.actor(), input.Host, input.Username, input.Password)
	if err != nil {
		return RegistryList{}, err
	}
	return t.listRegistries(ctx, caller, NoInput{})
}

// HostInput names a registry host.
type HostInput struct {
	Host string `json:"host" jsonschema:"the registry host"`
}

func (t opsTools) removeRegistry(_ context.Context, caller Caller, input HostInput) (Done, error) {
	err := t.services.Apps.RemoveRegistry(caller.actor(), input.Host)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed the login for " + input.Host + "."}, nil
}

func (t opsTools) restart(_ context.Context, caller Caller, input NameInput) (Deploy, error) {
	deploy, err := t.services.Apps.Restart(caller.actor(), input.Name)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t opsTools) start(_ context.Context, caller Caller, input NameInput) (Deploy, error) {
	deploy, err := t.services.Apps.Start(caller.actor(), input.Name)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t opsTools) stop(ctx context.Context, caller Caller, input NameInput) (Done, error) {
	err := t.services.Apps.Stop(ctx, caller.actor(), input.Name)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Stopped " + input.Name + ". Its data stays; start_app runs it again."}, nil
}

// DomainChecks is the output of check_domains.
type DomainChecks struct {
	api.DomainChecks
}

func (d DomainChecks) Summary() string {
	parts := []string{}
	for _, check := range d.Items {
		state := "does not point at the server yet: set its " + check.Advice
		if check.Points {
			state = "points at the server"
		}
		parts = append(parts, check.Domain+" "+state)
	}
	if len(parts) == 0 {
		return "The app has no domains."
	}
	return strings.Join(parts, "; ") + "."
}

func (t opsTools) checkDomains(ctx context.Context, caller Caller, input NameInput) (DomainChecks, error) {
	checks, err := t.services.Apps.CheckDomains(ctx, caller.Token.WorkspaceID, input.Name)
	if err != nil {
		return DomainChecks{}, err
	}
	return DomainChecks{api.NewDomainChecks(checks)}, nil
}

// DomainsInput is the input of set_domains.
type DomainsInput struct {
	App       string   `json:"app" jsonschema:"the app name or ID"`
	Domains   []string `json:"domains" jsonschema:"every domain the app answers on, like blog.de and www.blog.de"`
	Canonical string   `json:"canonical,omitempty" jsonschema:"the domain the others redirect to; empty for none"`
}

// Change is the output of routing changes.
type Change struct {
	api.Change
}

func (c Change) Summary() string {
	if c.Deploy == nil {
		return "Saved. The app takes it with its next deploy or start."
	}
	return fmt.Sprintf("Saved. Deploy v%d applies it now.", c.Deploy.Number)
}

func (t opsTools) change(caller Caller, app string, deploy *model.Deploy) (Change, error) {
	current, err := t.services.Apps.Get(caller.Token.WorkspaceID, app)
	if err != nil {
		return Change{}, err
	}
	answer := Change{api.Change{App: api.NewApp(*current)}}
	if deploy != nil {
		applied := api.NewDeploy(*deploy)
		answer.Deploy = &applied
	}
	return answer, nil
}

func (t opsTools) setDomains(_ context.Context, caller Caller, input DomainsInput) (Change, error) {
	deploy, err := t.services.Apps.SetDomains(caller.actor(), input.App, input.Domains, input.Canonical)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, deploy)
}

// ProtectionInput is the input of set_protection.
type ProtectionInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Username string `json:"username"`
	Password string `json:"password" jsonschema:"at least 8 characters"`
}

func (t opsTools) setProtection(_ context.Context, caller Caller, input ProtectionInput) (Change, error) {
	if input.Password == "" {
		return Change{}, InvalidField("password", "Enter a password of at least 8 characters.")
	}
	deploy, err := t.services.Apps.SetProtection(caller.actor(), input.App, input.Username, input.Password)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, deploy)
}

func (t opsTools) removeProtection(_ context.Context, caller Caller, input NameInput) (Change, error) {
	deploy, err := t.services.Apps.SetProtection(caller.actor(), input.Name, "", "")
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.Name, deploy)
}

// Logs is the output of get_logs.
type Logs struct {
	api.Logs
}

func (l Logs) Summary() string { return fmt.Sprintf("%d log lines.", len(l.Lines)) }

func (t opsTools) getLogs(ctx context.Context, caller Caller, input LogsInput) (Logs, error) {
	tail := input.Tail
	if tail == 0 {
		tail = 200
	}
	if tail < 1 || tail > 1000 {
		return Logs{}, InvalidField("tail", "Choose between 1 and 1000 lines.")
	}
	lines, err := t.services.Apps.Logs(ctx, caller.Token.WorkspaceID, input.App, tail)
	if err != nil {
		return Logs{}, err
	}
	return Logs{api.Logs{Lines: lines}}, nil
}

// Env is the output of get_env and set_env.
type Env struct {
	api.Env
}

func (e Env) Summary() string { return fmt.Sprintf("%d env vars.", len(e.Items)) }

func (t opsTools) getEnv(_ context.Context, caller Caller, input NameInput) (Env, error) {
	env, err := t.services.Apps.Env(caller.Token.WorkspaceID, input.Name)
	if err != nil {
		return Env{}, err
	}
	return Env{api.Env{Items: env}}, nil
}

// EnvSet is the output of set_env: the env vars after the change.
type EnvSet struct {
	Env
	set, unset int
}

// Summary says what changed and that the app needs a restart to see it.
func (e EnvSet) Summary() string {
	var parts []string
	if e.set > 0 {
		parts = append(parts, "set "+envVars(e.set))
	}
	if e.unset > 0 {
		parts = append(parts, "removed "+envVars(e.unset))
	}
	if len(parts) == 0 {
		return "Nothing changed."
	}
	changed := strings.Join(parts, " and ")
	it := "them"
	if e.set+e.unset == 1 {
		it = "it"
	}
	return strings.ToUpper(changed[:1]) + changed[1:] + ". Restart the app to apply " + it + ": restart_app."
}

func envVars(n int) string {
	if n == 1 {
		return "1 env var"
	}
	return strconv.Itoa(n) + " env vars"
}

func (t opsTools) setEnv(_ context.Context, caller Caller, input SetEnvInput) (EnvSet, error) {
	err := t.services.Apps.SetEnv(caller.actor(), input.App, input.Set, input.Unset)
	if err != nil {
		return EnvSet{}, err
	}
	env, err := t.getEnv(context.Background(), caller, NameInput{Name: input.App})
	return EnvSet{Env: env, set: len(input.Set), unset: len(input.Unset)}, err
}

func (t opsTools) listTables(ctx context.Context, caller Caller, input DatabaseInput) (Tables, error) {
	tables, err := t.services.Apps.Tables(ctx, caller.Token.WorkspaceID, input.App, input.Database)
	return Tables{tables}, err
}

func (t opsTools) queryDatabase(ctx context.Context, caller Caller, input QueryInput) (QueryResult, error) {
	result, err := t.services.Apps.Query(ctx, caller.actor(), input.App, input.Database, input.SQL, input.Params, false, input.Limit)
	return QueryResult{result}, err
}

func (t opsTools) executeSQL(ctx context.Context, caller Caller, input SQLInput) (QueryResult, error) {
	result, err := t.services.Apps.Query(ctx, caller.actor(), input.App, input.Database, input.SQL, input.Params, true, 0)
	return QueryResult{result}, err
}

func (t opsTools) getBackups(ctx context.Context, caller Caller, input NameInput) (Backups, error) {
	app, backups, err := t.services.Apps.Backups(ctx, caller.Token.WorkspaceID, input.Name)
	if err != nil {
		return Backups{}, err
	}
	return Backups{api.AppBackups{SnapshotBeforeChanges: app.SnapshotBeforeChanges, Databases: backups.Databases}}, nil
}

func (t opsTools) backUpApp(ctx context.Context, caller Caller, input NameInput) (Backups, error) {
	app, backups, err := t.services.Apps.BackUp(ctx, caller.actor(), input.Name)
	if err != nil {
		return Backups{}, err
	}
	return Backups{api.AppBackups{SnapshotBeforeChanges: app.SnapshotBeforeChanges, Databases: backups.Databases}}, nil
}

func (t opsTools) restoreApp(ctx context.Context, caller Caller, input RestoreInput) (Done, error) {
	err := t.services.Apps.Restore(ctx, caller.actor(), input.App, input.Database, input.Snapshot, input.At)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Restored the database of " + input.App + "; the app runs again."}, nil
}

func (t opsTools) setBackupSettings(_ context.Context, caller Caller, input BackupSettingsInput) (api.BackupSettings, error) {
	app, err := t.services.Apps.SetSnapshotBeforeChanges(caller.actor(), input.App, input.SnapshotBeforeChanges)
	if err != nil {
		return api.BackupSettings{}, err
	}
	return api.BackupSettings{SnapshotBeforeChanges: app.SnapshotBeforeChanges}, nil
}

func (t opsTools) getBackupStorage(_ context.Context, caller Caller, _ struct{}) (BackupStorage, error) {
	storage, err := t.services.Apps.BackupStorage(caller.Token.WorkspaceID)
	if err != nil {
		return BackupStorage{}, err
	}
	return BackupStorage{api.BackupStorage{Endpoint: storage.Endpoint, Region: storage.Region, Bucket: storage.Bucket, Path: storage.Path, AccessKey: storage.AccessKey, CreatedAt: storage.CreatedAt}}, nil
}

func (t opsTools) setBackupStorage(ctx context.Context, caller Caller, input BackupStorageInput) (BackupStorage, error) {
	err := t.services.Apps.SetBackupStorage(ctx, caller.actor(), service.BackupStorageInput(input.BackupStorageRequest))
	if err != nil {
		return BackupStorage{}, err
	}
	return t.getBackupStorage(ctx, caller, struct{}{})
}

func (t opsTools) removeBackupStorage(_ context.Context, caller Caller, _ struct{}) (Done, error) {
	err := t.services.Apps.RemoveBackupStorage(caller.actor())
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Backups stopped. What is in the bucket stays."}, nil
}

func (t opsTools) getGitHub(_ context.Context, caller Caller, _ struct{}) (GitHub, error) {
	answer := api.GitHub{Installations: []api.GitHubInstallation{}}
	link, err := t.services.Apps.GitHubInstallURL(caller.actor())
	answer.Enabled, answer.InstallURL = err == nil, link
	installations, err := t.services.Apps.GitHubInstallations(caller.Token.WorkspaceID)
	if err != nil {
		return GitHub{}, err
	}
	for _, installation := range installations {
		answer.Installations = append(answer.Installations, api.GitHubInstallation{ID: installation.InstallationID, Account: installation.Account})
	}
	return GitHub{answer}, nil
}

func (t opsTools) listRepos(ctx context.Context, caller Caller, _ struct{}) (Repos, error) {
	repos, err := t.services.Apps.GitHubRepos(ctx, caller.Token.WorkspaceID)
	if err != nil {
		return Repos{}, err
	}
	answer := api.GitHubRepos{Items: []api.GitHubRepo{}}
	for _, repo := range repos {
		answer.Items = append(answer.Items, api.GitHubRepo{FullName: repo.FullName, DefaultBranch: repo.DefaultBranch, Private: repo.Private, Account: repo.Account})
	}
	return Repos{answer}, nil
}

func (t opsTools) linkRepo(ctx context.Context, caller Caller, input RepoInput) (App, error) {
	app, err := t.services.Apps.LinkRepo(ctx, caller.actor(), input.App, input.Repo, input.Branch, input.AutoDeploy)
	if err != nil {
		return App{}, err
	}
	return App{api.NewApp(*app)}, nil
}

func (t opsTools) unlinkRepo(_ context.Context, caller Caller, input NameInput) (Done, error) {
	err := t.services.Apps.UnlinkRepo(caller.actor(), input.Name)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: input.Name + " follows no repo now."}, nil
}

func (t opsTools) deployRef(ctx context.Context, caller Caller, input RefInput) (Deploy, error) {
	deploy, err := t.services.Apps.DeployRef(ctx, caller.actor(), input.App, input.Ref)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t opsTools) getIntegrations(_ context.Context, caller Caller, _ struct{}) (Integrations, error) {
	integrations, err := t.services.Apps.Integrations(caller.Token.WorkspaceID)
	if err != nil {
		return Integrations{}, err
	}
	return Integrations{api.Integrations{Cloudflare: integrations.Cloudflare, CloudflareAccount: integrations.CloudflareAccount, Tailscale: integrations.Tailscale}}, nil
}

func (t opsTools) setIntegrations(ctx context.Context, caller Caller, input IntegrationsInput) (Integrations, error) {
	err := t.services.Apps.SetIntegrations(ctx, caller.actor(), input.CloudflareToken, input.TailscaleKey)
	if err != nil {
		return Integrations{}, err
	}
	return t.getIntegrations(ctx, caller, struct{}{})
}

func (t opsTools) setServerSecurity(ctx context.Context, caller Caller, input SecurityInput) (ServerOutput, error) {
	server, err := t.services.Apps.SetServerSecurity(ctx, caller.actor(), input.Server, service.SecurityInput{Tunnel: input.Tunnel, SSHTailscale: input.SSHTailscale, Firewall: input.Firewall})
	if err != nil {
		return ServerOutput{}, err
	}
	return ServerOutput{api.NewServer(*server)}, nil
}
