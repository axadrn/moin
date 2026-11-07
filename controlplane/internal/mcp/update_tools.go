package mcp

import (
	"context"

	"github.com/axadrn/moin/controlplane/internal/api"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// VersionOutput is the output of get_version and update_control_plane.
type VersionOutput struct {
	api.VersionInfo
	// AutoUpdateDaemons is whether the daemons of the token's workspace
	// update to the control plane's version by themselves.
	AutoUpdateDaemons bool `json:"auto_update_daemons"`
}

// DaemonUpdatesInput turns automatic daemon updates on or off.
type DaemonUpdatesInput struct {
	Auto bool `json:"auto" jsonschema:"true updates every daemon older than the control plane by itself"`
}

// DaemonUpdatesOutput is the output of set_daemon_updates.
type DaemonUpdatesOutput struct {
	api.DaemonUpdates
}

// registerUpdateTools registers the tools for the version of the control
// plane and the updates of it and of the daemons.
func registerUpdateTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "get_version",
		Title: "Get the version",
		Description: "Returns the control plane's version, the one daemons update to, and whether the daemons of the workspace update by themselves (auto_update_daemons). " +
			"A self-hosted control plane (self_hosted) also returns the newest release (latest, update_available, summary, notes, notes_url) and the update under way or of the past day (update.state: requested, updating, done, rolled_back or failed, with a reason). " +
			"can_update is true for an admin of a self-hosted control plane when update_control_plane would work. The hosted control plane updates itself with every release. Needs the read scope.",
		Annotations: readOnly("Get the version"),
	}, Read(member(services, tools.getVersion)))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "update_control_plane",
		Title: "Update the control plane",
		Description: "Updates a self-hosted control plane to the newest release, for an admin of it (can_update in get_version). The updater next to it pulls the new version and restarts it; " +
			"if the new version does not come up within two minutes, the old one starts again. The control plane is away for a minute meanwhile: poll get_version until update.state is done, rolled_back or failed. Ask the user first. Needs a token with read and write access.",
		Annotations: &mcpsdk.ToolAnnotations{Title: "Update the control plane", DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(true)},
	}, Write(member(services, tools.updateControlPlane)))
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "set_daemon_updates",
		Title: "Turn automatic daemon updates on or off",
		Description: "With auto true, every daemon of the workspace older than the control plane updates by itself when it connects, one server at a time, never during a deploy and never on a server with rules. On by default. " +
			"update_daemon updates one daemon by hand. Needs a token with read and write access.",
		Annotations: change(false, true),
	}, Write(member(services, tools.setDaemonUpdates)))
}

func (t moinTools) getVersion(_ context.Context, caller Caller, _ NoInput) (VersionOutput, error) {
	auto, err := t.services.Servers.AutoUpdateDaemons(caller.Token.WorkspaceID)
	if err != nil {
		return VersionOutput{}, err
	}
	return VersionOutput{VersionInfo: api.NewVersion(t.services.Updates.Info(caller.User)), AutoUpdateDaemons: auto}, nil
}

func (t moinTools) updateControlPlane(_ context.Context, caller Caller, _ NoInput) (VersionOutput, error) {
	info, err := t.services.Updates.Start(caller.actor(), caller.User)
	if err != nil {
		return VersionOutput{}, err
	}
	auto, err := t.services.Servers.AutoUpdateDaemons(caller.Token.WorkspaceID)
	if err != nil {
		return VersionOutput{}, err
	}
	return VersionOutput{VersionInfo: api.NewVersion(info), AutoUpdateDaemons: auto}, nil
}

func (t moinTools) setDaemonUpdates(_ context.Context, caller Caller, input DaemonUpdatesInput) (DaemonUpdatesOutput, error) {
	if err := t.services.Servers.SetAutoUpdateDaemons(caller.actor(), input.Auto); err != nil {
		return DaemonUpdatesOutput{}, err
	}
	return DaemonUpdatesOutput{api.DaemonUpdates{Auto: input.Auto}}, nil
}
