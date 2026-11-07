package mcp

import (
	"context"
	"slices"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/internal/dotenv"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerEnvTools registers the tools of an app's env vars.
func registerEnvTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, newTool("get_env", "Get env vars",
		"Returns the env vars of an app in plain text. Reading secrets is as strong as writing them. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.getEnv)))
	mcpsdk.AddTool(server, newTool("set_env", "Set env vars",
		"Sets and removes env vars of an app. They are stored encrypted and reach the app with its next restart_app or deploy. Needs a token with read and write access.",
		change(false, true)), Write(changes(services, tools.setEnv)))
	mcpsdk.AddTool(server, newTool("replace_env", "Replace all env vars",
		"Makes env every env var of an app, all at once, like one .env file: new and changed keys are set, keys not in env are removed. Show the user what changes first: get_env, then compare. Returns the keys added, changed and removed. They reach the app with its next restart_app or deploy. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.replaceEnv)))
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
