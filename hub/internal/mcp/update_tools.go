package mcp

import (
	"context"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// VersionOutput is the output of get_version.
type VersionOutput struct {
	api.VersionInfo
	// AutoUpdateDaemons is whether the daemons of the token's workspace
	// update to the hub's version by themselves.
	AutoUpdateDaemons bool `json:"auto_update_daemons"`
}

// DaemonUpdatesInput turns automatic daemon updates on or off.
type DaemonUpdatesInput struct {
	Auto bool `json:"auto" jsonschema:"true updates every daemon older than the hub by itself"`
}

// DaemonUpdatesOutput is the output of set_daemon_updates.
type DaemonUpdatesOutput struct {
	api.DaemonUpdates
}

// registerUpdateTools registers the tools for the version of the hub
// and the updates of the daemons.
func registerUpdateTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	Add(server, &mcpsdk.Tool{
		Name:  "get_version",
		Title: "Get the version",
		Description: "Returns the hub's version, the one daemons update to, and whether the daemons of the workspace update by themselves (auto_update_daemons). " +
			"A self-hosted hub (self_hosted) also returns the newest release (latest, update_available, summary, notes, notes_url) and read_first. " +
			"Its admins update it on its server with `sh /opt/moin/update.sh`; show the user read_first and the whole notes first, since some updates need a step from them. The hosted hub updates itself with every release. " +
			"An admin of the hub, hosted or self-hosted, also gets hub_backup: how the backup of the hub's own database stands (state on, off or late when no copy reached the bucket for two minutes, the bucket and last_copy); null for everyone else.",
		Annotations: readOnly("Get the version"),
	}, model.RightRead, member(services, tools.getVersion))
	Add(server, &mcpsdk.Tool{
		Name:  "set_daemon_updates",
		Title: "Turn automatic daemon updates on or off",
		Description: "With auto true, every daemon of the workspace older than the hub updates by itself when it connects, one server at a time, never during a deploy. On by default. " +
			"update_daemon updates one daemon by hand.",
		Annotations: change(false, true),
	}, model.RightServers, member(services, tools.setDaemonUpdates))
}

func (t moinTools) getVersion(_ context.Context, caller Caller, _ NoInput) (VersionOutput, error) {
	auto, err := t.services.Servers.AutoUpdateDaemons(caller.Bearer.WorkspaceID)
	if err != nil {
		return VersionOutput{}, err
	}
	return VersionOutput{VersionInfo: api.NewVersion(t.services.Updates.Info(caller.User)), AutoUpdateDaemons: auto}, nil
}

func (t moinTools) setDaemonUpdates(_ context.Context, caller Caller, input DaemonUpdatesInput) (DaemonUpdatesOutput, error) {
	if err := t.services.Servers.SetAutoUpdateDaemons(caller.actor(), input.Auto); err != nil {
		return DaemonUpdatesOutput{}, err
	}
	return DaemonUpdatesOutput{api.DaemonUpdates{Auto: input.Auto}}, nil
}
