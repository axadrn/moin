package mcp

import (
	"cmp"
	"context"
	"fmt"
	"github.com/axadrn/moin/hub/internal/model"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerAppTools registers the tools of apps: listing, creating and
// removing them, starting and stopping them, their logs and their compose
// file.
func registerAppTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	Add(server, newTool("list_apps", "List apps",
		"Lists the user's apps with their server, newest deploy and live deploy.",
		readOnly("")), model.RightRead, member(services, tools.listApps))
	Add(server, newTool("get_app", "Get an app",
		"Returns an app with its 20 newest deploys: status, errors, and what the daemon detected (compose, Dockerfile, Go, or Railpack with the language and start command it found; service; port; databases, SQLite and Postgres).",
		readOnly("")), model.RightRead, member(services, tools.getApp))
	Add(server, newTool("create_app", "Create an app",
		"Creates an app on a server. Deploys come from its GitHub repo (link_repo, or `moin deploy` in the pushed repo, which can also create the app), an image (deploy_image) or the catalog. A new app starts with its password on: its domains and its instant address ask for user moin and the password in password, the one time moin shows it, so give it to the user to keep. remove_password turns it off.",
		change(false, false)), model.RightDeploy, changes(services, tools.createApp))
	Add(server, newTool("remove_app", "Remove an app",
		"Stops an app on its server and removes its containers and images. Its volumes and data stay on the server. Ask the user first: confirm must be the app's name, typed out.",
		change(true, true)), model.RightDelete, changes(services, tools.removeApp))
	Add(server, newTool("rename_app", "Rename an app",
		"Gives an app a new name, 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit, unique in the workspace, and returns it. Links name the app by its name: links inside moin follow, an old bookmark finds nothing. Its data, backups and instant address stay. The next restart or deploy gives its containers the new name.",
		change(false, true)), model.RightDeploy, changes(services, tools.renameApp))
	Add(server, newTool("pin_app", "Pin or unpin an app",
		"Pins an app, or unpins it with pinned false, and returns it. Every list of the workspace shows pinned apps above the others that run fine, on every surface.",
		change(false, true)), model.RightDeploy, changes(services, tools.pinApp))
	Add(server, newTool("restart_app", "Restart an app",
		"Starts the live deploy of an app again with its env vars, domains and password as they are now, without a build. Use it after set_env. A stopped app runs again. Returns the deploy; follow it with get_deploy.",
		change(false, false)), model.RightDeploy, changes(services, tools.restart))
	Add(server, newTool("stop_app", "Stop an app",
		"Stops the containers of an app. It stays offline, also after a reboot of the server, until start_app, restart_app or a deploy. Its data and settings stay.",
		change(true, true)), model.RightDeploy, member(services, tools.stop))
	Add(server, newTool("start_app", "Start an app",
		"Runs a stopped app again, with its env vars, domains and password as they are now. An app that runs already answers already_running and nothing changes. Returns the deploy; follow it with get_deploy.",
		change(false, false)), model.RightDeploy, member(services, tools.start))
	Add(server, newTool("get_logs", "Get app logs",
		"Returns the last lines of every container of an app, read from its server.",
		readOnly("")), model.RightRead, member(services, tools.getLogs))
	Add(server, newTool("get_compose_file", "Get the compose file",
		"Returns the compose file of the live deploy of an app, YAML: everything moin runs for it, the proxy with its domains and HTTPS too, written to run anywhere, without the instant address. It holds no secret value. image, ports, expose, volumes, user, the env PORT and the paths of a build show the values the deploy ran with; every other ${NAME} stays as written. Saved with a .env of the app's values next to it, docker compose up -d runs the app elsewhere. A live deploy without one, as deploys before v1.0.0-beta.35, answers no_compose_file: redeploy_app or restart_app makes one, and get_deploy's log says why if it stays empty.",
		readOnly("")), model.RightRead, member(services, tools.getComposeFile))
}

// ComposeFileInput is the input of get_compose_file.
type ComposeFileInput struct {
	App string `json:"app" jsonschema:"the app name or ID"`
}

// ComposeFile is the output of get_compose_file.
type ComposeFile struct {
	api.ComposeFile
}

func (f ComposeFile) Summary() string {
	return fmt.Sprintf("The compose file of v%d:\n\n%s", f.Number, f.Text)
}

func (t moinTools) getComposeFile(_ context.Context, caller Caller, input ComposeFileInput) (ComposeFile, error) {
	number, text, err := t.services.Apps.ComposeFile(caller.Bearer.WorkspaceID, input.App)
	if err != nil {
		return ComposeFile{}, err
	}
	return ComposeFile{api.ComposeFile{Number: number, Text: text}}, nil
}

// RemoveAppInput is the input of remove_app.
type RemoveAppInput struct {
	App     string `json:"app" jsonschema:"the app name or ID"`
	Confirm string `json:"confirm" jsonschema:"the app's name, typed out, after the user agreed"`
}

// CreateAppInput is the input of create_app.
type CreateAppInput struct {
	App    string `json:"app" jsonschema:"the app name: lowercase letters, digits and dashes; also the compose project on the server"`
	Server string `json:"server" jsonschema:"the server name or ID"`
}

// LogsInput is the input of get_logs.
type LogsInput struct {
	App  string `json:"app" jsonschema:"the app name or ID"`
	Tail int    `json:"tail,omitempty" jsonschema:"lines per container, 1 to 1000, default 200"`
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
	apps, err := t.services.Apps.ListSorted(caller.Bearer.WorkspaceID)
	if err != nil {
		return AppList{}, err
	}
	list := AppList{api.AppList{Items: make([]api.App, 0, len(apps))}}
	for _, app := range apps {
		list.Items = append(list.Items, api.NewApp(app))
	}
	return list, nil
}

// PinInput is the input of pin_app.
type PinInput struct {
	App    string `json:"app" jsonschema:"the app's name or ID"`
	Pinned bool   `json:"pinned" jsonschema:"true pins the app, false unpins it"`
}

// PinnedApp is the output of pin_app.
type PinnedApp struct {
	api.App
}

func (a PinnedApp) Summary() string {
	if a.Pinned {
		return "Pinned " + a.Name + ". Lists show it above the apps that run fine."
	}
	return "Unpinned " + a.Name + "."
}

func (t moinTools) pinApp(_ context.Context, caller Caller, input PinInput) (PinnedApp, error) {
	app, err := t.services.Apps.SetPinned(caller.actor(), input.App, input.Pinned)
	if err != nil {
		return PinnedApp{}, err
	}
	return PinnedApp{api.NewApp(*app)}, nil
}

// RenameAppInput is the input of rename_app.
type RenameAppInput struct {
	App  string `json:"app" jsonschema:"the app name or ID"`
	Name string `json:"name" jsonschema:"the new name"`
}

// RenamedApp is the output of rename_app.
type RenamedApp struct {
	api.App
}

func (a RenamedApp) Summary() string {
	return fmt.Sprintf("Renamed the app to %s. Links inside moin follow; restart it to give its containers the name.", a.Name)
}

func (t moinTools) renameApp(_ context.Context, caller Caller, input RenameAppInput) (RenamedApp, error) {
	app, err := t.services.Apps.Rename(caller.actor(), input.App, input.Name)
	if err != nil {
		return RenamedApp{}, err
	}
	return RenamedApp{api.NewApp(*app)}, nil
}

// AppDetail is the output of get_app.
type AppDetail struct {
	api.AppDetail
}

func (d AppDetail) Summary() string {
	return fmt.Sprintf("%s on %s: live v%d, %s listed.", d.App.Name, d.App.ServerName, d.App.LiveNumber, count(len(d.Deploys), "deploy"))
}

func (t moinTools) getApp(_ context.Context, caller Caller, input AppInput) (AppDetail, error) {
	app, deploys, err := t.services.Apps.Deploys(caller.Bearer.WorkspaceID, input.App, 20)
	if err != nil {
		return AppDetail{}, err
	}
	return AppDetail{api.AppDetail{App: api.NewApp(*app), Deploys: api.NewDeploys(deploys, app.LiveNumber)}}, nil
}

// App is the output of set_app_bucket.
type App struct {
	api.App
}

func (a App) Summary() string {
	return fmt.Sprintf("%s backs up to %s.", a.Name, cmp.Or(a.Bucket, "the workspace's default bucket"))
}

// CreatedApp is the output of create_app.
type CreatedApp struct {
	api.CreatedApp
}

func (a CreatedApp) Summary() string {
	return fmt.Sprintf("Created %s on %s. Its domains and its instant address ask for user %s and the password in password: shown this once. Link its GitHub repo, or run `moin deploy %s` in the pushed repo.", a.App.Name, a.App.ServerName, service.PasswordUser, a.App.Name)
}

func (t moinTools) createApp(_ context.Context, caller Caller, input CreateAppInput) (CreatedApp, error) {
	app, err := t.services.Apps.Create(caller.actor(), input.App, input.Server)
	if err != nil {
		return CreatedApp{}, err
	}
	return CreatedApp{api.CreatedApp{App: api.NewApp(*app), Password: app.NewPassword}}, nil
}

func (t moinTools) removeApp(ctx context.Context, caller Caller, input RemoveAppInput) (Done, error) {
	app, err := t.services.Apps.Get(caller.Bearer.WorkspaceID, input.App)
	if err != nil {
		return Done{}, err
	}
	err = t.services.Apps.Delete(ctx, caller.actor(), app.ID, input.Confirm)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Removed app " + app.Name + ". Its data stays on the server."}, nil
}

func (t moinTools) restart(_ context.Context, caller Caller, input AppInput) (Deploy, error) {
	deploy, err := t.services.Apps.Restart(caller.actor(), input.App)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t moinTools) start(_ context.Context, caller Caller, input AppInput) (Deploy, error) {
	deploy, err := t.services.Apps.Start(caller.actor(), input.App)
	if err != nil {
		return Deploy{}, err
	}
	return Deploy{api.NewDeploy(*deploy)}, nil
}

func (t moinTools) stop(ctx context.Context, caller Caller, input AppInput) (Done, error) {
	err := t.services.Apps.Stop(ctx, caller.actor(), input.App)
	if err != nil {
		return Done{}, err
	}
	return Done{Message: "Stopped " + input.App + ". Its data stays; start_app runs it again."}, nil
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
	lines, err := t.services.Apps.Logs(ctx, caller.Bearer.WorkspaceID, input.App, tail)
	if err != nil {
		return Logs{}, err
	}
	return Logs{api.Logs{Lines: lines}}, nil
}
