package mcp

import (
	"context"
	"strconv"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/protocol"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerExecTools registers exec_command.
func registerExecTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, newTool("exec_command", "Run a command in a container",
		"Runs one command in a container of an app, such as rails db:migrate or ls /data, and returns its output (the last 64 KB) and exit code. No shell reads it: wrap pipes and variables in sh -c '...'. Only owners and admins may, and a server can forbid it with exec: off in its rules. Ask the user before a command that changes data. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.exec)))
}

// execOutputLimit is how much of a command's output exec_command returns:
// enough to read, small enough for a daemon's context.
const execOutputLimit = 64 * 1024

// ExecInput is the input of exec_command.
type ExecInput struct {
	App     string `json:"app" jsonschema:"the app name or ID"`
	Command string `json:"command" jsonschema:"the command, like rails db:migrate; split into words like a shell quotes them, with no variables, globs or pipes: wrap those in sh -c '...'"`
	Service string `json:"service,omitempty" jsonschema:"the compose service to run it in, default the app's main service"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"seconds before the daemon stops waiting, default 600, at most 3600"`
}

// ExecOutput is the output of exec_command.
type ExecOutput struct {
	service.ExecResult
	Output string `json:"output"`
	// Cut says the output passed what this tool returns; the end is kept.
	Cut bool `json:"cut,omitempty"`
}

func (o ExecOutput) Summary() string {
	if o.TimedOut {
		return "Timed out in " + o.Service + "."
	}
	return "Exited with " + strconv.Itoa(o.ExitCode) + " in " + o.Service + "."
}

// execMember is the caller's membership in the token's workspace: exec is
// for owners and admins, and the service decides by the role.
func execMember(services Services, caller Caller) (*model.WorkspaceMember, error) {
	memberships, err := services.Workspaces.ListForUser(caller.User.ID)
	if err != nil {
		return nil, err
	}
	for _, membership := range memberships {
		if membership.ID == caller.Token.WorkspaceID {
			return &model.WorkspaceMember{WorkspaceID: membership.ID, UserID: caller.User.ID, Role: membership.Role}, nil
		}
	}
	return nil, repository.ErrWorkspaceNotFound
}

func (t moinTools) exec(ctx context.Context, caller Caller, input ExecInput) (ExecOutput, error) {
	argv, err := protocol.SplitCommand(input.Command)
	if err != nil {
		return ExecOutput{}, InvalidField("command", "The command: "+err.Error()+".")
	}
	member, err := execMember(t.services, caller)
	if err != nil {
		return ExecOutput{}, err
	}
	var output strings.Builder
	result, err := t.services.Apps.Exec(ctx, member, caller.Source, input.App, service.ExecInput{Service: input.Service, Argv: argv, Timeout: input.Timeout}, func(piece string) {
		output.WriteString(piece)
	})
	if err != nil {
		return ExecOutput{}, err
	}
	text, cut := output.String(), false
	if len(text) > execOutputLimit {
		text, cut = text[len(text)-execOutputLimit:], true
	}
	return ExecOutput{ExecResult: result, Output: text, Cut: cut}, nil
}
