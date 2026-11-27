package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// You name the app, moin finds its server: from any terminal tab. It
// keeps which servers run which app in ~/.config/moin/apps.json, from
// every deploy and every `moin`, and asks all your servers when the file
// does not know or is wrong. The same app on two servers needs -s.

// errNoApp is what a server answers when it does not run the app.
var errNoApp = errors.New("the server does not run the app")

// noAppCode is the exit code of `moin here` for errNoApp.
const noAppCode = 98

// withServer adds -s and --server, which name the server outright.
func (f *flagSet) withServer() *flagSet {
	f.serverFlag = new(string)
	f.StringVar(f.serverFlag, "s", "", "the server, when the app runs on more than one")
	f.StringVar(f.serverFlag, "server", "", "the server, when the app runs on more than one")
	return f
}

// appArgs reads the app and the other arguments of an app command: the
// app comes first and may be left out in its folder. others is how many
// arguments come after it, like the domain of `domains add`.
func appArgs(flags *flagSet, others int) (app string, rest []string, err error) {
	switch flags.NArg() {
	case others + 1:
		app, rest = flags.Arg(0), flags.Args()[1:]
		if !protocol.ValidAppName(app) {
			return "", nil, fmt.Errorf("%q is not an app name: lowercase letters, digits and dashes", app)
		}
		return app, rest, nil
	case others:
		app, err = folderApp()
		return app, flags.Args(), err
	}
	return "", nil, flags.wrong()
}

// onApp runs do on the server that runs app: the one -s names, else the
// one apps.json knows, else the one that answers it runs the app.
func onApp(ctx context.Context, flags *flagSet, app string, do func(server string) error) error {
	if flags.serverFlag != nil && *flags.serverFlag != "" {
		return do(*flags.serverFlag)
	}
	known := knownServers(app)
	if len(known) > 1 {
		return twoServers(app, known)
	}
	if len(known) == 1 {
		err := do(known[0])
		if !errors.Is(err, errNoApp) {
			return err
		}
	}
	found, err := findServers(ctx, app)
	if err != nil {
		return err
	}
	switch len(found) {
	case 0:
		return noServer(app)
	case 1:
		return do(found[0])
	}
	return twoServers(app, found)
}

// findServers asks every server of your list which apps it runs, keeps
// the answers, and returns the servers that run app.
func findServers(ctx context.Context, app string) ([]string, error) {
	servers, err := loadServers()
	if err != nil {
		return nil, err
	}
	rows := askServers(ctx, servers)
	rememberApps(rows)
	var found []string
	for _, row := range rows {
		for _, each := range row.status.Apps {
			if each.Name == app {
				found = append(found, row.server)
			}
		}
	}
	return found, nil
}

// twoServers asks you to pick one of the servers that run app.
func twoServers(app string, servers []string) error {
	return fmt.Errorf("%s runs on %s. Name one with -s, like `%s … -s %s`", app, strings.Join(servers, " and "), build.Name, servers[0])
}

// noServer says no server of yours runs app.
func noServer(app string) error {
	return fmt.Errorf("none of your servers runs %s. Deploy it with `%s deploy <server>`, or add the server it runs on with `%s add <server>`", app, build.Name, build.Name)
}

// appsFile keeps which servers run which app.
func appsFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "apps.json"), nil
}

// loadApps reads apps.json: app name to servers. Empty when it is not
// there or cannot be read: moin asks the servers then.
func loadApps() map[string][]string {
	apps := map[string][]string{}
	file, err := appsFile()
	if err != nil {
		return apps
	}
	body, err := os.ReadFile(file)
	if err == nil {
		_ = json.Unmarshal(body, &apps)
	}
	return apps
}

// saveApps writes apps.json.
func saveApps(apps map[string][]string) error {
	file, err := appsFile()
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(apps, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return os.WriteFile(file, body, 0o600)
}

// knownServers are the servers apps.json says run app.
func knownServers(app string) []string {
	return loadApps()[app]
}

// rememberApp notes that server runs app, after a deploy.
func rememberApp(server, app string) {
	apps := loadApps()
	if !slices.Contains(apps[app], server) {
		apps[app] = append(apps[app], server)
		_ = saveApps(apps)
	}
}

// rememberApps keeps what the servers that answered run. A server that
// did not answer keeps what was known of it.
func rememberApps(rows []serverRow) {
	apps := loadApps()
	for _, row := range rows {
		if row.err == nil {
			updateApps(apps, row.server, row.status.Apps)
		}
	}
	_ = saveApps(apps)
}

// updateApps sets in apps what server runs now: it drops the server from
// the apps it no longer runs and adds it to the ones it does.
func updateApps(apps map[string][]string, server string, running []daemon.AppStatus) {
	for app, servers := range apps {
		servers = slices.DeleteFunc(servers, func(s string) bool { return s == server })
		if len(servers) == 0 {
			delete(apps, app)
		} else {
			apps[app] = servers
		}
	}
	for _, each := range running {
		apps[each.Name] = append(apps[each.Name], server)
	}
}

// folderApp is the name of this folder, as the app it holds.
func folderApp() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	name := strings.ToLower(filepath.Base(dir))
	if !protocol.ValidAppName(name) {
		return "", fmt.Errorf("name the app, like `%s logs shop`: this folder's name %q is not an app name", build.Name, name)
	}
	return name, nil
}
