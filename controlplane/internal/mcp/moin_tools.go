package mcp

import (
	"cmp"
	"context"
	"fmt"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/protocol"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/controlplane/internal/model"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerOpsTools registers a tool for every server and app action of the
// web UI, the CLI and the TUI. Removing a server or an app is destructive:
// clients ask before they call it.
// member runs a moin tool only while the token's user is still a member of
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
	tools := moinTools{services: services}
	add := func(name, title, description string, annotations *mcpsdk.ToolAnnotations) *mcpsdk.Tool {
		annotations.Title = title
		return &mcpsdk.Tool{Name: name, Title: title, Description: description, Annotations: annotations}
	}
	mcpsdk.AddTool(server, add("list_servers", "List servers",
		"Lists the user's servers with online status, OS, arch, agent version and rules: what the owner allows moin to do on each server, from /etc/moin/rules there. The agent refuses what its rules do not allow; nobody can change them through moin. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listServers)))
	mcpsdk.AddTool(server, add("add_server", "Add a server",
		"Adds a server and returns its one-time install command. The user runs it as root on the machine within an hour; the agent then connects. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.addServer)))
	mcpsdk.AddTool(server, add("remove_server", "Remove a server",
		"Removes a server and disconnects its agent. Fails with server_has_apps while it is online and has apps; an offline server goes with its apps, listed in apps, which stay on the machine if it comes back. The agent stays installed on the machine: returns uninstall_command, which the user runs on the server to remove it, empty when no agent ever connected; the apps there keep running with their data unless it gets --apps. Ask the user first: confirm must be the server's name, typed out. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeServer)))
	mcpsdk.AddTool(server, add("new_install_command", "New install command",
		"Makes a new one-time install command for a server and returns it, for a lost command or to move the agent to another machine. It works once within an hour; moin stores only a fingerprint, so it is shown this once. The old install command stops working, and an agent that registers with the new one takes over from the old agent. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.newInstallCommand)))
	mcpsdk.AddTool(server, add("rename_server", "Rename a server",
		"Gives a server a new name, 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit, unique in the workspace, and returns it. Its apps stay on it. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.renameServer)))
	mcpsdk.AddTool(server, add("update_agent", "Update the agent of a server",
		"Has the agent of a server download the moin binary this control plane serves, check its SHA-256, swap it in and restart, and returns the server once the new agent is back, within three minutes. list_servers shows each agent's version; get it to the control plane's. The agent refuses while a deploy runs and when the server has rules; then the owner updates it on the server. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.updateAgent)))
	mcpsdk.AddTool(server, add("reorder_servers", "Order the servers",
		"Puts the servers of the workspace in a new order and returns them in it. order names every server once, by name or ID; list_servers gives the current order. Every list shows it: the web, the CLI, the TUI, the API and list_servers. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.reorderServers)))
	mcpsdk.AddTool(server, add("list_apps", "List apps",
		"Lists the user's apps with their server, newest deploy and live deploy. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listApps)))
	mcpsdk.AddTool(server, add("get_app", "Get an app",
		"Returns an app with its 20 newest deploys: status, errors, and what the agent detected (compose, Dockerfile, Go, or Railpack with the language and start command it found; service; port; databases, SQLite and Postgres; the x-moin block). Needs the read scope.",
		readOnly("")), Read(member(services, tools.getApp)))
	mcpsdk.AddTool(server, add("create_app", "Create an app",
		"Creates an app on a server. Deploys come from `moin deploy` in the repo, which can also create the app. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.createApp)))
	mcpsdk.AddTool(server, add("remove_app", "Remove an app",
		"Stops an app on its server and removes its containers and images. Its volumes and data stay on the server. Ask the user first: confirm must be the app's name, typed out. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeApp)))
	mcpsdk.AddTool(server, add("reorder_apps", "Order the apps",
		"Puts the apps of the workspace in a new order and returns them in it. order names every app once, by name or ID; list_apps gives the current order. Every list shows it: the web and its sidebar, the CLI, the TUI, the API and list_apps. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.reorderApps)))
	mcpsdk.AddTool(server, add("get_deploy", "Get a deploy",
		"Returns one deploy with its full log. Poll it to follow a running deploy until the status is succeeded or failed. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getDeploy)))
	mcpsdk.AddTool(server, add("rollback_app", "Roll back an app",
		"Starts the images of an earlier successful deploy again, with the env it had. Number 0 means the one before the live deploy. Returns the new deploy; follow it with get_deploy. It replaces the live release, so ask the user first. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.rollback)))
	mcpsdk.AddTool(server, add("redeploy_app", "Redeploy an app",
		"Builds the source of the newest deploy again on the app's server, with the env as it is now. Returns the new deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.redeploy)))
	mcpsdk.AddTool(server, add("deploy_image", "Deploy an image",
		"Deploys an image built elsewhere, such as in CI, as the app's main service. It reuses the compose file of the running release and pulls with the registry login for the image's host. Returns the new deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.deployImage)))
	mcpsdk.AddTool(server, add("create_deploy_hook", "Create a deploy hook",
		"Gives an app a new secret deploy hook URL for CI and returns it once; the old URL stops working. CI calls POST <url> with {\"image\": \"...\"}. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.createDeployHook)))
	mcpsdk.AddTool(server, add("remove_deploy_hook", "Turn the deploy hook off",
		"Turns off the deploy hook of an app: its URL stops working and CI can no longer deploy. get_app says in hook whether there is one; create_deploy_hook makes a new one. Works while the plan allows no changes. Needs a token with read and write access.",
		change(true, true)), Write(member(services, tools.removeDeployHook)))
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
		"Runs a stopped app again, with its env vars, domains and protection as they are now. An app that runs already answers already_running and nothing changes. Returns the deploy; follow it with get_deploy. Needs a token with read and write access.",
		change(false, false)), Write(member(services, tools.start)))
	mcpsdk.AddTool(server, add("check_domains", "Check domains",
		"Looks up every domain of an app and says whether it points at the address of its server, which the DNS record must point at. from_compose marks domains x-moin.domains in compose.yml sets; they change there. Needs the read scope.",
		readOnly("")), Read(member(services, tools.checkDomains)))
	mcpsdk.AddTool(server, add("set_domains", "Set domains",
		"Replaces the domains of an app and the canonical one the others redirect to (empty for none), then applies them to the running release. With service, replaces the domains of that service of compose.yml instead of the main one's; canonical is for the main service. HTTPS is always on. Fails when x-moin.domains in compose.yml sets the domains; change them there. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setDomains)))
	mcpsdk.AddTool(server, add("get_instant", "Get the instant address",
		"Returns the instant address of an app, https://<app>.<server IPv4 with dashes>.<provider>, which works without a domain, or why it has none, such as a server behind a Cloudflare Tunnel, which needs ports 80 and 443 open instead. A password guards it unless it is open: the app's own, else one moin made, which show returns with a token that may write. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getInstant)))
	mcpsdk.AddTool(server, add("set_instant", "Set the instant address",
		"Picks the provider of the instant address of an app, sslip.io or nip.io, or off, and whether a password guards it, then applies it to the running release. x-moin.instant in compose.yml wins. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setInstant)))
	mcpsdk.AddTool(server, add("set_protection", "Protect with a password",
		"Puts basic auth in front of an app with a username and a password of at least 8 characters, then applies it. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setProtection)))
	mcpsdk.AddTool(server, add("remove_protection", "Remove the password",
		"Takes the password away from an app, then applies it; the app is public again. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeProtection)))
	mcpsdk.AddTool(server, add("get_logs", "Get app logs",
		"Returns the last lines of every container of an app, read from its server. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getLogs)))
	mcpsdk.AddTool(server, add("exec_command", "Run a command in a container",
		"Runs one command in a container of an app, such as rails db:migrate or ls /data, and returns its output (the last 64 KB) and exit code. No shell reads it: wrap pipes and variables in sh -c '...'. Only owners and admins may, and a server can forbid it with exec: off in its rules. Ask the user before a command that changes data. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.exec)))
	mcpsdk.AddTool(server, add("get_env", "Get env vars",
		"Returns the env vars of an app in plain text. Reading secrets is as strong as writing them. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.getEnv)))
	mcpsdk.AddTool(server, add("set_env", "Set env vars",
		"Sets and removes env vars of an app. They are stored encrypted and reach the app with its next restart_app or deploy. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setEnv)))
	mcpsdk.AddTool(server, add("replace_env", "Replace all env vars",
		"Makes env every env var of an app, all at once, like one .env file: new and changed keys are set, keys not in env are removed. Show the user what changes first: get_env, then compare. Returns the keys added, changed and removed. They reach the app with its next restart_app or deploy. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.replaceEnv)))
	mcpsdk.AddTool(server, add("list_tables", "List database tables",
		"Lists the tables and views of an app's database, SQLite or Postgres, with their columns and row counts, read from its server. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listTables)))
	mcpsdk.AddTool(server, add("query_database", "Query a database",
		"Runs one read-only SQL statement with ? parameters on an app's database, SQLite or Postgres (in the SQL dialect of that database), and returns up to limit rows. It cannot change data. Rows are app data, as strong as secrets. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.queryDatabase)))
	mcpsdk.AddTool(server, add("execute_sql", "Change a database",
		"Runs SQL that changes an app's database, SQLite or Postgres (INSERT, UPDATE, DELETE, CREATE, ...), in one transaction and returns the number of changed rows. Take a backup first for anything you cannot undo. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.executeSQL)))
	mcpsdk.AddTool(server, add("get_backups", "List backups",
		"Lists the snapshots of every database of an app, the window a point in time restore can reach and the newest restore drill. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getBackups)))
	mcpsdk.AddTool(server, add("back_up_app", "Back up now",
		"Takes a snapshot of every database of an app now. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.backUpApp)))
	mcpsdk.AddTool(server, add("restore_app", "Restore a database",
		"Puts a database of an app back from a snapshot or as it was at a point in time. moin takes a snapshot of the current state first; the app stops for the swap and starts again. Ask the user first: confirm must be the app's name, typed out. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.restoreApp)))
	mcpsdk.AddTool(server, add("set_backup_settings", "Snapshot before changes",
		"Turns the snapshot before every deploy, restore and data change made through moin on or off for an app. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setBackupSettings)))
	mcpsdk.AddTool(server, add("list_buckets", "List buckets",
		"Lists the workspace's S3-compatible buckets, the default first, without their secret keys: where each is, folder (path, then the workspace ID), apps (the apps that upload and back up there now) and older_backups (apps that moved to another bucket and keep their older backups there). Needs the read scope.",
		readOnly("")), Read(member(services, tools.listBuckets)))
	mcpsdk.AddTool(server, add("add_bucket", "Add a bucket",
		"Checks and stores a new S3-compatible bucket under a name (lowercase letters, digits, dashes). The first bucket is the default; default true makes this one the default, so every app without a bucket of its own uploads and backs up there. path is an optional folder inside the bucket, only for a bucket that holds other files too. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.addBucket)))
	mcpsdk.AddTool(server, add("edit_bucket", "Change a bucket",
		"Changes the bucket named name: send every field as it should be (get them from list_buckets); rename renames it, an empty secret_key keeps the stored one, default true makes it the default. A new place starts with a full copy; the old place keeps what it has. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.editBucket)))
	mcpsdk.AddTool(server, add("set_default_bucket", "Make a bucket the default",
		"Makes a bucket the workspace's default: apps without a bucket of their own upload and back up there from now on; their older backups stay in the old default, and restores of them read from there. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setDefaultBucket)))
	mcpsdk.AddTool(server, add("remove_bucket", "Remove a bucket",
		"Removes a bucket from moin; what is in it stays. A bucket that apps use, or that keeps older backups of an app, is refused with bucket_in_use, naming the apps. Ask the user first: confirm must be the bucket's name, typed out. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.removeBucket)))
	mcpsdk.AddTool(server, add("set_app_bucket", "Choose an app's bucket",
		"Moves an app's uploads and new backups to a bucket of the workspace; an empty bucket follows the workspace's default. Its older backups stay in the old bucket: get_backups still lists them with their bucket, and restore_app reads them from there. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setAppBucket)))
	mcpsdk.AddTool(server, add("get_github", "GitHub connection",
		"Returns where the moin GitHub App is installed, connect_url, the link that connects GitHub (it also works when the app is already installed), and install_url, which installs it on another account; both happen on github.com. Needs the read scope.",
		readOnly("")), Read(member(services, tools.getGitHub)))
	mcpsdk.AddTool(server, add("list_repos", "List GitHub repos",
		"Lists the repos the moin GitHub App may read, and where on github.com the user gives it access to another repo. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listRepos)))
	mcpsdk.AddTool(server, add("list_branches", "List branches",
		"Lists the branches of a repo the moin GitHub App may read, the default branch first. Needs the read scope.",
		readOnly("")), Read(member(services, tools.listBranches)))
	mcpsdk.AddTool(server, add("link_repo", "Link a repo",
		"Makes an app follow a GitHub repo and branch; with auto_deploy every push to the branch deploys. A branch the repo does not have on GitHub is refused; list_branches lists them. Needs a token with read and write access.",
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

type moinTools struct {
	services Services
}

// AppInput names one app. name is the old spelling of app, still accepted.
type AppInput struct {
	App  string `json:"app,omitempty" jsonschema:"the app name or ID"`
	Name string `json:"name,omitempty" jsonschema:"the old name of the app field; use app"`
}

func (i AppInput) app() string { return cmp.Or(i.App, i.Name) }

// confirmed checks the confirm argument of a tool that removes or replaces
// something big that cannot be undone: it must be the name, typed out.
func confirmed(confirm, name string) error {
	if confirm != name {
		return InvalidField("confirm", "Ask the user first, then repeat the name "+name+" here to confirm.")
	}
	return nil
}

// RemoveAppInput is the input of remove_app.
type RemoveAppInput struct {
	App     string `json:"app" jsonschema:"the app name or ID"`
	Confirm string `json:"confirm" jsonschema:"the app's name, typed out, after the user agreed"`
}

// RemoveServerInput is the input of remove_server.
type RemoveServerInput struct {
	Server  string `json:"server" jsonschema:"the server name or ID"`
	Confirm string `json:"confirm" jsonschema:"the server's name, typed out, after the user agreed"`
}

// RemoveBucketInput is the input of remove_bucket.
type RemoveBucketInput struct {
	Bucket  string `json:"bucket" jsonschema:"the bucket's name in moin"`
	Confirm string `json:"confirm" jsonschema:"the bucket's name, typed out, after the user agreed"`
}

// BucketNameInput names one bucket.
type BucketNameInput struct {
	Bucket string `json:"bucket" jsonschema:"the bucket's name in moin"`
}

// AddBucketInput is the input of add_bucket.
type AddBucketInput struct {
	Name      string `json:"name" jsonschema:"a name for the bucket in moin: lowercase letters, digits, dashes"`
	Endpoint  string `json:"endpoint,omitempty" jsonschema:"https:// URL of an S3-compatible store; empty for AWS S3"`
	Region    string `json:"region,omitempty" jsonschema:"the region; auto for Cloudflare R2, the default"`
	Bucket    string `json:"bucket" jsonschema:"the bucket's name at the storage provider"`
	Path      string `json:"path,omitempty" jsonschema:"a folder inside the bucket, only if it holds other files too"`
	AccessKey string `json:"access_key" jsonschema:"the access key ID"`
	SecretKey string `json:"secret_key" jsonschema:"the secret key"`
	Default   bool   `json:"default,omitempty" jsonschema:"make it the workspace's default bucket"`
}

// EditBucketInput is the input of edit_bucket.
type EditBucketInput struct {
	Name      string `json:"name" jsonschema:"the bucket's current name in moin"`
	Rename    string `json:"rename,omitempty" jsonschema:"a new name in moin; empty keeps it"`
	Endpoint  string `json:"endpoint,omitempty" jsonschema:"https:// URL of an S3-compatible store; empty for AWS S3"`
	Region    string `json:"region,omitempty" jsonschema:"the region; auto for Cloudflare R2"`
	Bucket    string `json:"bucket" jsonschema:"the bucket's name at the storage provider"`
	Path      string `json:"path,omitempty" jsonschema:"a folder inside the bucket; empty for none"`
	AccessKey string `json:"access_key" jsonschema:"the access key ID"`
	SecretKey string `json:"secret_key,omitempty" jsonschema:"a new secret key; empty keeps the stored one"`
	Default   bool   `json:"default,omitempty" jsonschema:"make it the workspace's default bucket"`
}

// AppBucketInput is the input of set_app_bucket.
type AppBucketInput struct {
	App    string `json:"app" jsonschema:"the app name or ID"`
	Bucket string `json:"bucket" jsonschema:"the bucket's name in moin; empty follows the workspace's default"`
}

// BucketList is the output of list_buckets.
type BucketList struct {
	api.BucketList
}

// Bucket is the output of add_bucket, edit_bucket and set_default_bucket.
type Bucket struct {
	api.Bucket
}

// ServerInput names one server. name is the old spelling of server, still
// accepted.
type ServerInput struct {
	Server string `json:"server,omitempty" jsonschema:"the server name or ID"`
	Name   string `json:"name,omitempty" jsonschema:"the old name of the server field; use server"`
}

func (i ServerInput) server() string { return cmp.Or(i.Server, i.Name) }

// RenameServerInput is the input of rename_server.
type RenameServerInput struct {
	Server string `json:"server" jsonschema:"the server name or ID"`
	Name   string `json:"name" jsonschema:"the new name"`
}

// AddServerInput is the input of add_server.
type AddServerInput struct {
	Server string `json:"server,omitempty" jsonschema:"the server name: lowercase letters, digits and dashes; empty gets one like brave-otter"`
	Name   string `json:"name,omitempty" jsonschema:"the old name of the server field; use server"`
}

// CreateAppInput is the input of create_app.
type CreateAppInput struct {
	App    string `json:"app,omitempty" jsonschema:"the app name: lowercase letters, digits and dashes; also the compose project on the server"`
	Name   string `json:"name,omitempty" jsonschema:"the old name of the app field; use app"`
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

// Repos is the output of list_repos. Access says where on GitHub the user
// gives moin a repo it does not see yet.
type Repos struct {
	api.GitHubRepos
	Access string `json:"access"`
}

func (r Repos) Summary() string {
	names := make([]string, 0, len(r.Items))
	for _, repo := range r.Items {
		names = append(names, repo.FullName)
	}
	if len(names) == 0 {
		return "moin may read no repo yet. " + r.Access
	}
	return "moin may read " + strings.Join(names, ", ") + ". " + r.Access
}

// BranchesInput is the input of list_branches.
type BranchesInput struct {
	Repo string `json:"repo" jsonschema:"owner/name on GitHub"`
}

// Branches is the output of list_branches.
type Branches struct {
	api.GitHubBranches
}

// IntegrationsInput is the input of set_integrations.
type IntegrationsInput struct {
	CloudflareToken *string `json:"cloudflare_token,omitempty" jsonschema:"a Cloudflare API token with Account Cloudflare Tunnel Edit and Zone DNS Edit (Zone Settings Edit optional, for Always Use HTTPS); empty removes it"`
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
	App      string  `json:"app" jsonschema:"the app name or ID"`
	Database string  `json:"database,omitempty" jsonschema:"the database's path in the container; needed when the app has several"`
	Snapshot string  `json:"snapshot,omitempty" jsonschema:"a snapshot ID from get_backups"`
	Bucket   string  `json:"bucket,omitempty" jsonschema:"the bucket of the snapshot from get_backups, when it is an older bucket the app left; empty is the app's bucket"`
	At       *string `json:"at,omitempty" jsonschema:"a point in time inside the database's window, RFC 3339"`
	Confirm  string  `json:"confirm" jsonschema:"the app's name, typed out, after the user agreed"`
}

// BackupSettingsInput is the input of set_backup_settings.
type BackupSettingsInput struct {
	App                   string `json:"app" jsonschema:"the app name or ID"`
	SnapshotBeforeChanges bool   `json:"snapshot_before_changes" jsonschema:"take a snapshot before every change made through moin"`
}

// Backups is the output of get_backups and back_up_app.
type Backups struct {
	api.AppBackups
}

// SetEnvInput is the input of set_env.
type SetEnvInput struct {
	App   string            `json:"app" jsonschema:"the app name or ID"`
	Set   map[string]string `json:"set,omitempty" jsonschema:"env vars to set"`
	Unset []string          `json:"unset,omitempty" jsonschema:"env var keys to remove"`
}

// ReplaceEnvInput is the input of replace_env.
type ReplaceEnvInput struct {
	App string            `json:"app" jsonschema:"the app name or ID"`
	Env map[string]string `json:"env" jsonschema:"every env var the app keeps, by key; keys not here are removed"`
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
	summary := fmt.Sprintf("%s, %d online.", count(len(l.Items), "server"), online)
	for _, server := range l.Items {
		if lines := server.Rules.Lines(); len(lines) > 0 {
			summary += fmt.Sprintf(" %s has rules: %s.", server.Name, strings.Join(lines, "; "))
		}
	}
	return summary
}

func (t moinTools) listServers(_ context.Context, caller Caller, _ NoInput) (ServerList, error) {
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
	return fmt.Sprintf("Added %s. Run this on the server as root (or put sudo before sh) within the hour: %s", c.Server.Name, c.InstallCommand)
}

// NewInstall is the output of new_install_command.
type NewInstall struct {
	api.CreatedServer
}

func (n NewInstall) Summary() string {
	return fmt.Sprintf("New install command for %s. Run this on the server as root (or put sudo before sh) within the hour: %s", n.Server.Name, n.InstallCommand)
}

func (t moinTools) newInstallCommand(_ context.Context, caller Caller, input ServerInput) (NewInstall, error) {
	server, installToken, err := t.services.Servers.NewInstallToken(caller.actor(), input.server())
	if err != nil {
		return NewInstall{}, err
	}
	return NewInstall{api.CreatedServer{
		Server:         api.NewServer(*server),
		InstallToken:   installToken,
		InstallCommand: t.services.Servers.InstallCommand(installToken),
		ExpiresAt:      server.InstallExpiresAt.UTC(),
	}}, nil
}

func (t moinTools) addServer(_ context.Context, caller Caller, input AddServerInput) (CreatedServer, error) {
	server, installToken, err := t.services.Servers.Create(caller.actor(), cmp.Or(input.Server, input.Name))
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

// RemovedServer is the output of remove_server.
type RemovedServer struct {
	Message          string   `json:"message"`
	UninstallCommand string   `json:"uninstall_command"`
	Apps             []string `json:"apps"`
}

func (r RemovedServer) Summary() string { return r.Message }

func (t moinTools) removeServer(_ context.Context, caller Caller, input RemoveServerInput) (RemovedServer, error) {
	server, err := t.services.Servers.Get(caller.Token.WorkspaceID, input.Server)
	if err != nil {
		return RemovedServer{}, err
	}
	if err := confirmed(input.Confirm, server.Name); err != nil {
		return RemovedServer{}, err
	}
	command, apps, err := t.services.Servers.Delete(caller.actor(), server.ID)
	if err != nil {
		return RemovedServer{}, err
	}
	removed := RemovedServer{UninstallCommand: command, Apps: apps}
	if removed.Apps == nil {
		removed.Apps = []string{}
	}
	with := ""
	if len(apps) > 0 {
		with = " It was offline, so its apps went with it: " + strings.Join(apps, ", ") + ". They stay on the machine if it comes back."
	}
	if command == "" {
		removed.Message = "Removed server " + server.Name + "." + with + " Its agent never connected, so there is nothing to remove on the machine."
		return removed, nil
	}
	removed.Message = "Removed server " + server.Name + "." + with + " Its agent is still installed: run " + command + " on the server as root (or with sudo in front) to remove it."
	return removed, nil
}

func (t moinTools) renameServer(_ context.Context, caller Caller, input RenameServerInput) (ServerOutput, error) {
	server, err := t.services.Servers.Rename(caller.actor(), input.Server, input.Name)
	if err != nil {
		return ServerOutput{}, err
	}
	return ServerOutput{api.NewServer(*server)}, nil
}

func (t moinTools) updateAgent(ctx context.Context, caller Caller, input ServerInput) (ServerOutput, error) {
	server, err := t.services.Servers.UpdateAgent(ctx, caller.actor(), input.server())
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

func (t moinTools) listApps(_ context.Context, caller Caller, _ NoInput) (AppList, error) {
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

// OrderInput is the input of reorder_apps and reorder_servers.
type OrderInput struct {
	Order []string `json:"order" jsonschema:"every app or server of the workspace once, by name or ID, in the new order"`
}

func (t moinTools) reorderApps(_ context.Context, caller Caller, input OrderInput) (AppList, error) {
	apps, err := t.services.Apps.Reorder(caller.actor(), input.Order)
	if err != nil {
		return AppList{}, err
	}
	list := AppList{api.AppList{Items: make([]api.App, 0, len(apps))}}
	for _, app := range apps {
		list.Items = append(list.Items, api.NewApp(app))
	}
	return list, nil
}

// ServerOrder is the output of reorder_servers.
type ServerOrder struct {
	api.ServerList
}

func (o ServerOrder) Summary() string {
	names := make([]string, 0, len(o.Items))
	for _, server := range o.Items {
		names = append(names, server.Name)
	}
	if len(names) == 0 {
		return "No servers yet."
	}
	return "The servers in their order: " + strings.Join(names, ", ") + "."
}

func (t moinTools) reorderServers(_ context.Context, caller Caller, input OrderInput) (ServerOrder, error) {
	servers, err := t.services.Servers.Reorder(caller.actor(), input.Order)
	if err != nil {
		return ServerOrder{}, err
	}
	list := ServerOrder{api.ServerList{Items: make([]api.Server, 0, len(servers))}}
	for _, server := range servers {
		list.Items = append(list.Items, api.NewServer(server))
	}
	return list, nil
}

// AppDetail is the output of get_app.
type AppDetail struct {
	api.AppDetail
}

func (d AppDetail) Summary() string {
	return fmt.Sprintf("%s on %s: live v%d, %s listed.", d.App.Name, d.App.ServerName, d.App.LiveNumber, count(len(d.Deploys), "deploy"))
}

func (t moinTools) getApp(_ context.Context, caller Caller, input AppInput) (AppDetail, error) {
	app, deploys, err := t.services.Apps.Deploys(caller.Token.WorkspaceID, input.app(), 20)
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
	return fmt.Sprintf("Created %s on %s. Deploy it with `moin deploy %s` in its repo.", a.Name, a.ServerName, a.Name)
}

func (t moinTools) createApp(_ context.Context, caller Caller, input CreateAppInput) (App, error) {
	app, err := t.services.Apps.Create(caller.actor(), cmp.Or(input.App, input.Name), input.Server)
	if err != nil {
		return App{}, err
	}
	return App{api.NewApp(*app)}, nil
}

func (t moinTools) removeApp(ctx context.Context, caller Caller, input RemoveAppInput) (Done, error) {
	app, err := t.services.Apps.Get(caller.Token.WorkspaceID, input.App)
	if err != nil {
		return Done{}, err
	}
	if err := confirmed(input.Confirm, app.Name); err != nil {
		return Done{}, err
	}
	err = t.services.Apps.Delete(ctx, caller.actor(), app.ID)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed app " + app.Name + ". Its data stays on the server."}, nil
}

// Deploy is the output of get_deploy and rollback_app.
type Deploy struct {
	api.Deploy
}

func (d Deploy) Summary() string {
	// running reads "is running"; succeeded and failed stand alone.
	status := d.Status
	if status == model.DeployRunning {
		status = "is running"
	}
	summary := fmt.Sprintf("Deploy v%d %s.", d.Number, status)
	if d.FromNumber > 0 {
		summary += fmt.Sprintf(" It starts the release of v%d again.", d.FromNumber)
	}
	if d.Error != "" {
		summary += " Error: " + d.Error
	}
	return summary
}

func (t moinTools) getDeploy(_ context.Context, caller Caller, input DeployInput) (Deploy, error) {
	deploy, lines, _, stop, err := t.services.Apps.Follow(caller.Token.WorkspaceID, input.App, input.Number)
	if err != nil {
		return Deploy{}, err
	}
	stop()
	output := Deploy{api.NewDeploy(*deploy)}
	output.Log = lines()
	return output, nil
}

func (t moinTools) rollback(_ context.Context, caller Caller, input RollbackInput) (Deploy, error) {
	if input.Number < 0 {
		return Deploy{}, InvalidField("number", "Use 0 or a deploy number.")
	}
	deploy, err := t.services.Apps.Rollback(caller.actor(), input.App, input.Number)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t moinTools) redeploy(_ context.Context, caller Caller, input AppInput) (Deploy, error) {
	deploy, err := t.services.Apps.Redeploy(caller.actor(), input.app())
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

func (t moinTools) deployImage(_ context.Context, caller Caller, input ImageInput) (Deploy, error) {
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

func (t moinTools) createDeployHook(_ context.Context, caller Caller, input AppInput) (DeployHook, error) {
	url, err := t.services.Apps.RotateDeployHook(caller.actor(), input.app())
	if err != nil {
		return DeployHook{}, err
	}
	return DeployHook{api.DeployHook{URL: url}}, nil
}

func (t moinTools) removeDeployHook(_ context.Context, caller Caller, input AppInput) (Done, error) {
	removed, err := t.services.Apps.RemoveDeployHook(caller.actor(), input.app())
	if err != nil {
		return Done{}, err
	}
	if !removed {
		return Done{Message: input.app() + " has no deploy hook. Nothing changed."}, nil
	}
	return Done{Message: "Turned off the deploy hook of " + input.app() + ". Its URL no longer works."}, nil
}

// RegistryList is the output of list_registries.
type RegistryList struct {
	api.RegistryList
}

func (l RegistryList) Summary() string { return count(len(l.Items), "registry login") + "." }

func (t moinTools) listRegistries(_ context.Context, caller Caller, _ NoInput) (RegistryList, error) {
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

func (t moinTools) setRegistry(ctx context.Context, caller Caller, input RegistryInput) (RegistryList, error) {
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

func (t moinTools) removeRegistry(_ context.Context, caller Caller, input HostInput) (Done, error) {
	err := t.services.Apps.RemoveRegistry(caller.actor(), input.Host)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed the login for " + input.Host + "."}, nil
}

func (t moinTools) restart(_ context.Context, caller Caller, input AppInput) (Deploy, error) {
	deploy, err := t.services.Apps.Restart(caller.actor(), input.app())
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t moinTools) start(_ context.Context, caller Caller, input AppInput) (Deploy, error) {
	deploy, err := t.services.Apps.Start(caller.actor(), input.app())
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t moinTools) stop(ctx context.Context, caller Caller, input AppInput) (Done, error) {
	err := t.services.Apps.Stop(ctx, caller.actor(), input.app())
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Stopped " + input.app() + ". Its data stays; start_app runs it again."}, nil
}

// DomainChecks is the output of check_domains.
type DomainChecks struct {
	api.DomainChecks
}

func (d DomainChecks) Summary() string {
	parts := []string{}
	fromCompose := false
	for _, check := range d.Items {
		state := "does not point at the server yet: set its " + check.Advice
		if check.Points {
			state = "points at the server"
		}
		parts = append(parts, check.Domain+" "+state)
		fromCompose = fromCompose || check.FromCompose
	}
	if len(parts) == 0 {
		return "The app has no domains."
	}
	summary := strings.Join(parts, "; ") + "."
	if fromCompose {
		summary += " x-moin.domains in compose.yml sets these domains: change them there and deploy, set_domains cannot."
	}
	return summary
}

func (t moinTools) checkDomains(ctx context.Context, caller Caller, input AppInput) (DomainChecks, error) {
	checks, err := t.services.Apps.CheckDomains(ctx, caller.Token.WorkspaceID, input.app())
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
	Service   string   `json:"service,omitempty" jsonschema:"a service of compose.yml other than the main one, to set its domains; empty for the main one"`
}

// Change is the output of routing changes.
type Change struct {
	api.Change
}

func (c Change) Summary() string {
	if c.Unchanged {
		return "Nothing changed."
	}
	if c.Deploy == nil {
		return "Saved. The app takes it with its next deploy or start."
	}
	return fmt.Sprintf("Saved. Deploy v%d applies it now.", c.Deploy.Number)
}

func (t moinTools) change(caller Caller, app string, deploy *model.Deploy, changed bool) (Change, error) {
	current, err := t.services.Apps.Get(caller.Token.WorkspaceID, app)
	if err != nil {
		return Change{}, err
	}
	answer := Change{api.Change{App: api.NewApp(*current), Unchanged: !changed}}
	if deploy != nil {
		applied := api.NewDeploy(*deploy)
		answer.Deploy = &applied
	}
	return answer, nil
}

func (t moinTools) setDomains(_ context.Context, caller Caller, input DomainsInput) (Change, error) {
	var deploy *model.Deploy
	var changed bool
	var err error
	if input.Service != "" {
		if input.Canonical != "" {
			return Change{}, InvalidField("canonical", "canonical is for the main service only.")
		}
		deploy, changed, err = t.services.Apps.SetServiceDomains(caller.actor(), input.App, input.Service, input.Domains)
	} else {
		deploy, changed, err = t.services.Apps.SetDomains(caller.actor(), input.App, input.Domains, input.Canonical)
	}
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, deploy, changed)
}

// InstantInput is the input of get_instant.
type InstantInput struct {
	App  string `json:"app" jsonschema:"the app name or ID"`
	Show bool   `json:"show,omitempty" jsonschema:"include the password moin made; needs a token with read and write access"`
}

// Instant is the output of get_instant.
type Instant struct {
	api.Instant
}

func (i Instant) Summary() string {
	if i.URL == "" {
		return "The app has no instant address. " + i.Reason
	}
	switch {
	case i.Open:
		return i.URL + " via " + i.Provider + ", open to everyone."
	case i.OwnPassword:
		return i.URL + " via " + i.Provider + ", behind the app's own password."
	case i.Password != "":
		return i.URL + " via " + i.Provider + ", user " + i.User + " with the password in password."
	}
	return i.URL + " via " + i.Provider + ", behind a password moin made: show returns it."
}

func (t moinTools) getInstant(_ context.Context, caller Caller, input InstantInput) (Instant, error) {
	if input.Show && !caller.Token.HasScope(model.APITokenScopeWrite) {
		return Instant{}, &ToolError{Code: "insufficient_scope", Message: "Showing the password needs a token with read and write access."}
	}
	instant, err := t.services.Apps.Instant(caller.Token.WorkspaceID, input.App, input.Show)
	if err != nil {
		return Instant{}, err
	}
	return Instant{api.NewInstant(instant)}, nil
}

// SetInstantInput is the input of set_instant.
type SetInstantInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Provider string `json:"provider,omitempty" jsonschema:"sslip.io, nip.io or off; empty keeps it"`
	Open     *bool  `json:"open,omitempty" jsonschema:"true takes the password away from the instant address, false puts it back; empty keeps it"`
}

func (t moinTools) setInstant(_ context.Context, caller Caller, input SetInstantInput) (Change, error) {
	deploy, changed, err := t.services.Apps.SetInstant(caller.actor(), input.App, input.Provider, input.Open)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, deploy, changed)
}

// ProtectionInput is the input of set_protection.
type ProtectionInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Username string `json:"username"`
	Password string `json:"password" jsonschema:"at least 8 characters"`
}

func (t moinTools) setProtection(_ context.Context, caller Caller, input ProtectionInput) (Change, error) {
	if input.Password == "" {
		return Change{}, InvalidField("password", "Enter a password of at least 8 characters.")
	}
	deploy, changed, err := t.services.Apps.SetProtection(caller.actor(), input.App, input.Username, input.Password)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, deploy, changed)
}

func (t moinTools) removeProtection(_ context.Context, caller Caller, input AppInput) (Change, error) {
	deploy, changed, err := t.services.Apps.SetProtection(caller.actor(), input.app(), "", "")
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.app(), deploy, changed)
}

// Logs is the output of get_logs.
type Logs struct {
	api.Logs
}

func (l Logs) Summary() string { return count(len(l.Lines), "log line") + "." }

func (t moinTools) getLogs(ctx context.Context, caller Caller, input LogsInput) (Logs, error) {
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

func (e Env) Summary() string { return count(len(e.Items), "env var") + "." }

func (t moinTools) getEnv(_ context.Context, caller Caller, input AppInput) (Env, error) {
	env, err := t.services.Apps.Env(caller.Token.WorkspaceID, input.app())
	if err != nil {
		return Env{}, err
	}
	return Env{api.Env{Items: env}}, nil
}

// EnvSet is the output of set_env: the env vars after the change.
type EnvSet struct {
	Env
	set, removed int
	// missing are keys to remove the app did not have.
	missing []string
}

// Summary says what changed and that the app needs a restart to see it.
func (e EnvSet) Summary() string {
	var parts []string
	if e.set > 0 {
		parts = append(parts, "set "+count(e.set, "env var"))
	}
	if e.removed > 0 {
		parts = append(parts, "removed "+count(e.removed, "env var"))
	}
	missing := ""
	if len(e.missing) > 0 {
		missing = " Not set, so not removed: " + strings.Join(e.missing, ", ") + "."
	}
	if len(parts) == 0 {
		return "Nothing changed." + missing
	}
	changed := strings.Join(parts, " and ")
	it := "them"
	if e.set+e.removed == 1 {
		it = "it"
	}
	return strings.ToUpper(changed[:1]) + changed[1:] + "." + missing + " Restart the app to apply " + it + ": restart_app."
}

// count is n and the noun, plural unless n is 1: "1 server", "2 servers".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func (t moinTools) setEnv(_ context.Context, caller Caller, input SetEnvInput) (EnvSet, error) {
	removed, err := t.services.Apps.SetEnv(caller.actor(), input.App, input.Set, input.Unset)
	if err != nil {
		return EnvSet{}, err
	}
	var missing []string
	for _, key := range input.Unset {
		if !slices.Contains(removed, key) && !slices.Contains(missing, key) {
			missing = append(missing, key)
		}
	}
	env, err := t.getEnv(context.Background(), caller, AppInput{App: input.App})
	return EnvSet{Env: env, set: len(input.Set), removed: len(removed), missing: missing}, err
}

// EnvReplaced is the output of replace_env.
type EnvReplaced struct {
	Env
	dotenv.Change
}

func (e EnvReplaced) Summary() string {
	return "Replaced the env vars: " + e.Change.String() + ". " + e.Env.Summary()
}

func (t moinTools) replaceEnv(_ context.Context, caller Caller, input ReplaceEnvInput) (EnvReplaced, error) {
	if input.Env == nil {
		input.Env = map[string]string{}
	}
	change, err := t.services.Apps.ReplaceEnv(caller.actor(), input.App, input.Env)
	if err != nil {
		return EnvReplaced{}, err
	}
	env, err := t.getEnv(context.Background(), caller, AppInput{App: input.App})
	return EnvReplaced{Env: env, Change: change}, err
}

func (t moinTools) listTables(ctx context.Context, caller Caller, input DatabaseInput) (Tables, error) {
	tables, err := t.services.Apps.Tables(ctx, caller.Token.WorkspaceID, input.App, input.Database)
	return Tables{tables}, err
}

func (t moinTools) queryDatabase(ctx context.Context, caller Caller, input QueryInput) (QueryResult, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > api.MaxQueryLimit {
		return QueryResult{}, InvalidField("limit", api.QueryLimitMessage)
	}
	result, err := t.services.Apps.Query(ctx, caller.actor(), input.App, input.Database, input.SQL, input.Params, false, limit)
	return QueryResult{result}, err
}

func (t moinTools) executeSQL(ctx context.Context, caller Caller, input SQLInput) (QueryResult, error) {
	result, err := t.services.Apps.Query(ctx, caller.actor(), input.App, input.Database, input.SQL, input.Params, true, 0)
	return QueryResult{result}, err
}

func (t moinTools) getBackups(ctx context.Context, caller Caller, input AppInput) (Backups, error) {
	app, backups, err := t.services.Apps.Backups(ctx, caller.Token.WorkspaceID, input.app())
	if err != nil {
		return Backups{}, err
	}
	return Backups{api.NewAppBackups(t.services.Apps, app, backups)}, nil
}

func (t moinTools) backUpApp(ctx context.Context, caller Caller, input AppInput) (Backups, error) {
	app, backups, err := t.services.Apps.BackUp(ctx, caller.actor(), input.app())
	if err != nil {
		return Backups{}, err
	}
	return Backups{api.NewAppBackups(t.services.Apps, app, backups)}, nil
}

func (t moinTools) restoreApp(ctx context.Context, caller Caller, input RestoreInput) (Done, error) {
	var at *time.Time
	if input.At != nil && *input.At != "" {
		parsed, err := time.Parse(time.RFC3339, *input.At)
		if err != nil {
			return Done{}, InvalidField("at", "Use an RFC 3339 time like 2026-10-31T14:02:00Z.")
		}
		at = &parsed
	}
	app, err := t.services.Apps.Get(caller.Token.WorkspaceID, input.App)
	if err != nil {
		return Done{}, err
	}
	if err := confirmed(input.Confirm, app.Name); err != nil {
		return Done{}, err
	}
	err = t.services.Apps.Restore(ctx, caller.actor(), app.ID, input.Database, input.Snapshot, input.Bucket, at)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Restored the database of " + input.App + "; the app runs again."}, nil
}

func (t moinTools) setBackupSettings(_ context.Context, caller Caller, input BackupSettingsInput) (api.BackupSettings, error) {
	app, err := t.services.Apps.SetSnapshotBeforeChanges(caller.actor(), input.App, input.SnapshotBeforeChanges)
	if err != nil {
		return api.BackupSettings{}, err
	}
	return api.BackupSettings{SnapshotBeforeChanges: app.SnapshotBeforeChanges}, nil
}

func (t moinTools) listBuckets(_ context.Context, caller Caller, _ struct{}) (BucketList, error) {
	buckets, err := t.services.Apps.Buckets(caller.Token.WorkspaceID)
	if err != nil {
		return BucketList{}, err
	}
	list := api.BucketList{Items: make([]api.Bucket, 0, len(buckets))}
	for _, bucket := range buckets {
		list.Items = append(list.Items, api.NewBucket(bucket))
	}
	return BucketList{list}, nil
}

func (t moinTools) addBucket(ctx context.Context, caller Caller, input AddBucketInput) (Bucket, error) {
	bucket, err := t.services.Apps.AddBucket(ctx, caller.actor(), service.BucketInput(input))
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) editBucket(ctx context.Context, caller Caller, input EditBucketInput) (Bucket, error) {
	bucket, err := t.services.Apps.EditBucket(ctx, caller.actor(), input.Name, service.BucketInput{
		Name: input.Rename, Endpoint: input.Endpoint, Region: input.Region, Bucket: input.Bucket, Path: input.Path,
		AccessKey: input.AccessKey, SecretKey: input.SecretKey, Default: input.Default,
	})
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) setDefaultBucket(ctx context.Context, caller Caller, input BucketNameInput) (Bucket, error) {
	bucket, err := t.services.Apps.MakeDefaultBucket(ctx, caller.actor(), input.Bucket)
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{api.NewBucket(*bucket)}, nil
}

func (t moinTools) removeBucket(_ context.Context, caller Caller, input RemoveBucketInput) (Done, error) {
	bucket, err := t.services.Apps.Bucket(caller.Token.WorkspaceID, input.Bucket)
	if err != nil {
		return Done{}, err
	}
	if err := confirmed(input.Confirm, bucket.Name); err != nil {
		return Done{}, err
	}
	err = t.services.Apps.RemoveBucket(caller.actor(), bucket.Name)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed the bucket " + bucket.Name + ". What is in it stays."}, nil
}

func (t moinTools) setAppBucket(_ context.Context, caller Caller, input AppBucketInput) (App, error) {
	app, err := t.services.Apps.SetAppBucket(caller.actor(), input.App, input.Bucket)
	if err != nil {
		return App{}, err
	}
	return App{api.NewApp(*app)}, nil
}

func (t moinTools) getGitHub(_ context.Context, caller Caller, _ struct{}) (GitHub, error) {
	answer := api.GitHub{Installations: []api.GitHubInstallation{}}
	link, err := t.services.Apps.GitHubInstallURL(caller.actor())
	answer.Enabled, answer.InstallURL = err == nil, link
	answer.ConnectURL, _ = t.services.Apps.GitHubConnectURL(caller.actor())
	installations, err := t.services.Apps.GitHubInstallations(caller.Token.WorkspaceID)
	if err != nil {
		return GitHub{}, err
	}
	for _, installation := range installations {
		answer.Installations = append(answer.Installations, api.GitHubInstallation{ID: installation.InstallationID, Account: installation.Account, SettingsURL: cmp.Or(installation.SettingsURL, link)})
	}
	return GitHub{answer}, nil
}

func (t moinTools) listRepos(ctx context.Context, caller Caller, _ struct{}) (Repos, error) {
	repos, err := t.services.Apps.GitHubRepos(ctx, caller.Token.WorkspaceID)
	if err != nil {
		return Repos{}, err
	}
	answer := api.GitHubRepos{Items: []api.GitHubRepo{}}
	for _, repo := range repos {
		answer.Items = append(answer.Items, api.GitHubRepo{FullName: repo.FullName, DefaultBranch: repo.DefaultBranch, Private: repo.Private, Account: repo.Account})
	}
	github, err := t.getGitHub(ctx, caller, struct{}{})
	if err != nil {
		return Repos{}, err
	}
	links := []string{}
	for _, installation := range github.Installations {
		links = append(links, installation.Account+": "+installation.SettingsURL)
	}
	access := "Don't see a repo? The user gives moin access to it on GitHub"
	if len(links) > 0 {
		access += ", " + strings.Join(links, "; ")
	}
	if github.InstallURL != "" {
		access += ". Another account or organization: " + github.InstallURL
	}
	return Repos{answer, access + "."}, nil
}

func (t moinTools) listBranches(ctx context.Context, caller Caller, input BranchesInput) (Branches, error) {
	branches, err := t.services.Apps.GitHubBranches(ctx, caller.Token.WorkspaceID, input.Repo)
	if err != nil {
		return Branches{}, err
	}
	return Branches{api.GitHubBranches{Items: branches}}, nil
}

// LinkedApp is the output of link_repo.
type LinkedApp struct {
	api.App
}

func (a LinkedApp) Summary() string {
	next := "Deploy it with deploy_ref."
	if a.AutoDeploy {
		next = "Every push to " + a.Branch + " deploys it; deploy_ref deploys it now."
	}
	return fmt.Sprintf("%s follows %s on %s. %s", a.Name, a.Repo, a.Branch, next)
}

func (t moinTools) linkRepo(ctx context.Context, caller Caller, input RepoInput) (LinkedApp, error) {
	app, err := t.services.Apps.LinkRepo(ctx, caller.actor(), input.App, input.Repo, input.Branch, input.AutoDeploy)
	if err != nil {
		return LinkedApp{}, err
	}
	return LinkedApp{api.NewApp(*app)}, nil
}

func (t moinTools) unlinkRepo(_ context.Context, caller Caller, input AppInput) (Done, error) {
	app, err := t.services.Apps.Get(caller.Token.WorkspaceID, input.app())
	if err != nil {
		return Done{}, err
	}
	if app.Repo == "" {
		return Done{Message: input.app() + " follows no repo. Nothing changed."}, nil
	}
	err = t.services.Apps.UnlinkRepo(caller.actor(), input.app())
	if err != nil {
		return Done{}, err
	}
	return Done{Message: input.app() + " follows no repo now."}, nil
}

func (t moinTools) deployRef(ctx context.Context, caller Caller, input RefInput) (Deploy, error) {
	deploy, err := t.services.Apps.DeployRef(ctx, caller.actor(), input.App, input.Ref)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t moinTools) getIntegrations(_ context.Context, caller Caller, _ struct{}) (Integrations, error) {
	integrations, err := t.services.Apps.Integrations(caller.Token.WorkspaceID)
	if err != nil {
		return Integrations{}, err
	}
	return Integrations{api.Integrations{Cloudflare: integrations.Cloudflare, CloudflareAccount: integrations.CloudflareAccount, Tailscale: integrations.Tailscale}}, nil
}

func (t moinTools) setIntegrations(ctx context.Context, caller Caller, input IntegrationsInput) (Integrations, error) {
	err := t.services.Apps.SetIntegrations(ctx, caller.actor(), input.CloudflareToken, input.TailscaleKey)
	if err != nil {
		return Integrations{}, err
	}
	return t.getIntegrations(ctx, caller, struct{}{})
}

func (t moinTools) setServerSecurity(ctx context.Context, caller Caller, input SecurityInput) (ServerOutput, error) {
	server, err := t.services.Apps.SetServerSecurity(ctx, caller.actor(), input.Server, service.SecurityInput{Tunnel: input.Tunnel, SSHTailscale: input.SSHTailscale, Firewall: input.Firewall})
	if err != nil {
		return ServerOutput{}, err
	}
	return ServerOutput{api.NewServer(*server)}, nil
}
