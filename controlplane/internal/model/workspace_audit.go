package model

import (
	"encoding/json"
	"strings"
	"time"
)

type WorkspaceAuditEvent struct {
	ID          string    `db:"id"`
	WorkspaceID string    `db:"workspace_id"`
	ActorUserID *string   `db:"actor_user_id"`
	ActorEmail  string    `db:"actor_email"`
	Action      string    `db:"action"`
	TargetType  string    `db:"target_type"`
	TargetID    string    `db:"target_id"`
	Metadata    string    `db:"metadata"`
	CreatedAt   time.Time `db:"created_at"`
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

// Via is how the change came in: web, api, mcp, hook or github. Older and
// workspace events have none.
func (e WorkspaceAuditEvent) Via() string { return e.Details()["via"] }

// Actor names who did it: the member's email, or GitHub and CI for changes
// that came without a member.
func (e WorkspaceAuditEvent) Actor() string {
	switch {
	case e.ActorEmail != "":
		return e.ActorEmail
	case e.Via() == "github":
		return "GitHub"
	case e.Via() == "hook":
		return "A deploy hook"
	default:
		return "ops"
	}
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
	case "app.deleted":
		return "deleted the app " + target
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
		return "set the domains of " + target + " to " + d["domains"]
	case "app.protected":
		return "put a password in front of " + target
	case "app.unprotected":
		return "took the password away from " + target
	case "app.hook_rotated":
		return "made a new deploy hook for " + target
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
		return "linked " + target + " to " + d["repo"] + " on " + d["branch"]
	case "repo.unlinked":
		return "unlinked " + target + " from " + d["repo"]
	case "integration.cloudflare":
		return on("turned the Cloudflare token %s")
	case "integration.tailscale":
		return on("turned the Tailscale key %s")
	case "server.added":
		return "added the server " + target
	case "server.removed":
		return "removed the server " + target
	case "server.agent_updated":
		return "updated the agent of " + target + " from " + d["from"] + " to " + d["to"]
	case "server.security_set":
		return "changed how " + target + " is reached: tunnel " + d["tunnel"] + ", SSH on Tailscale " + d["ssh_tailscale"] + ", firewall " + d["firewall"]
	default:
		label := WorkspaceAuditActionLabel(e.Action)
		if label == e.Action {
			return e.Action
		}
		return strings.ToLower(label[:1]) + label[1:]
	}
}
