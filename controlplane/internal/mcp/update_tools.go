package mcp

import (
	"context"

	"github.com/axadrn/moin/controlplane/internal/api"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// VersionOutput is the output of get_version and update_control_plane.
type VersionOutput struct {
	api.VersionInfo
	// AutoUpdateAgents is whether the agents of the token's workspace
	// update to the control plane's version by themselves.
	AutoUpdateAgents bool `json:"auto_update_agents"`
}

// AgentUpdatesInput turns automatic agent updates on or off.
type AgentUpdatesInput struct {
	Auto bool `json:"auto" jsonschema:"true updates every agent older than the control plane by itself"`
}

// AgentUpdatesOutput is the output of set_agent_updates.
type AgentUpdatesOutput struct {
	api.AgentUpdates
}

// registerUpdateTools registers the tools for the version of the control
// plane and the updates of it and of the agents.
func registerUpdateTools(server *mcpsdk.Server, services Services) {
	tools := opsTools{services: services}
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:  "get_version",
		Title: "Get the version",
		Description: "Returns the control plane's version, the one agents update to, and whether the agents of the workspace update by themselves (auto_update_agents). " +
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
		Name:  "set_agent_updates",
		Title: "Turn automatic agent updates on or off",
		Description: "With auto true, every agent of the workspace older than the control plane updates by itself when it connects, one server at a time, never during a deploy and never on a server with rules. On by default. " +
			"update_agent updates one agent by hand. Needs a token with read and write access.",
		Annotations: change(false, true),
	}, Write(member(services, tools.setAgentUpdates)))
}

func (t opsTools) getVersion(_ context.Context, caller Caller, _ NoInput) (VersionOutput, error) {
	auto, err := t.services.Servers.AutoUpdateAgents(caller.Token.WorkspaceID)
	if err != nil {
		return VersionOutput{}, err
	}
	return VersionOutput{VersionInfo: api.NewVersion(t.services.Updates.Info(caller.User)), AutoUpdateAgents: auto}, nil
}

func (t opsTools) updateControlPlane(_ context.Context, caller Caller, _ NoInput) (VersionOutput, error) {
	info, err := t.services.Updates.Start(caller.actor(), caller.User)
	if err != nil {
		return VersionOutput{}, err
	}
	auto, err := t.services.Servers.AutoUpdateAgents(caller.Token.WorkspaceID)
	if err != nil {
		return VersionOutput{}, err
	}
	return VersionOutput{VersionInfo: api.NewVersion(info), AutoUpdateAgents: auto}, nil
}

func (t opsTools) setAgentUpdates(_ context.Context, caller Caller, input AgentUpdatesInput) (AgentUpdatesOutput, error) {
	if err := t.services.Servers.SetAutoUpdateAgents(caller.actor(), input.Auto); err != nil {
		return AgentUpdatesOutput{}, err
	}
	return AgentUpdatesOutput{api.AgentUpdates{Auto: input.Auto}}, nil
}
