package cli

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// deployName is a deploy as you name it on the command line, like v12.
var deployName = regexp.MustCompile(`^v[0-9]+$`)

// deploys is `moin deploys [app] [v12]`: the app's deploys, newest first,
// each with where it came from, what it brought and how it ended. With a
// deploy, what that deploy printed.
func deploys(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("deploys", "deploys [app] [v12] [-s server] [--json]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	rest := flags.Args()
	number := 0
	if n := len(rest); n > 0 && deployName.MatchString(rest[n-1]) {
		number, _ = strconv.Atoi(strings.TrimPrefix(rest[n-1], "v"))
		rest = rest[:n-1]
	}
	app, err := deploysApp(flags, rest)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		if number > 0 {
			var deploy daemon.DeployLog
			if err := sshJSON(ctx, server, []string{"deploys", app, strconv.Itoa(number)}, nil, &deploy); err != nil {
				return err
			}
			if flags.json() {
				return printJSON(stdout, deploy)
			}
			printDeployLog(stdout, deploy)
			return nil
		}
		var list []daemon.DeployRecord
		if err := sshJSON(ctx, server, []string{"deploys", app}, nil, &list); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, list)
		}
		printDeploys(stdout, app, list, time.Now())
		return nil
	})
}

// deploysApp is the app of `moin deploys`: the one named, else this
// folder's.
func deploysApp(flags *flagSet, rest []string) (string, error) {
	switch len(rest) {
	case 0:
		return folderApp()
	case 1:
		return rest[0], checkApp(rest[0])
	}
	return "", flags.wrong()
}

// printDeploys is the table of `moin deploys`. When the newest deploy
// failed, it says how to read why.
func printDeploys(stdout io.Writer, app string, list []daemon.DeployRecord, now time.Time) {
	rows := [][]string{}
	for _, deploy := range list {
		took := ""
		if !deploy.Ended.IsZero() {
			took = human.Duration(deploy.Ended.Sub(deploy.At))
		}
		rows = append(rows, []string{"v" + strconv.Itoa(deploy.Number), human.Ago(deploy.At, now), deployFrom(deploy), deploy.What(), deployState(stdout, deploy), took})
	}
	table(stdout, []string{"DEPLOY", "WHEN", "FROM", "WHAT", "STATE", "TOOK"}, rows)
	if len(list) > 0 && list[0].Status == "failed" {
		hint(stdout, "Why it failed:", fmt.Sprintf("%s deploys %s v%d", build.Name, app, list[0].Number))
	}
}

// printDeployLog prints what a deploy printed, after a line that says
// what it was.
func printDeployLog(stdout io.Writer, deploy daemon.DeployLog) {
	line := []string{style.Bold.Render("v" + strconv.Itoa(deploy.Number))}
	for _, part := range []string{deployFrom(deploy.DeployRecord), deploy.What(), deployState(stdout, deploy.DeployRecord)} {
		if part != "" {
			line = append(line, part)
		}
	}
	fmt.Fprintln(stdout, strings.Join(line, " · "))
	if deploy.Log == "" {
		fmt.Fprintln(stdout, style.Muted.Render("No log. Deploys keep one since beta.39."))
		return
	}
	fmt.Fprint(stdout, deploy.Log)
}

// deployFrom is where a deploy came from, like "laptop" or
// "rollback to v9".
func deployFrom(deploy daemon.DeployRecord) string {
	if deploy.From > 0 {
		return fmt.Sprintf("rollback to v%d", deploy.From)
	}
	return deploy.Source
}

// deployState is how a deploy ended, in words and with its color on a
// terminal.
func deployState(w io.Writer, deploy daemon.DeployRecord) string {
	switch {
	case deploy.Live:
		return badge(w, style.Badge(style.StatusLive, "live"), "live")
	case deploy.Status == "failed":
		return badge(w, style.Badge(style.StatusFailed, "failed"), "failed")
	case deploy.Status == "running":
		return badge(w, style.Badge(style.StatusRunning, "running"), "running")
	case deploy.Status == "":
		return style.Muted.Render("no record")
	}
	return deploy.Status
}

// hereDeploys is `moin here deploys <app> [n]`: the app's deploys, or
// deploy n with its log.
func hereDeploys(dir string, args []string, stdout io.Writer) error {
	flags := newFlags("here deploys", "here deploys <app> [n]")
	if err := flags.parse(args); err != nil {
		return err
	}
	switch flags.NArg() {
	case 1:
		value, err := daemon.DeploysHere(dir, flags.Arg(0))
		return respond(stdout, value, err)
	case 2:
		number, err := strconv.Atoi(flags.Arg(1))
		if err != nil {
			return flags.wrong()
		}
		value, err := daemon.DeployLogHere(dir, flags.Arg(0), number)
		return respond(stdout, value, err)
	}
	return flags.wrong()
}
