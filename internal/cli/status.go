package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// statusTimeout is how long `moin` waits for all servers together.
const statusTimeout = 15 * time.Second

// configDir is moin's folder on your laptop, ~/.config/moin.
func configDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, build.Name), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", build.Name), nil
}

// serversFile lists the servers you deployed to, one name per line.
func serversFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "servers"), nil
}

// loadServers reads the servers file, none when it is not there.
func loadServers() ([]string, error) {
	file, err := serversFile()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return parseServers(string(body)), err
}

// savedServer is the server of your list that name means.
func savedServer(name string) string {
	servers, _ := loadServers()
	return matchServer(servers, name)
}

// matchServer is the server of servers that name means: name itself, or
// for a name without user@ the one server with that host, like
// root@my-server for my-server. Any other name stays as it is.
func matchServer(servers []string, name string) string {
	if slices.Contains(servers, name) || strings.Contains(name, "@") {
		return name
	}
	var found []string
	for _, server := range servers {
		if _, host, ok := strings.Cut(server, "@"); ok && host == name {
			found = append(found, server)
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	return name
}

// parseServers reads one server per line. Blank lines and # comments are
// skipped, a name twice counts once.
func parseServers(text string) []string {
	var servers []string
	for line := range strings.Lines(text) {
		name := strings.TrimSpace(line)
		if name != "" && !strings.HasPrefix(name, "#") && !slices.Contains(servers, name) {
			servers = append(servers, name)
		}
	}
	return servers
}

// addServer puts server in the servers file, once.
func addServer(server string) error {
	servers, err := loadServers()
	if err != nil || slices.Contains(servers, server) {
		return err
	}
	return saveServers(append(servers, server))
}

// saveServers writes the servers file.
func saveServers(servers []string) error {
	file, err := serversFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, []byte(strings.Join(servers, "\n")+"\n"), 0o600)
}

// servers is `moin servers`, the servers you deployed to with their
// watchers, and `servers add|rm <server>`.
func servers(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "add" {
		return addServerCommand(ctx, args[1:], stdin, stdout)
	}
	if len(args) > 0 && args[0] == "rm" {
		return removeServerCommand(args[1:], stdin, stdout)
	}
	flags := newFlags("servers", "servers [--json], or servers add|rm <server>").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	list, err := loadServers()
	if err != nil {
		return err
	}
	rows := askServers(ctx, list)
	rememberApps(rows)
	if flags.json() {
		return printJSON(stdout, overviewJSON(rows))
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "No servers yet.")
		hint(stdout, "Your first deploy adds one:", build.Name+" deploy <server>")
		return nil
	}
	lines := [][]string{}
	for _, row := range rows {
		if row.err != nil {
			lines = append(lines, []string{style.Bold.Render(row.server), badge(stdout, style.Badge(style.StatusFailed, "unreachable"), "unreachable"), "", style.Muted.Render(explain(row.err))})
			continue
		}
		watchers := style.Warning.Render("none: nobody notices when it is gone")
		if len(row.status.Watchers) > 0 {
			watchers = strings.Join(row.status.Watchers, " ")
		}
		lines = append(lines, []string{style.Bold.Render(row.server), build.Name + " " + row.status.Version, strconv.Itoa(len(row.status.Apps)), watchers})
	}
	table(stdout, []string{"SERVER", "RUNS", "APPS", "WATCHERS"}, lines)
	return nil
}

// addServerCommand is `moin servers add <server>`, as on a new laptop. It
// asks the server first, so a typo is not added. A server without moin
// gets it, after a yes, with no app: then it runs the firewall, the
// knocks and the reports, and no proxy.
func addServerCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("servers add", "servers add <server> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	name, err := flags.oneArg()
	if err != nil {
		return err
	}
	server := savedServer(name)
	if err := setUp(ctx, server, "", flags.yes(), stdin, stdout); err != nil {
		return err
	}
	status, err := serverStatus(ctx, server)
	if err != nil {
		return err
	}
	rememberApps([]serverRow{{server: server, status: status}})
	if err := addServer(server); err != nil {
		return err
	}
	done(stdout, "Added %s, with %d apps. `%s` shows it now", server, len(status.Apps), build.Name)
	return nil
}

// removeServerCommand is `moin servers rm <server>`: moin forgets the
// server. Nothing changes on it.
func removeServerCommand(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("servers rm", "servers rm <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, _, err := flags.serverArgs(stdin, 0)
	if err != nil {
		return err
	}
	list, err := loadServers()
	if err != nil {
		return err
	}
	if !slices.Contains(list, server) {
		return fmt.Errorf("%s is not in your list: see it with `%s servers`", server, build.Name)
	}
	if err := saveServers(slices.DeleteFunc(list, func(s string) bool { return s == server })); err != nil {
		return err
	}
	apps := loadApps()
	updateApps(apps, server, nil)
	_ = saveApps(apps)
	done(stdout, "%s forgot %s. Nothing changed on the server", build.Name, server)
	return nil
}

// serverStatus asks one server what runs there.
func serverStatus(ctx context.Context, server string) (daemon.Status, error) {
	var status daemon.Status
	err := sshJSON(ctx, server, []string{"apps"}, nil, &status)
	return status, err
}

// serverRow is one server in the table of `moin`: what it said, or why it
// did not answer.
type serverRow struct {
	server string
	status daemon.Status
	err    error
}

// overview is `moin` alone and `moin apps`: every server of the list and
// its apps, asked in parallel, each with one SSH call.
func overview(ctx context.Context, name string, args []string, stdout io.Writer) error {
	flags := newFlags(name, strings.TrimSpace(name+" [--json]")).withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	servers, err := loadServers()
	if err != nil {
		return err
	}
	if len(servers) == 0 && !flags.json() {
		welcome(stdout)
		return nil
	}
	rows := askServers(ctx, servers)
	rememberApps(rows)
	if flags.json() {
		return printJSON(stdout, overviewJSON(rows))
	}
	printApps(stdout, rows)
	printNeeds(stdout, rows)
	return nil
}

// welcome is `moin` before the first deploy: what moin is, and the
// three steps to an app on a server.
func welcome(w io.Writer) {
	fmt.Fprintln(w, style.Mark.Render(build.Name)+"  your app on your server. Backups checked every day.")
	fmt.Fprintln(w)
	steps := [][3]string{
		{"1", "cd my-app", "the app's folder, a git repository"},
		{"2", build.Name + " deploy <server>", "any server you can ssh to, like ssh my-server"},
		{"3", build.Name, "your apps, live, with their backups"},
	}
	for _, step := range steps {
		fmt.Fprintln(w, "  "+style.Muted.Render(step[0])+"  "+command(w, step[1])+strings.Repeat(" ", max(22-len(step[1]), 1))+style.Muted.Render(step[2]))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, style.Muted.Render("All commands: ")+command(w, build.Name+" help")+style.Muted.Render("   Docs: https://moin.run/docs"))
}

// printNeeds says under the table what needs you, one thing each: a
// server that does not answer, an app that does not run, a server open
// to the internet or watched by nobody. The command that settles it
// follows on the next line.
func printNeeds(stdout io.Writer, rows []serverRow) {
	var lines []string
	for _, row := range rows {
		if row.err != nil {
			lines = append(lines, style.Danger.Render(style.SymbolFailed)+" "+commands(stdout, explain(row.err)))
			continue
		}
		for _, app := range row.status.Apps {
			if !app.Running {
				lines = append(lines, needLine(stdout, app.Name+" does not run. Its logs say why.", build.Name+" logs "+app.Name))
			}
		}
		for _, need := range row.status.Needs {
			if need.Soft {
				lines = append(lines, style.Muted.Render(need.Text)+"\n  "+command(stdout, need.Command))
				continue
			}
			lines = append(lines, needLine(stdout, need.Text, need.Command))
		}
		if row.status.PublicSSH {
			lines = append(lines, needLine(stdout, row.server+": port 22 is open to the internet.", build.Name+" secure "+row.server))
		}
		if len(row.status.Watchers) == 0 {
			lines = append(lines, needLine(stdout, row.server+" reports to no watcher. Nobody notices when it is gone.", build.Name+" watcher connect "+row.server+" <url> <token>"))
		}
		if row.status.KnocksToday > 0 {
			lines = append(lines, style.Muted.Render(row.server+": "+human.Knocks(row.status.KnocksToday)))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(stdout)
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
}

// needLine is one thing that needs you, in amber, and the command that
// settles it under it.
func needLine(w io.Writer, text, next string) string {
	return style.Warning.Render("!") + " " + text + "\n  " + command(w, next)
}

// askServers asks every server at once and waits for all, at most
// statusTimeout. A server that does not answer comes back with its error.
func askServers(ctx context.Context, servers []string) []serverRow {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	rows := make([]serverRow, len(servers))
	var wait sync.WaitGroup
	for i, server := range servers {
		wait.Go(func() {
			status, err := quietStatus(ctx, server)
			rows[i] = serverRow{server: server, status: status, err: err}
		})
	}
	wait.Wait()
	return rows
}

// quietStatus is serverStatus without a password prompt and without what
// ssh says on stderr, which would mix into the table.
func quietStatus(ctx context.Context, server string) (daemon.Status, error) {
	var status daemon.Status
	err := quietJSON(ctx, server, []string{"apps"}, &status)
	if ctx.Err() != nil {
		return daemon.Status{}, errors.New("no answer in time")
	}
	return status, err
}

// printApps writes the table of servers and their apps.
func printApps(stdout io.Writer, rows []serverRow) {
	lines := [][]string{}
	for _, row := range rows {
		if row.err != nil {
			lines = append(lines, []string{style.Bold.Render(row.server), "", "", badge(stdout, style.Badge(style.StatusFailed, "unreachable"), "unreachable"), ""})
			continue
		}
		if len(row.status.Apps) == 0 {
			lines = append(lines, []string{style.Bold.Render(row.server), style.Muted.Render("no apps"), "", "", ""})
		}
		for _, app := range row.status.Apps {
			domain := strings.Join(app.Addresses, " ")
			if app.Password {
				domain += "  " + style.Muted.Render("password on")
			}
			deploy := "v" + strconv.Itoa(app.Release)
			if app.Catalog != "" {
				deploy += "  " + style.Muted.Render(app.Catalog+" "+app.Version)
			}
			lines = append(lines, []string{style.Bold.Render(row.server), app.Name, deploy, appState(stdout, app.Running), domain})
		}
	}
	table(stdout, []string{"SERVER", "APP", "DEPLOY", "STATE", "DOMAIN"}, lines)
}

// serverJSON is one server in `moin --json`.
type serverJSON struct {
	Server   string             `json:"server"`
	Version  string             `json:"version,omitempty"`
	Error    string             `json:"error,omitempty"`
	Apps     []daemon.AppStatus `json:"apps"`
	Watchers []string           `json:"watchers"`
	// PublicSSH: port 22 is open to the internet.
	PublicSSH   bool  `json:"public_ssh"`
	KnocksToday int64 `json:"knocks_today"`
}

// overviewJSON is the table of `moin` for scripts.
func overviewJSON(rows []serverRow) []serverJSON {
	list := []serverJSON{}
	for _, row := range rows {
		entry := serverJSON{Server: row.server, Version: row.status.Version, Apps: row.status.Apps, Watchers: row.status.Watchers, PublicSSH: row.status.PublicSSH, KnocksToday: row.status.KnocksToday}
		if row.err != nil {
			entry.Error = explain(row.err)
		}
		if entry.Apps == nil {
			entry.Apps = []daemon.AppStatus{}
		}
		if entry.Watchers == nil {
			entry.Watchers = []string{}
		}
		list = append(list, entry)
	}
	return list
}
