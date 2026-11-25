package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/protocol"
)

func apiClient() (*client.Client, error) {
	config, err := client.LoadConfig()
	if err != nil {
		return nil, err
	}
	return client.New(config)
}

// appsCall is one run of a subcommand of apps.
type appsCall struct {
	command string
	args    []string
	stdin   io.Reader
	stdout  io.Writer
	api     *client.Client
	apiErr  error
}

// parse reads the flags, then checks the login: help works without one.
func (call appsCall) parse(flags *flagSet) error {
	err := flags.parse(call.args)
	if err != nil {
		return err
	}
	return call.apiErr
}

// appsCommands are the subcommands of apps. Any other first argument is an
// app to show.
var appsCommands = map[string]func(context.Context, appsCall) error{
	"":       appsList,
	"add":    appsAdd,
	"pin":    appsPin,
	"unpin":  appsPin,
	"rename": appsRename,
	"rm":     appsRemove,
}

func apps(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	api, apiErr := apiClient()
	call := appsCall{command: command, args: args, stdin: stdin, stdout: stdout, api: api, apiErr: apiErr}
	subcommand, known := appsCommands[command]
	if known {
		return subcommand(ctx, call)
	}
	return appsShow(ctx, call)
}

func appsList(ctx context.Context, call appsCall) error {
	flags := newFlags("apps", "apps")
	err := call.parse(flags)
	if err != nil {
		return err
	}
	list, err := call.api.Apps(ctx)
	if err != nil {
		return err
	}
	PrintApps(call.stdout, list)
	return nil
}

func appsAdd(ctx context.Context, call appsCall) error {
	flags := newFlags("apps add", "apps add --server <server> <name>")
	server := flags.String("server", "", "server name or ID")
	err := call.parse(flags)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 || *server == "" {
		return flags.wrong()
	}
	app, err := call.api.AddApp(ctx, flags.Arg(0), *server)
	if err != nil {
		return err
	}
	done(call.stdout, "Added %s on %s", style.Bold.Render(app.Name), app.ServerName)
	printInstantPassword(call.stdout, app.InstantPassword)
	hint(call.stdout, "Deploy it with", build.Name+" deploy "+app.Name)
	return nil
}

// appsPin runs both pin and unpin.
func appsPin(ctx context.Context, call appsCall) error {
	pinned := call.command == "pin"
	flags := newFlags("apps "+call.command, "apps "+call.command+" [app]").withApp()
	err := call.parse(flags)
	if err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	app, err := call.api.SetPinned(ctx, name, pinned)
	if err != nil {
		return err
	}
	if app.Pinned {
		done(call.stdout, "Pinned %s. Lists show it above the apps that run fine", style.Bold.Render(app.Name))
	} else {
		done(call.stdout, "Unpinned %s", style.Bold.Render(app.Name))
	}
	return nil
}

func appsRename(ctx context.Context, call appsCall) error {
	flags := newFlags("apps rename", "apps rename <app> <new name>")
	err := call.parse(flags)
	if err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	renamed, err := call.api.RenameApp(ctx, flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	done(call.stdout, "Renamed %s to %s. Its links, data and backups stay", flags.Arg(0), style.Bold.Render(renamed.Name))
	hint(call.stdout, "Its containers take the name on the next restart:", build.Name+" restart "+renamed.Name)
	return nil
}

func appsRemove(ctx context.Context, call appsCall) error {
	flags := newFlags("apps rm", "apps rm <app> [--confirm <app>]").withApp().withConfirm()
	err := call.parse(flags)
	if err != nil {
		return err
	}
	if flags.NArg() > 1 || (flags.NArg() == 0 && *flags.appFlag == "") {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	question := fmt.Sprintf("Remove app %s? Its containers and images go; its data stays on the server.", name)
	typed, err := typeName(call.stdin, call.stdout, flags.confirmed(), question, name)
	if err != nil {
		return err
	}
	err = call.api.RemoveApp(ctx, name, typed)
	if err != nil {
		return err
	}
	done(call.stdout, "Removed %s. Its data stays on the server", style.Bold.Render(name))
	return nil
}

// appsShow shows one app: apps my-app, the way people try it after apps.
func appsShow(ctx context.Context, call appsCall) error {
	if len(call.args) > 0 {
		return fmt.Errorf("unknown command %q. Run `%s help`", "apps "+call.command, build.Name)
	}
	if call.apiErr != nil {
		return call.apiErr
	}
	detail, err := call.api.App(ctx, call.command)
	if client.IsNotFound(err) {
		return fmt.Errorf("no app or command %q. Run `%s apps` for your apps, `%s help` for the commands", call.command, build.Name, build.Name)
	}
	if err != nil {
		return err
	}
	printApp(call.stdout, detail, time.Now())
	return nil
}

// printApp writes one app: its state, server, domains, repo and deploys.
func printApp(stdout io.Writer, detail client.AppDetail, now time.Time) {
	app := detail.App
	fmt.Fprintln(stdout, style.Bold.Render(app.Name)+" "+badge(stdout, style.App(app), app.Status()))
	built := ""
	for _, deploy := range detail.Deploys {
		if deploy.Number == app.LiveNumber && deploy.Detected != nil {
			built = deploy.Detected.Built()
			if deploy.Detected.Builder != "" {
				built = "Railpack: " + deploy.Detected.Builder
			}
		}
	}
	domainText := "none"
	if len(app.Domains) > 0 {
		domainText = strings.Join(app.Domains, ", ")
		if app.Canonical != "" {
			domainText += " (canonical " + app.Canonical + ")"
		}
	}
	from := "your folder, " + build.Name + " deploy"
	switch app.DeployFrom {
	case "github":
		auto := "every push"
		if !app.AutoDeploy {
			auto = "by hand"
		}
		from = "GitHub, " + app.Repo + ", branch " + app.Branch + ", " + auto
	case "ci":
		from = "your CI, through the deploy hook"
		if app.HookImage != "" {
			from += ": new tags of " + app.HookImage
		}
		if app.Repo != "" {
			from += ", compose.yml from " + app.Repo
		}
	}
	password := "off"
	if app.Protected {
		password = "on"
	}
	instantText := "none"
	if app.Instant != "" {
		instantText = app.Instant
		if !app.InstantOpen {
			instantText += ", with a password"
		}
	} else if app.InstantProvider == "off" {
		instantText = "off"
	}
	rows := [][]string{{"server", app.ServerName}, {"domains", domainText}, {"instant", instantText}, {"password", password}, {"deploy from", from}}
	for _, service := range slices.Sorted(maps.Keys(app.ServiceDomains)) {
		rows = append(rows, []string{service, strings.Join(app.ServiceDomains[service], ", ")})
	}
	if built != "" {
		rows = append(rows, []string{"built", built})
	}
	for _, row := range rows {
		fmt.Fprintln(stdout, "  "+style.Muted.Render(fmt.Sprintf("%-11s", row[0]))+" "+row[1])
	}
	if app.ChangesWaiting > 0 {
		fmt.Fprintln(stdout)
		hint(stdout, changesWaiting(app.ChangesWaiting)+". A restart applies them:", build.Name+" restart "+app.Name)
	}
	fmt.Fprintln(stdout)
	printDeploys(stdout, detail, now)
}

// PrintApps writes the app table.
func PrintApps(stdout io.Writer, list []client.App) {
	if len(list) == 0 {
		fmt.Fprintln(stdout, "No apps yet.")
		hint(stdout, "Deploy one from its repo with", build.Name+" deploy")
		return
	}
	rows := make([][]string, 0, len(list))
	for _, app := range list {
		rows = append(rows, []string{style.Bold.Render(app.Name), app.ServerName, badge(stdout, style.App(app), app.Status())})
	}
	table(stdout, []string{"NAME", "SERVER", "STATUS"}, rows)
}

// deploy starts a deploy and follows it: the pushed commit of the GitHub
// repo of this folder, a --ref of it, or an --image built elsewhere.
func deploy(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("deploy", "deploy [--server <server>] [--image <image>] [--ref <ref>] [--detach] [--yes] [app]").withApp().withYes()
	server := flags.String("server", "", "server for a new app")
	detach := flags.Bool("detach", false, "start the deploy and do not wait for it")
	image := flags.String("image", "", "deploy this image, built elsewhere, as the main service, with the compose file of --ref in the linked GitHub repo")
	ref := flags.String("ref", "", "deploy this branch, tag or commit of the linked GitHub repo; default its branch")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if picked.from == client.FromDirectory {
		name, err = repoApp(ctx, api, name)
		if err != nil {
			return err
		}
	}
	if *ref != "" && !protocol.ValidRefName(*ref) {
		return fmt.Errorf("%q is not a valid branch, tag or commit name", *ref)
	}
	if *image != "" {
		// The server takes the compose file of the ref from GitHub, or
		// keeps the one of the live deploy for an app without a repo, and
		// pulls the image.
		step(stdout, "Deploying %s", *image)
		started, err := api.DeployImage(ctx, name, *image, *ref)
		if err != nil {
			return err
		}
		printCommit(stdout, started)
		return follow(ctx, api, name, started, "Deploy", *detach, stdout)
	}
	from, commit := *ref, *ref
	if commit == "" {
		// This folder deploys what it has on GitHub, the commit it is at.
		from, commit, err = client.PushedCommit(".")
		if err != nil {
			return err
		}
		err = followRepo(ctx, api, name, *server, from, flags.yes(), stdin, stdout)
		if err != nil {
			return err
		}
	}
	// The server downloads the commit from GitHub and builds it.
	step(stdout, "Deploying %s from GitHub", from)
	started, err := api.DeployRef(ctx, name, commit)
	if err != nil {
		return err
	}
	printCommit(stdout, started)
	return follow(ctx, api, name, started, "Deploy", *detach, stdout)
}

// repoApp is the app moin deploy deploys when no app is named: the one app
// that follows the GitHub repo of this folder, else the folder's name.
func repoApp(ctx context.Context, api *client.Client, folderName string) (string, error) {
	repo := client.OriginRepo(".")
	if repo == "" {
		return folderName, nil
	}
	apps, err := api.Apps(ctx)
	if err != nil {
		return "", err
	}
	following := []string{}
	for _, app := range apps {
		if strings.EqualFold(app.Repo, repo) {
			following = append(following, app.Name)
		}
	}
	if len(following) != 1 {
		return folderName, nil
	}
	picked = pickedApp{name: following[0], from: "the app that follows " + repo}
	return following[0], nil
}

// printCommit names the GitHub commit a deploy came from, if any.
func printCommit(stdout io.Writer, started client.Deploy) {
	if started.Commit != "" {
		step(stdout, "commit %s %s", started.Commit[:min(7, len(started.Commit))], style.Muted.Render(started.CommitMessage))
	}
}

// followRepo makes the app follow repo before moin deploy deploys a commit
// of it. An app that does not exist yet is created after a question, so a
// typo makes no new app: on the server --server names, or on the only
// server. An app that follows no repo is linked to this one, without
// deploys on push.
func followRepo(ctx context.Context, api *client.Client, name, server, repo string, yes bool, stdin io.Reader, stdout io.Writer) error {
	detail, err := api.App(ctx, name)
	if client.IsNotFound(err) {
		err = confirm(stdin, stdout, yes, "App "+name+" does not exist. Create it?")
		if err == nil {
			err = createApp(ctx, api, name, server, stdout)
		}
	}
	if err != nil {
		return err
	}
	switch {
	case strings.EqualFold(detail.App.Repo, repo):
		return nil
	case detail.App.Repo != "":
		return fmt.Errorf("%s follows %s, and this folder is %s: link it with %s link %s %s, then deploy again", name, detail.App.Repo, repo, build.Name, repo, name)
	}
	app, err := linkRepo(ctx, api, name, repo, "", false)
	if err != nil {
		return err
	}
	done(stdout, "%s follows %s@%s", app.Name, app.Repo, app.Branch)
	return nil
}

func createApp(ctx context.Context, api *client.Client, name, server string, stdout io.Writer) error {
	if server == "" {
		servers, err := api.Servers(ctx)
		if err != nil {
			return err
		}
		if len(servers) != 1 {
			return fmt.Errorf("app %s does not exist yet: pass --server, you have %d servers", name, len(servers))
		}
		server = servers[0].Name
	}
	app, err := api.AddApp(ctx, name, server)
	if err != nil {
		return err
	}
	done(stdout, "Created app %s on %s", style.Bold.Render(app.Name), app.ServerName)
	printInstantPassword(stdout, app.InstantPassword)
	return nil
}

// printInstantPassword shows the password of a new app's instant address,
// the one time moin shows it.
func printInstantPassword(stdout io.Writer, password string) {
	if password == "" {
		return
	}
	fmt.Fprintln(stdout, "  "+style.Warning.Render("The password of its instant address, shown only this once:"))
	fmt.Fprintln(stdout, "  user "+instantUser+", password "+password)
}

// follow prints the deploy log and fails when the deploy fails. what is
// "Deploy", "Redeploy" or "Rollback".
func follow(ctx context.Context, api *client.Client, name string, started client.Deploy, what string, detach bool, stdout io.Writer) error {
	to := ""
	if started.FromNumber > 0 {
		to = fmt.Sprintf(" to v%d", started.FromNumber)
	}
	step(stdout, "%s v%d of %s%s started", what, started.Number, style.Bold.Render(name), to)
	if detach {
		hint(stdout, "It goes on on the server. Watch it with", build.Name+" deploys "+name)
		return nil
	}
	progress := newProgress(stdout, "waiting for the server")
	result, err := api.FollowDeploy(ctx, name, started.Number, func(line string) {
		// The result line below says the same.
		if isResult(line) {
			return
		}
		progress.line(line)
	})
	progress.end()
	switch {
	case ctx.Err() != nil:
		fmt.Fprintln(stdout)
		hint(stdout, "Stopped watching. The deploy goes on on the server. See it with", fmt.Sprintf("%s deploys %s v%d", build.Name, name, started.Number))
		return errReported
	case err != nil:
		return err
	case result.Status != "succeeded":
		// The stream may end before the error is stored: ask once more.
		if result.Error == "" {
			if stored, err := api.GetDeploy(ctx, name, result.Number); err == nil {
				result.Error = stored.Error
			}
		}
		if result.Error == "" {
			result.Error = fmt.Sprintf("see %s deploys %s v%d", build.Name, name, result.Number)
		}
		fmt.Fprintln(stdout, style.Danger.Render(fmt.Sprintf("%s %s v%d of %s failed: %s", style.SymbolFailed, what, result.Number, name, result.Error)))
		if app, err := api.App(ctx, name); err == nil && app.App.LiveNumber > 0 {
			hint(stdout, fmt.Sprintf("v%d stays live. Its logs:", app.App.LiveNumber), build.Name+" logs "+name)
		} else {
			hint(stdout, "Fix it and deploy again with", build.Name+" deploy")
		}
		return errReported
	}
	took := style.Muted.Render(fmt.Sprintf(" v%d in %s", result.Number, progress.elapsed()))
	app, err := api.App(ctx, name)
	if err != nil {
		fmt.Fprintln(stdout, style.Success.Render(style.SymbolDone)+" "+style.Bold.Render("moin. "+name+" is live")+took)
	} else {
		checks, _ := api.CheckDomains(ctx, name)
		printLive(app.App, checks, took, stdout)
	}
	hint(stdout, "See it run with", build.Name+" logs "+name)
	return nil
}

// printLive is the moin moment after a deploy: ✓ moin. and where the app
// answers, its domains and its instant address, with how to get past its
// password.
func printLive(app client.App, checks []client.DomainCheck, took string, stdout io.Writer) {
	urls := []string{}
	for _, check := range checks {
		urls = append(urls, "https://"+check.Domain)
	}
	if app.Instant != "" {
		urls = append(urls, app.Instant)
	}
	line := "moin. " + app.Name + " is live"
	if len(urls) > 0 {
		line += " at " + urls[0]
	}
	fmt.Fprintln(stdout, style.Success.Render(style.SymbolDone)+" "+style.Bold.Render(line)+took)
	if len(urls) > 1 {
		fmt.Fprintln(stdout, "  also at "+strings.Join(urls[1:], ", "))
	}
	if app.Instant != "" && !app.InstantOpen && !app.Protected {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("A password guards "+app.Instant+", shown once when the app was made. A new one: "+build.Name+" instant password "+app.Name))
	}
}

// isResult reports whether a deploy log line is the daemon's last word,
// "v4 is live" or "deploy failed: …".
func isResult(line string) bool {
	if strings.HasPrefix(line, "deploy failed: ") {
		return true
	}
	number, ok := strings.CutSuffix(line, " is live")
	_, err := strconv.Atoi(strings.TrimPrefix(number, "v"))
	return ok && strings.HasPrefix(number, "v") && err == nil
}

// deployNumber reads "v3" or "3".
func deployNumber(text string) (int, bool) {
	number, err := strconv.Atoi(strings.TrimPrefix(text, "v"))
	return number, err == nil && number > 0
}

// appAndNumber splits the arguments of rollback and deploys: an app and a
// deploy number like v3, both optional, in any order.
func appAndNumber(flags *flagSet) (string, int, error) {
	app, number := "", 0
	for _, argument := range flags.Args() {
		if parsed, ok := deployNumber(argument); ok && number == 0 {
			number = parsed
			continue
		}
		if app != "" {
			return "", 0, flags.wrong()
		}
		app = argument
	}
	name, err := flags.app(app)
	return name, number, err
}

func rollback(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("rollback", "rollback [--app <app>] [app] [v<number>] [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 2 {
		return flags.wrong()
	}
	name, number, err := appAndNumber(flags)
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	target := "the deploy before the live one"
	if number != 0 {
		target = fmt.Sprintf("v%d", number)
	}
	err = confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Roll %s back to %s? It replaces the live deploy.", name, target))
	if err != nil {
		return err
	}
	started, err := api.Rollback(ctx, name, number)
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, "Rollback", false, stdout)
}

// deploys lists the newest deploys of an app, or prints one with its log.
func deploys(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("deploys", "deploys [--app <app>] [app] [v<number>]").withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 2 {
		return flags.wrong()
	}
	name, number, err := appAndNumber(flags)
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	now := time.Now()
	if number > 0 {
		deploy, err := api.GetDeploy(ctx, name, number)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, style.Bold.Render(fmt.Sprintf("v%d of %s", deploy.Number, name))+" "+badge(stdout, style.Deploy(deploy, 0), deploy.Status)+style.Muted.Render(" · "+deployTimes(deploy, now)))
		if note := deployNote(deploy); note != "" {
			fmt.Fprintln(stdout, style.Muted.Render(note))
		}
		fmt.Fprintln(stdout)
		for _, line := range deploy.Log {
			text, isStep := style.LogLine(line)
			switch {
			case text == "":
			case isStep:
				fmt.Fprintln(stdout, style.Step(text))
			default:
				fmt.Fprintln(stdout, "    "+style.Noise(text))
			}
		}
		if len(deploy.Log) == 0 {
			fmt.Fprintln(stdout, style.Muted.Render("No log lines."))
		}
		// The daemon's log usually ends with the error already.
		logged := len(deploy.Log) > 0 && strings.HasSuffix(deploy.Log[len(deploy.Log)-1], "deploy failed: "+deploy.Error)
		if deploy.Status == "failed" && deploy.Error != "" && !logged {
			fmt.Fprintln(stdout, style.Danger.Render(style.SymbolFailed+" "+deploy.Error))
		}
		return nil
	}
	detail, err := api.App(ctx, name)
	if err != nil {
		return err
	}
	printDeploys(stdout, detail, now)
	return nil
}

// printDeploys writes the deploy history of an app.
func printDeploys(stdout io.Writer, detail client.AppDetail, now time.Time) {
	name := detail.App.Name
	if len(detail.Deploys) == 0 {
		fmt.Fprintf(stdout, "%s has no deploys yet.\n", name)
		hint(stdout, "Deploy it with", build.Name+" deploy")
		return
	}
	rows := make([][]string, 0, len(detail.Deploys))
	for _, deploy := range detail.Deploys {
		took := "-"
		if deploy.FinishedAt != nil {
			took = human.Duration(deploy.FinishedAt.Sub(deploy.CreatedAt))
		}
		status := deploy.Status
		if deploy.Number == detail.App.LiveNumber && status == "succeeded" {
			status = "live"
		}
		rows = append(rows, []string{fmt.Sprintf("v%d", deploy.Number), badge(stdout, style.Deploy(deploy, detail.App.LiveNumber), status), human.Ago(deploy.CreatedAt, now), took, deployNote(deploy)})
	}
	table(stdout, []string{"DEPLOY", "STATUS", "STARTED", "TOOK", "NOTE"}, rows)
	fmt.Fprintln(stdout)
	hint(stdout, "The log of one:", fmt.Sprintf("%s deploys %s v%d", build.Name, name, detail.Deploys[0].Number))
}

// deployTimes is "started 3 min ago, took 41s".
func deployTimes(deploy client.Deploy, now time.Time) string {
	text := "started " + human.Ago(deploy.CreatedAt, now)
	if deploy.FinishedAt != nil {
		text += ", took " + human.Duration(deploy.FinishedAt.Sub(deploy.CreatedAt))
	}
	return text
}

// deployNote says where a deploy came from, or why it failed.
func deployNote(deploy client.Deploy) string {
	note := ""
	switch {
	case deploy.FromNumber > 0:
		note = fmt.Sprintf("rollback to v%d", deploy.FromNumber)
	case deploy.Commit != "":
		ref := deploy.Ref
		// A deploy of a commit by its SHA names it once.
		if strings.HasPrefix(deploy.Commit, ref) {
			ref = ""
		}
		note = ref + " " + deploy.Commit[:min(7, len(deploy.Commit))] + " " + deploy.CommitMessage
	case deploy.Image != "":
		note = deploy.Image
	}
	if deploy.Status == "failed" && deploy.Error != "" {
		if note != "" {
			note += ": "
		}
		note += deploy.Error
	}
	return clip(strings.TrimSpace(strings.ReplaceAll(note, "\n", " ")), 70)
}

func redeploy(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("redeploy", "redeploy [app]").withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	started, err := api.Redeploy(ctx, name)
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, "Redeploy", false, stdout)
}

// hook gives the app a new deploy hook URL for CI. It asks first when the
// old URL stops working or pushes stop deploying.
func hook(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("hook", "hook [app] [--yes], or hook off [app] [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	off := flags.Arg(0) == "off"
	argument := flags.Arg(0)
	if off {
		argument = flags.Arg(1)
	}
	if flags.NArg() > 2 || (!off && flags.NArg() > 1) {
		return flags.wrong()
	}
	name, err := flags.app(argument)
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if off {
		return hookOff(ctx, api, name, stdin, stdout, flags.yes())
	}
	detail, err := api.App(ctx, name)
	if err != nil {
		return err
	}
	// Only a hook that exists stops working, and only deploy on push
	// turns off: one thing brings new versions. The repo stays linked for
	// the compose file. The first hook of an app without deploy on push
	// asks nothing.
	question := ""
	if detail.App.Hook {
		question = fmt.Sprintf("Make a new deploy hook for %s? The old one stops working.", name)
	}
	if detail.App.AutoDeploy {
		question = fmt.Sprintf("Make a deploy hook for %s? Pushes to %s no longer deploy it; CI does.", name, detail.App.Repo)
	}
	if question != "" {
		err = confirm(stdin, stdout, flags.yes(), question)
		if err != nil {
			return err
		}
	}
	url, err := api.RotateDeployHook(ctx, name)
	if err != nil {
		return err
	}
	done(stdout, "New deploy hook for %s. It is shown once; the old one stops working:", name)
	fmt.Fprintf(stdout, "\n  %s\n\n", url)
	fmt.Fprintln(stdout, "Keep it secret, like a password.")
	if detail.App.HookImage == "" {
		fmt.Fprintln(stdout, "It waits for an image. Deploy one by hand once, then it deploys its new tags.")
	} else {
		fmt.Fprintf(stdout, "It deploys new tags of %s.\n", detail.App.HookImage)
	}
	fmt.Fprintln(stdout)
	hint(stdout, "Add it to your CI:", api.URL()+"/docs/guides/ci#3-the-workflow")
	return nil
}

// hookOff turns the deploy hook of an app off after asking.
func hookOff(ctx context.Context, api *client.Client, name string, stdin io.Reader, stdout io.Writer, yes bool) error {
	detail, err := api.App(ctx, name)
	if err != nil {
		return err
	}
	if !detail.App.Hook {
		done(stdout, "Nothing changed: %s has no deploy hook.", name)
		return nil
	}
	err = confirm(stdin, stdout, yes, fmt.Sprintf("Turn off the deploy hook of %s? CI can no longer deploy it.", name))
	if err != nil {
		return err
	}
	err = api.RemoveDeployHook(ctx, name)
	if err != nil {
		return err
	}
	done(stdout, "Turned off the deploy hook of %s. Its URL no longer works.", name)
	return nil
}

// registry manages the logins daemons use to pull private images.
func registry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	// Help works without a login.
	api, apiErr := apiClient()
	switch command {
	case "":
		flags := newFlags("registry", "registry")
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		list, err := api.Registries(ctx)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			hint(stdout, "No registry logins. Images from public registries pull without one; for private ones:", build.Name+" registry set ghcr.io <user>")
			return nil
		}
		rows := make([][]string, 0, len(list))
		for _, login := range list {
			rows = append(rows, []string{login.Host, login.Username, login.CreatedAt.Local().Format("Jan 2 2006"), keptOn(login.Servers, login.MissingOn)})
		}
		table(stdout, []string{"HOST", "USERNAME", "ADDED", "LOGIN ON"}, rows)
		for _, login := range list {
			for _, server := range login.MissingOn {
				hint(stdout, server+" has no login for "+login.Host+". Enter it:", build.Name+" registry set "+login.Host+" "+login.Username+" --server "+server)
			}
		}
		return nil
	case "set":
		flags := newFlags("registry set", "registry set <host> <username> [--server <server>], then the password or token on stdin")
		server := flags.String("server", "", "the server that keeps the login; needed with more than one")
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		if flags.NArg() != 2 {
			return flags.wrong()
		}
		host, username := flags.Arg(0), flags.Arg(1)
		if isTerminal(stdin) {
			fmt.Fprintf(stdout, "Password or token for %s on %s: ", username, host)
		}
		password, err := readSecret(stdin)
		if isTerminal(stdin) {
			fmt.Fprintln(stdout)
		}
		if err != nil {
			return err
		}
		err = api.SetRegistry(ctx, host, username, password, *server)
		if err != nil {
			return err
		}
		done(stdout, "Saved the login for %s on the server. Image deploys from it pull with it.", host)
		return nil
	case "rm":
		flags := newFlags("registry rm", "registry rm <host> [--yes]").withYes()
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		host := flags.Arg(0)
		err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove the login for %s? Private images from it stop pulling.", host))
		if err != nil {
			return err
		}
		err = api.RemoveRegistry(ctx, host)
		if err != nil {
			return err
		}
		done(stdout, "Removed the login for %s, also from the servers.", host)
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "registry "+command, build.Name)
}

func logs(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("logs", "logs [--tail 200] [app]").withApp()
	tail := flags.Int("tail", 200, "lines per container")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	lines, err := api.Logs(ctx, name, *tail)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		fmt.Fprintf(stdout, "%s has written no log lines yet.\n", name)
	}
	for _, line := range lines {
		fmt.Fprintln(stdout, logLine(line))
	}
	return nil
}

// envCommands are the subcommands of env. Any other first argument is the
// app whose env vars to list.
var envCommands = []string{"set", "rm", "push"}

// env lists the env vars of an app, and sets, removes and pushes them. In
// every one, usage mistakes show before a missing login.
func env(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	command := ""
	if len(args) > 0 && slices.Contains(envCommands, args[0]) {
		command, args = args[0], args[1:]
	}
	switch command {
	case "":
		return envList(ctx, args, stdout)
	case "set":
		return envSet(ctx, args, stdout)
	case "rm":
		return envRm(ctx, args, stdin, stdout)
	case "push":
		return envPush(ctx, args, stdin, stdout)
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "env "+command, build.Name)
}

// envList lists the env vars of an app: each key and when it changed.
// Values are write-only, so there is none to show.
func envList(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("env", "env [app]").withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	api, apiErr := apiClient()
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	vars, err := api.Env(ctx, name)
	if err != nil {
		return err
	}
	if len(vars) == 0 {
		fmt.Fprintf(stdout, "%s has no env vars.\n", name)
		hint(stdout, "Set one with", build.Name+" env set KEY=value")
		return nil
	}
	var rows [][]string
	now := time.Now()
	for _, envVar := range vars {
		rows = append(rows, []string{envVar.Key, human.Ago(envVar.UpdatedAt, now)})
	}
	table(stdout, []string{"KEY", "CHANGED"}, rows)
	if isTTY(stdout) {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Values are write-only: you see the name and when it changed, never the value."))
	}
	return nil
}

// envSet sets KEY=value pairs on an app; a first argument without = is the
// app.
func envSet(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("env set", "env set [--app <app>] [app] KEY=value ...").withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	api, apiErr := apiClient()
	pairs := flags.Args()
	app := ""
	if len(pairs) > 0 && !strings.Contains(pairs[0], "=") {
		app, pairs = pairs[0], pairs[1:]
	}
	set := map[string]string{}
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return fmt.Errorf("%q is not KEY=value", pair)
		}
		set[key] = value
	}
	if len(set) == 0 {
		return flags.wrong()
	}
	name, err := flags.app(app)
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	_, err = api.SetEnv(ctx, name, set, nil)
	if err != nil {
		return err
	}
	done(stdout, "Set %s on %s", strings.Join(sortedKeys(set), ", "), style.Bold.Render(name))
	hint(stdout, "The app gets them when it restarts. Now:", build.Name+" restart "+name)
	return nil
}

// envRm removes keys from an app after asking. Only keys the app has are
// removed; the others say so.
func envRm(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("env rm", "env rm [--app <app>] KEY ... [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	api, apiErr := apiClient()
	if flags.NArg() == 0 {
		return flags.wrong()
	}
	name, err := flags.app("")
	if err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	current, err := api.Env(ctx, name)
	if err != nil {
		return err
	}
	var remove, missing []string
	for _, key := range flags.Args() {
		if slices.ContainsFunc(current, func(envVar client.EnvVar) bool { return envVar.Key == key }) {
			remove = append(remove, key)
		} else {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stdout, "%s has no %s.\n", name, strings.Join(missing, ", "))
	}
	if len(remove) == 0 {
		return nil
	}
	keys := strings.Join(remove, ", ")
	err = confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove %s from %s?", keys, name))
	if err != nil {
		return err
	}
	_, err = api.SetEnv(ctx, name, nil, remove)
	if err != nil {
		return err
	}
	done(stdout, "Removed %s from %s", keys, style.Bold.Render(name))
	hint(stdout, "The app drops them when it restarts. Now:", build.Name+" restart "+name)
	return nil
}

// envPush replaces the env vars of an app with a file, .env by default:
// it shows which keys it adds, replaces and removes, and asks first.
func envPush(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("env push", "env push [--app <app>] [file] [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	api, apiErr := apiClient()
	path, err := envFile(flags)
	if err != nil {
		return err
	}
	name, err := flags.app("")
	if err != nil {
		return err
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, err := dotenv.Parse(string(text))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if apiErr != nil {
		return apiErr
	}
	current, err := api.Env(ctx, name)
	if err != nil {
		return err
	}
	keys := make([]string, len(current))
	for index, envVar := range current {
		keys[index] = envVar.Key
	}
	change := dotenv.Diff(keys, next)
	if change.None() {
		fmt.Fprintf(stdout, "%s has no env vars, and %s has none. Nothing changes.\n", path, name)
		return nil
	}
	printEnvChange(stdout, change)
	question := fmt.Sprintf("Replace the env vars of %s with %s?", name, path)
	if len(change.Removed) > 0 {
		question = fmt.Sprintf("Replace the env vars of %s with %s? %s %s.", name, path, strings.Join(change.Removed, ", "), pluralWord(int64(len(change.Removed)), "is removed", "are removed"))
	}
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	change, err = api.ReplaceEnv(ctx, name, next)
	if err != nil {
		return err
	}
	done(stdout, "The env vars of %s: %s", style.Bold.Render(name), change)
	hint(stdout, "The app gets them when it restarts. Now:", build.Name+" restart "+name)
	return nil
}

// envFile is the file env push reads: the one argument, or .env.
func envFile(flags *flagSet) (string, error) {
	switch flags.NArg() {
	case 0:
		return ".env", nil
	case 1:
		return flags.Arg(0), nil
	}
	return "", flags.wrong()
}

// printEnvChange lists the keys a push adds, replaces and removes. The keys
// only, never a value: the terminal may be shared or recorded.
func printEnvChange(stdout io.Writer, change dotenv.Change) {
	for _, line := range []struct {
		mark string
		keys []string
		tint lipgloss.Style
	}{{"+", change.Added, style.Success}, {"~", change.Replaced, style.Warning}, {"-", change.Removed, style.Danger}} {
		for _, key := range line.keys {
			fmt.Fprintln(stdout, "  "+line.tint.Render(line.mark)+" "+key)
		}
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// logLine mutes the container prefix of a log line, "web-1  | ".
func logLine(line string) string {
	prefix, rest, ok := strings.Cut(line, " | ")
	if !ok || strings.TrimSpace(prefix) == "" || strings.Contains(strings.TrimSpace(prefix), " ") {
		return line
	}
	return style.Muted.Render(prefix+" |") + " " + rest
}

// changesWaiting says how many saved changes wait for a restart.
func changesWaiting(n int) string {
	if n == 1 {
		return "1 change waiting"
	}
	return fmt.Sprintf("%d changes waiting", n)
}
