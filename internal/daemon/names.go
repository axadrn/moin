package daemon

import "github.com/axadrn/moin/internal/build"

// The names the daemon gives things on a server, all from build.Name.
const (
	ourName = build.Name

	proxyName    = ourName + "-traefik"
	proxyNetwork = ourName + "-proxy"
	tunnelName   = ourName + "-cloudflared"
	// firewallTable is the nftables table of the firewall.
	firewallTable = ourName

	labelMode  = ourName + ".mode"
	labelSetup = ourName + ".setup"
	labelToken = ourName + ".token"
	// labelApp names the app of a container: its folder.
	labelApp = ourName + ".app"
)

// project is the Compose project an app ran as before renames, and the
// start of the names of its images and routers. The project it runs as
// now is apps.projectOf.
func project(app string) string { return ourName + "-" + app }

// tempName starts the names of temporary files and folders.
func tempName(what string) string { return ourName + "-" + what }
