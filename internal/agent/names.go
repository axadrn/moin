package agent

import (
	"strings"
	"sync"

	"github.com/axadrn/moin/internal/build"
)

// The names the agent gives things on a server, all from build.Name. The
// legacy ones are what an agent of the old name left behind; the migration
// moves them over, and they go after build.LegacyUntil.
const (
	ourName = build.Name
	legacy  = build.LegacyName

	proxyName    = ourName + "-traefik"
	proxyNetwork = ourName + "-proxy"
	tunnelName   = ourName + "-cloudflared"
	// firewallTable is the nftables table of the firewall.
	firewallTable = ourName
	walgSlot      = ourName + "_walg"

	labelMode       = ourName + ".mode"
	labelSetup      = ourName + ".setup"
	labelToken      = ourName + ".token"
	labelWalg       = ourName + ".walg"
	labelWalgTarget = ourName + ".walg.target"
	labelDrill      = ourName + ".drill"

	legacyProxyName    = legacy + "-traefik"
	legacyProxyNetwork = legacy + "-proxy"
	legacyTunnelName   = legacy + "-cloudflared"
	legacyFirewall     = legacy
	legacyWalgSlot     = legacy + "_walg"
	legacyLabelWalg    = legacy + ".walg"
	legacyStateDir     = "/var/lib/" + legacy
	legacyRulesFile    = "/etc/" + legacy + "/rules"
	legacyUnit         = legacy + "-agent"
	legacyBinary       = "/usr/local/bin/" + legacy
)

// legacyApps are the apps still on their old Compose project, because
// their migration failed; the agent keeps running them there and tries
// again at its next start.
var legacyApps sync.Map

// project is the Compose project of an app.
func project(app string) string {
	if _, ok := legacyApps.Load(app); ok {
		return legacyProject(app)
	}
	return ourName + "-" + app
}

func legacyProject(app string) string { return legacy + "-" + app }

// appOfProject is the app a Compose project belongs to.
func appOfProject(project string) (string, bool) {
	for _, prefix := range []string{ourName + "-", legacy + "-"} {
		if app, ok := strings.CutPrefix(project, prefix); ok {
			return app, true
		}
	}
	return "", false
}

// tempName starts the names of temporary files and folders.
func tempName(what string) string { return ourName + "-" + what }
