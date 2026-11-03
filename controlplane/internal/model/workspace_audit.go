package model

import (
	"encoding/json"
	"strings"
	"time"
)

// WorkspaceAuditEvent is one change in the activity of a workspace.
//
// What goes in: the exact time in UTC, who did it (the member, the API token
// by name, a deploy hook, a GitHub push or ops itself), how it came in, the
// client IP and user agent of the member's own request, what it touched and
// the settings before and after: names, on and off, domains, branches, roles,
// counts, versions, release numbers and error messages.
//
// What never goes in: env var values, secrets, tokens, passwords, SQL text,
// row contents or cell values, file contents and log lines of an app. A SQL
// write keeps the kind of statement, its tables and the row count; a row edit
// the table, row and column names.
type WorkspaceAuditEvent struct {
	ID          string  `db:"id"`
	WorkspaceID string  `db:"workspace_id"`
	ActorUserID *string `db:"actor_user_id"`
	// ActorEmail is the member's email now, or at the time of the event once
	// the account is gone.
	ActorEmail string `db:"actor_email"`
	Action     string `db:"action"`
	TargetType string `db:"target_type"`
	TargetID   string `db:"target_id"`
	Metadata   string `db:"metadata"`
	// Via is how the change came in: web, cli, tui, api, mcp, github, hook or
	// system. Events from before the CLI and TUI were told apart say api.
	Via string `db:"via"`
	// TokenName is the API token the change came with, by name.
	TokenName string `db:"token_name"`
	// IP and UserAgent are of the member's request; only admins see them.
	IP        string `db:"ip"`
	UserAgent string `db:"user_agent"`
	// App and Server are what the change touched, by name, for filters.
	App       string    `db:"app"`
	Server    string    `db:"server"`
	CreatedAt time.Time `db:"created_at"`
}

// ActivityChoices are the apps, servers and member emails the activity of a
// workspace can be filtered by.
type ActivityChoices struct {
	Apps    []string `json:"apps"`
	Servers []string `json:"servers"`
	Users   []string `json:"users"`
}

// ActivityFilter narrows the activity to one app, server or member. User is a
// user ID or an email. Empty fields match everything.
type ActivityFilter struct {
	App    string
	Server string
	User   string
}

func WorkspaceAuditActionLabel(action string) string {
	switch action {
	case "workspace.created":
		return "Created the workspace"
	case "workspace.updated":
		return "Updated workspace settings"
	case "workspace.invitation.sent":
		return "Sent an invitation"
	case "workspace.invitation.created":
		return "Created an invitation"
	case "workspace.invitation.revoked":
		return "Revoked an invitation"
	case "workspace.invitation.accepted":
		return "Accepted an invitation"
	case "workspace.member.role_changed":
		return "Changed a member role"
	case "workspace.member.removed":
		return "Removed a member"
	case "workspace.member.left":
		return "Left the workspace"
	case "billing.checkout_started":
		return "Started a billing checkout"
	case "billing.management_opened":
		return "Opened billing management"
	default:
		return action
	}
}

// Details are the metadata of an event as key and value.
func (e WorkspaceAuditEvent) Details() map[string]string {
	details := map[string]string{}
	_ = json.Unmarshal([]byte(e.Metadata), &details)
	return details
}

// Actor names who did it: the member's email, or GitHub, a deploy hook or ops
// itself for changes that came without a member.
func (e WorkspaceAuditEvent) Actor() string {
	switch {
	case e.ActorEmail != "":
		return e.ActorEmail
	case e.Via == "github":
		return "GitHub"
	case e.Via == "hook":
		return "A deploy hook"
	default:
		return "ops"
	}
}

// ViaLabel names how a change came in, with the token it came with:
// "CLI, token laptop".
func (e WorkspaceAuditEvent) ViaLabel() string {
	label := ""
	switch e.Via {
	case "web":
		label = "Web"
	case "cli":
		label = "CLI"
	case "tui":
		label = "TUI"
	case "api":
		label = "API"
	case "mcp":
		label = "MCP"
	case "hook":
		label = "Deploy hook"
	case "github":
		label = "GitHub"
	case "system":
		label = "ops"
	}
	if e.TokenName != "" {
		if label == "" {
			return "token " + e.TokenName
		}
		return label + ", token " + e.TokenName
	}
	return label
}

// Summary says what happened, to follow the actor: "deployed blog v12".
func (e WorkspaceAuditEvent) Summary() string {
	d := e.Details()
	target := e.TargetID
	version := ""
	if d["number"] != "" {
		version = " v" + d["number"]
	}
	source := ""
	switch {
	case d["ref"] != "" && d["commit"] != "":
		source = " from " + d["ref"] + " at " + d["commit"]
	case d["image"] != "":
		source = " from the image " + d["image"]
	}
	on := func(sentence string) string {
		if d["on"] == "false" {
			return strings.Replace(sentence, "%s", "off", 1)
		}
		return strings.Replace(sentence, "%s", "on", 1)
	}
	switch e.Action {
	case "app.created":
		return "created the app " + target + " on " + d["server"]
	case "app.deployed":
		return "deployed " + target + version + source
	case "app.redeployed":
		return "redeployed " + target + " as" + version
	case "app.rolled_back":
		return "rolled " + target + " back to v" + d["from"] + " as" + version
	case "app.restarted":
		return "restarted " + target + " with its current settings as" + version
	case "app.started":
		return "started " + target + " with its current settings as" + version
	case "app.stopped":
		return "stopped " + target
	case "app.domains_set":
		if d["domains"] == "" {
			return "removed the domains of " + target
		}
		if d["from"] != "" {
			return "set the domains of " + target + " to " + d["domains"] + ", from " + d["from"]
		}
		return "set the domains of " + target + " to " + d["domains"]
	case "app.protected":
		return "put a password in front of " + target
	case "app.unprotected":
		return "took the password away from " + target
	case "app.hook_rotated":
		return "made a new deploy hook for " + target
	case "app.hook_removed":
		return "turned off the deploy hook of " + target
	case "env.set":
		return "set " + d["keys"] + " in the env of " + target
	case "env.removed":
		return "removed " + d["keys"] + " from the env of " + target
	case "registry.set":
		return "saved the registry login for " + target
	case "registry.removed":
		return "removed the registry login for " + target
	case "bucket.set":
		return "connected the bucket " + target
	case "bucket.removed":
		return "removed the bucket"
	case "backup.taken":
		return "took a snapshot of " + target
	case "backup.restored":
		if d["snapshot"] != "" {
			return "restored " + target + " from the snapshot " + d["snapshot"]
		}
		return "restored " + target + " to " + d["at"]
	case "backup.snapshot_before_changes":
		return on("turned the snapshot before changes %s for " + target)
	case "data.sql":
		what := "SQL"
		if d["statements"] != "" {
			what = d["statements"]
		}
		switch d["changes"] {
		case "", "0":
			return "ran " + what + " in " + target
		case "1":
			return "ran " + what + " in " + target + ", 1 row changed"
		}
		return "ran " + what + " in " + target + ", " + d["changes"] + " rows changed"
	case "data.cell_updated":
		return "edited " + d["column"] + " of row " + d["row"] + " in " + d["table"] + " of " + target
	case "data.row_deleted":
		return "deleted row " + d["row"] + " of " + d["table"] + " in " + target
	case "data.row_inserted":
		return "added a row to " + d["table"] + " in " + target
	case "github.connected":
		return "connected the GitHub account " + target
	case "repo.linked":
		linked := "linked " + target + " to " + d["repo"] + " on " + d["branch"]
		switch d["auto_deploy"] {
		case "true":
			return linked + ", every push deploys"
		case "false":
			return linked + ", deploys only by hand"
		}
		return linked
	case "repo.unlinked":
		return "unlinked " + target + " from " + d["repo"]
	case "app.deleted":
		if d["server"] != "" {
			return "deleted the app " + target + " on " + d["server"]
		}
		return "deleted the app " + target
	case "integration.cloudflare":
		return on("turned the Cloudflare token %s")
	case "integration.tailscale":
		return on("turned the Tailscale key %s")
	case "server.added":
		return "added the server " + target
	case "server.removed":
		return "removed the server " + target
	case "server.install_command":
		return "made a new install command for " + target
	case "server.renamed":
		return "renamed the server " + d["from"] + " to " + target
	case "server.agent_updated":
		return "updated the agent of " + target + " from " + d["from"] + " to " + d["to"]
	case "server.agent_update_failed":
		return "could not update the agent of " + target + " from " + d["from"] + " to " + d["to"] + ", so agents of this workspace no longer update by themselves until one is updated by hand: " + d["error"]
	case "workspace.agent_updates":
		return on("turned automatic agent updates %s")
	case "controlplane.update_started":
		return "started the update of the control plane from " + d["from"] + " to " + d["to"]
	case "controlplane.updated":
		return "updated the control plane from " + d["from"] + " to " + d["to"]
	case "controlplane.rolled_back":
		return "could not update the control plane to " + d["to"] + ", so it runs " + d["from"] + " again: " + d["reason"]
	case "controlplane.update_failed":
		return "could not update the control plane to " + d["to"] + ": " + d["reason"]
	case "server.security_set":
		return "changed how " + target + " is reached: tunnel " + d["tunnel"] + ", SSH on Tailscale " + d["ssh_tailscale"] + ", firewall " + d["firewall"]
	case "server.reinstalled":
		return "installed the agent of " + target + " again, and the old agent's token stopped working: " + strings.Join(nonEmpty(d["hostname"], d["os"]+"/"+d["arch"], d["version"]), ", ")
	case "server.installed":
		return "connected " + target + " for the first time: " + strings.Join(nonEmpty(d["hostname"], d["os"]+"/"+d["arch"], d["version"]), ", ")
	case "deploy.succeeded":
		return "made" + version + " of " + target + " live after " + d["duration"]
	case "deploy.failed":
		return "could not make" + version + " of " + target + " live, after " + d["duration"]
	case "backup.failed":
		return "could not back up " + d["database"] + " of " + target
	case "backup.recovered":
		return "backs up " + d["database"] + " of " + target + " again"
	case "backup.drill_passed":
		return "restored " + d["database"] + " of " + target + " in a restore drill and checked it"
	case "github.disconnected":
		return "uninstalled the ops GitHub App from " + target
	case "api_token.created":
		return "created the API token " + d["name"] + " with " + strings.ReplaceAll(d["scopes"], ",", " and ") + " access, expiring " + d["expires"]
	case "api_token.revoked":
		return "revoked the API token " + d["name"]
	case "billing.plan_changed":
		return "changed the plan from " + d["from"] + " (" + d["status_from"] + ") to " + d["to"] + " (" + d["status"] + ")"
	case "workspace.updated":
		if d["from"] != "" {
			return "renamed the workspace from " + d["from"] + " to " + d["name"]
		}
		return "renamed the workspace to " + d["name"]
	case "workspace.invitation.created":
		return "invited " + d["email"] + " as " + d["role"]
	case "workspace.invitation.sent":
		return "sent the invitation to " + d["email"]
	case "workspace.invitation.revoked":
		if d["email"] != "" {
			return "revoked the invitation of " + d["email"]
		}
		return "revoked an invitation"
	case "workspace.invitation.accepted":
		if d["role"] != "" {
			return "joined the workspace as " + d["role"]
		}
		return "joined the workspace"
	case "workspace.member.role_changed":
		if d["email"] != "" {
			return "changed the role of " + d["email"] + " from " + d["from"] + " to " + d["to"]
		}
		return "changed a role from " + d["from"] + " to " + d["to"]
	case "workspace.member.removed":
		if d["email"] != "" {
			return "removed " + d["email"] + " from the workspace"
		}
		return "removed a member"
	case "workspace.member.account_deleted":
		return "deleted their account and left the workspace"
	default:
		label := WorkspaceAuditActionLabel(e.Action)
		if label == e.Action {
			return e.Action
		}
		return strings.ToLower(label[:1]) + label[1:]
	}
}

// nonEmpty drops the empty values and the ones that hold only a slash.
func nonEmpty(values ...string) []string {
	kept := []string{}
	for _, value := range values {
		if value != "" && value != "/" {
			kept = append(kept, value)
		}
	}
	return kept
}
