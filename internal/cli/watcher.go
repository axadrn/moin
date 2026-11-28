package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/internal/watcher"
)

// watcherCommand is `moin watcher`, `watcher connect`, `watcher
// disconnect` and `watcher logout`, on your laptop, and `moin watcher
// run` and `moin watcher token`, on the machine that runs a watcher.
func watcherCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "run":
		return watcherRun(ctx, args[1:])
	case "token":
		return watcherToken(args[1:], stdin, stdout)
	case "connect":
		return watcherConnect(ctx, args[1:], stdin, stdout)
	case "disconnect":
		return watcherDisconnect(ctx, args[1:], stdin, stdout)
	case "logout":
		flags := newFlags("watcher logout", "watcher logout")
		if err := flags.parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() > 0 {
			return flags.wrong()
		}
		return cloudLogout(ctx, stdout)
	}
	return watcherList(ctx, args, stdout)
}

// serverWatchers is a server and the watchers it reports to, for
// `moin watcher --json`.
type serverWatchers struct {
	Server   string   `json:"server"`
	Watchers []string `json:"watchers"`
	Error    string   `json:"error,omitempty"`
}

// watcherList is `moin watcher`: the watchers each server reports to,
// and for moin cloud the plan, the days left of the trial and where to
// pay.
func watcherList(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("watcher", "watcher [--json], or watcher connect|disconnect|logout|run|token").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unknown command %q. `%s watcher` takes connect, disconnect, logout, run or token", flags.Arg(0), build.Name)
	}
	list, err := loadServers()
	if err != nil {
		return err
	}
	servers := []serverWatchers{}
	toCloud := ""
	for _, row := range askServers(ctx, list) {
		server := serverWatchers{Server: row.server, Watchers: row.status.Watchers}
		if row.err != nil {
			server.Error = explain(row.err)
		}
		if slices.Contains(server.Watchers, cloudURL()) {
			toCloud = row.server
		}
		servers = append(servers, server)
	}
	var space *cloudWorkspace
	if session, err := loadCloudSession(); err == nil {
		space = &cloudWorkspace{}
		if _, err := session.call(ctx, http.MethodGet, "/api/cloud", nil, space); err != nil {
			printError(os.Stderr, err)
			space = nil
		} else {
			space.TrialDaysLeft = trialDaysLeft(*space, time.Now())
		}
	}
	if flags.json() {
		return printJSON(stdout, struct {
			Servers []serverWatchers `json:"servers"`
			Cloud   *cloudWorkspace  `json:"cloud"`
		}{servers, space})
	}
	if len(servers) == 0 {
		fmt.Fprintln(stdout, "No servers yet.")
		hint(stdout, "Your first deploy adds one:", build.Name+" deploy <server>")
	} else {
		printWatchers(stdout, servers)
	}
	switch {
	case space != nil:
		fmt.Fprintln(stdout)
		printCloud(stdout, *space)
	case toCloud != "":
		fmt.Fprintln(stdout)
		hint(stdout, "Sign in to see your plan of "+cloudName+":", build.Name+" watcher connect "+toCloud)
	}
	return nil
}

// printWatchers writes the table of servers and their watchers.
func printWatchers(stdout io.Writer, servers []serverWatchers) {
	rows := [][]string{}
	for _, server := range servers {
		if server.Error != "" {
			rows = append(rows, []string{style.Bold.Render(server.Server), style.Muted.Render(server.Error)})
			continue
		}
		watchers := style.Warning.Render("none: nobody notices when it is gone")
		if len(server.Watchers) > 0 {
			names := []string{}
			for _, address := range server.Watchers {
				if address == cloudURL() {
					address = cloudName
				}
				names = append(names, address)
			}
			watchers = strings.Join(names, ", ")
		}
		rows = append(rows, []string{style.Bold.Render(server.Server), watchers})
	}
	table(stdout, []string{"SERVER", "WATCHERS"}, rows)
}

// printCloud writes your workspace in moin cloud: the plan, and where to
// pay.
func printCloud(stdout io.Writer, space cloudWorkspace) {
	plan := space.Plan
	if space.TrialDaysLeft > 0 {
		plan = fmt.Sprintf("Trial, %d days left", space.TrialDaysLeft)
	}
	fmt.Fprintln(stdout, style.Bold.Render(cloudName)+"  "+space.Mail+"  "+style.Muted.Render(plan))
	if space.Status == "paid" || space.Storage > 0 {
		fmt.Fprintln(stdout, style.Muted.Render("Copies: "+human.Size(float64(space.Storage))+" of "+human.Size(float64(space.StorageLimit))))
	}
	if space.Billing != "" {
		fmt.Fprintln(stdout, style.Muted.Render("Billing: "+space.Billing))
	}
}

// watcherConnect is `moin watcher connect <server> [<url> <token>]`: the
// server reports to the watcher at url from now on. Without an address it
// is moin cloud, and the browser signs this laptop in the first time. An
// address without its token asks for the token without showing it, or
// reads it from stdin.
func watcherConnect(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("watcher connect", "watcher connect <server> [<url> <token>]")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, rest, err := flags.serverArgs(stdin, 2)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return cloudConnect(ctx, server, stdout)
	}
	to := daemon.Watcher{URL: rest[0]}
	if len(rest) == 2 {
		to.Token = rest[1]
	} else if to.Token, err = askSecret(stdin, stdout, "Token of "+to.URL+":", "token"); err != nil {
		return err
	}
	if err := sendJSON(ctx, server, to, "watchers", "connect"); err != nil {
		return err
	}
	done(stdout, "%s reports to %s every minute", server, to.URL)
	return nil
}

// watcherDisconnect is `moin watcher disconnect <server> [url]`: the
// server stops reporting there. Without an address it is moin cloud.
func watcherDisconnect(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("watcher disconnect", "watcher disconnect <server> [url] [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, rest, err := flags.serverArgs(stdin, 1)
	if err != nil {
		return err
	}
	address := ""
	if len(rest) == 1 {
		address = rest[0]
	}
	if address == "" || address == cloudURL() {
		return cloudDisconnect(ctx, server, flags.yes(), stdin, stdout)
	}
	question := fmt.Sprintf("Stop the reports of %s to %s? That watcher will say the server is silent.", server, address)
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	if err := sshRun(ctx, server, hereLine("watchers", "disconnect", address), nil, io.Discard, os.Stderr); err != nil {
		return err
	}
	done(stdout, "%s no longer reports to %s", server, address)
	return nil
}

// watcherRun runs the watcher until it is stopped.
func watcherRun(ctx context.Context, args []string) error {
	flags := newFlags("watcher run", "watcher run [--listen :8080] [--config "+watcher.DefaultConfig+"] [--dir "+watcher.DefaultDir+"]")
	listen := flags.String("listen", ":8080", "the address it listens on")
	config := flags.String("config", watcher.DefaultConfig, "the config file")
	dir := flags.String("dir", watcher.DefaultDir, "where it keeps what it knows")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	return watcher.Run(ctx, *listen, *config, *dir)
}

// askWatcherURL asks for the watcher's own address, which it cannot know:
// on a terminal, or else an error that names --url.
func askWatcherURL(stdin io.Reader, stdout io.Writer) (string, error) {
	if !isTerminal(stdin) {
		return "", errors.New("name the address daemons reach this watcher at with --url, like --url https://watcher.example.com")
	}
	fmt.Fprint(stdout, style.Warning.Render("?")+" The address daemons reach this watcher at, like https://watcher.example.com: ")
	address, err := readLine(stdin)
	if err == nil && address == "" {
		err = errors.New("no address, so no token. Name it with --url")
	}
	return address, err
}

// watcherToken prints a new token for a server: the line for the watcher's
// config, or a whole starter config when there is none yet, and the
// command that points the server at this watcher.
func watcherToken(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("watcher token", "watcher token <server> [--url https://watcher.example.com] [--config "+watcher.DefaultConfig+"]")
	address := flags.String("url", "", "the address daemons reach this watcher at; asked on a terminal")
	path := flags.String("config", watcher.DefaultConfig, "the config file")
	if err := flags.parse(args); err != nil {
		return err
	}
	name, err := flags.oneArg()
	if err != nil {
		return err
	}
	if *address == "" {
		*address, err = askWatcherURL(stdin, stdout)
		if err != nil {
			return err
		}
	}
	if err := watcher.CheckName(name); err != nil {
		return err
	}
	config, err := watcher.LoadConfig(*path)
	fresh := errors.Is(err, fs.ErrNotExist)
	if err != nil && !fresh {
		return err
	}
	if _, ok := config.Servers[name]; ok {
		return fmt.Errorf("%s is in %s already. Remove its line first for a new token", name, *path)
	}
	token := watcher.NewToken()
	if fresh {
		fmt.Fprintf(stdout, `1. Save this as %s, then run: chmod 600 %s

password: %s
servers:
  %s: %s

`, *path, *path, watcher.NewToken(), name, token)
	} else {
		fmt.Fprintf(stdout, `1. Put this line under servers: in %s, then restart the watcher:

  %s: %s

`, *path, name, token)
	}
	fmt.Fprintf(stdout, `2. On your laptop, point %s at this watcher:

  %s watcher connect %s %s %s

3. Alerts go where you send them, on your laptop:

  %s alerts add %s telegram
`, name, build.Name, name, *address, token, build.Name, name)
	return nil
}
