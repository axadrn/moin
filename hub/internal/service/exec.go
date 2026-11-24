package service

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// ExecInput is one command to run in a container of an app. Service is
// the compose service, the app's main one when empty. Timeout is in
// seconds, protocol.ExecTimeout when zero.
type ExecInput struct {
	Service string
	Argv    []string
	Timeout int
}

// ExecResult is how a run ended. Output is only what the caller kept.
type ExecResult struct {
	Service   string `json:"service"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
}

// ErrNoCommand is a run without a command.
var ErrNoCommand = errors.New("give a command to run")

// ExecServices are the services a command can run in, as far as moin knows
// them from the live deploy: the main one first, then the databases.
func (s *AppService) ExecServices(workspaceID, idOrName string) ([]string, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.execServices(app), nil
}

func (s *AppService) execServices(app *model.App) []string {
	if app.LiveNumber == 0 {
		return nil
	}
	deploy, err := s.apps.Deploy(app.ID, app.LiveNumber)
	if err != nil {
		return nil
	}
	detected := Detected(*deploy)
	if detected == nil || detected.Service == "" {
		return nil
	}
	services := []string{detected.Service}
	for _, database := range detected.Databases {
		if database.Service != "" && !slices.Contains(services, database.Service) {
			services = append(services, database.Service)
		}
	}
	return services
}

// Exec runs a command in a container of an app through its server's daemon,
// and hands what it writes to output as it comes. Only owners and admins
// may: a command can read and change everything the app can. The activity
// records the command as the daemon masked it, and how it ended; never its
// output. A command that did not run, refused or failed, keeps only why.
func (s *AppService) Exec(ctx context.Context, member *model.WorkspaceMember, source Source, idOrName string, input ExecInput, output func(string)) (ExecResult, error) {
	actor, err := manager(member, source)
	if err != nil {
		return ExecResult{}, err
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return ExecResult{}, err
	}
	if len(input.Argv) == 0 || input.Argv[0] == "" {
		return ExecResult{}, ErrNoCommand
	}
	service := input.Service
	if service == "" {
		services := s.execServices(app)
		if len(services) == 0 {
			return ExecResult{}, ErrNotLive
		}
		service = services[0]
	}
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = protocol.ExecTimeout
	}
	timeout = min(timeout, protocol.ExecMaxTimeout)
	// A little longer than the daemon's own timeout, so its answer comes first.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second+30*time.Second)
	defer cancel()
	result := ExecResult{Service: service, ExitCode: -1}
	// Only the daemon knows the env values, so only it masks the command.
	command := ""
	start := protocol.ExecStart{App: app.Folder, Service: service, Argv: input.Argv, Timeout: timeout}
	err = s.servers.Stream(ctx, app.ServerID, protocol.TypeExecStart, start, func(message protocol.Message) (bool, error) {
		switch message.Type {
		case protocol.TypeExecOutput:
			var piece protocol.ExecOutput
			if message.Decode(&piece) == nil && output != nil {
				output(piece.Data)
			}
			return false, nil
		case protocol.TypeExecDone:
			var done protocol.ExecDone
			if err := message.Decode(&done); err != nil {
				return true, err
			}
			if done.Error != "" {
				return true, &DaemonError{Message: done.Error}
			}
			result.ExitCode, result.Truncated, result.TimedOut, command = done.ExitCode, done.Truncated, done.TimedOut, done.Command
			return true, nil
		}
		return false, nil
	})
	details := map[string]string{"service": service}
	if command != "" {
		details["command"] = clipText(command, 300)
	}
	switch {
	case err != nil:
		details["error"] = clipText(err.Error(), 200)
	case result.TimedOut:
		details["result"] = "timed out after " + strconv.Itoa(timeout) + "s"
	default:
		details["exit_code"] = strconv.Itoa(result.ExitCode)
	}
	s.servers.record(actor, "app.exec", "app", app.Name, details)
	return result, err
}
