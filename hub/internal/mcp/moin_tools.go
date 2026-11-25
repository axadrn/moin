package mcp

import (
	"context"
	"strconv"

	"github.com/axadrn/moin/hub/internal/repository"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerOpsTools registers a tool for every server and app action of the
// web UI, the CLI and the TUI, one register function per topic. Removing a
// server or an app is destructive: clients ask before they call it.
func registerOpsTools(server *mcpsdk.Server, services Services) {
	registerServerTools(server, services)
	registerAppTools(server, services)
	registerDeployTools(server, services)
	registerDomainTools(server, services)
	registerEnvTools(server, services)
	registerDataTools(server, services)
	registerBackupTools(server, services)
	registerBucketTools(server, services)
	registerGitHubTools(server, services)
	registerExecTools(server, services)
}

// newTool describes a moin tool; its title is also the annotations' title.
func newTool(name, title, description string, annotations *mcpsdk.ToolAnnotations) *mcpsdk.Tool {
	annotations.Title = title
	return &mcpsdk.Tool{Name: name, Title: title, Description: description, Annotations: annotations}
}

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
			if membership.ID == caller.Bearer.WorkspaceID {
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
		if err := services.Servers.RequireChanges(caller.Bearer.WorkspaceID); err != nil {
			return empty, err
		}
		return tool(ctx, caller, input)
	})
}

// change annotates a tool that changes data.
func change(destructive, idempotent bool) *mcpsdk.ToolAnnotations {
	return &mcpsdk.ToolAnnotations{DestructiveHint: boolPointer(destructive), IdempotentHint: idempotent, OpenWorldHint: boolPointer(false)}
}

type moinTools struct {
	services Services
}

// AppInput names one app.
type AppInput struct {
	App string `json:"app" jsonschema:"the app name or ID"`
}

// Done is the output of tools that answer nothing but success.
type Done struct {
	Message string `json:"message"`
}

func (d Done) Summary() string { return d.Message }

// count is n and the noun, plural unless n is 1: "1 server", "2 servers".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
