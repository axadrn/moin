package protocol

import (
	"net/netip"
	"slices"
	"strings"
)

// DeployStart is one deploy. Number counts per app. A rollback carries
// FromNumber and no source: the daemon starts that release's images again.
// Image replaces the build of the main service with an image built
// elsewhere, such as in CI; without a source the daemon takes the compose
// file of the running release. Without source, image and FromNumber the
// deploy applies env and domains to the running release, without a build.
// The env never travels with a deploy: every release runs with the app's
// app.env on the server as it is then (EnvSet).
type DeployStart struct {
	DeployID string `json:"deploy_id"`
	// App names the app on the server in every message: its folder, the
	// name it was created with, which a rename never changes. Name is its
	// name now, which its Compose project and containers carry, such as
	// my-app-web-1; without one the daemon takes App.
	App    string `json:"app"`
	Name   string `json:"name,omitempty"`
	Number int    `json:"number"`
	// SourceFile is a gzipped tar of the source on the server itself, which
	// the CLI sent over SSH.
	SourceFile string `json:"-"`
	// Catalog deploys the catalog app of this name, such as umami: the
	// daemon takes its compose file from the catalog built into it.
	Catalog    string   `json:"catalog,omitempty"`
	FromNumber int      `json:"from_number,omitempty"`
	Image      string   `json:"image,omitempty"`
	Domains    *Domains `json:"routing,omitempty"`
	// NoCopy deploys without the copy of the databases just before the new
	// release starts: the way on when that copy fails.
	NoCopy bool `json:"no_copy,omitempty"`
}

// Domains is how visitors reach the app, as `moin domains` set them on
// the server, the only place they are set.
type Domains struct {
	Domains []string `json:"domains,omitempty"`
	// Instant is the provider of the app's instant address, such as
	// sslip.io, "" for none; InstantIP the server's public IPv4 it names.
	Instant   string `json:"instant,omitempty"`
	InstantIP string `json:"instant_ip,omitempty"`
}

// InstantProviders are the services that name an IP without any setup:
// <name>.<ip with dashes>.<provider> resolves to the IP.
var InstantProviders = []string{"sslip.io", "nip.io"}

// InstantHost is the instant address of an app on a server with the public
// IPv4 ip, such as my-app.203-0-113-7.sslip.io, or "" when the provider is
// none of InstantProviders or the IP is not IPv4.
func InstantHost(app, ip, provider string) string {
	address, err := netip.ParseAddr(ip)
	if err != nil || !address.Is4() || !slices.Contains(InstantProviders, provider) || app == "" {
		return ""
	}
	return app + "." + strings.ReplaceAll(address.String(), ".", "-") + "." + provider
}

// DeployLog is one line of build or start output.
type DeployLog struct {
	DeployID string `json:"deploy_id"`
	Line     string `json:"line"`
}

// DeployDone reports how a deploy ended and what the daemon found.
type DeployDone struct {
	DeployID string   `json:"deploy_id"`
	OK       bool     `json:"ok"`
	Error    string   `json:"error,omitempty"`
	Detected Detected `json:"detected"`
	// Env names the env keys the app has after the deploy, which may have
	// made the secrets of a catalog app: never a value.
	Env *AppEnv `json:"env,omitempty"`
	// ComposeFile is the compose file of a deploy that worked, YAML:
	// everything the daemon runs for the app, written to run anywhere,
	// without the instant address and with no value of the env. image,
	// ports, expose, volumes, user, the env PORT and the paths of a build
	// are as the deploy filled them in; every other ${NAME} stays as
	// written. Daemons before it send none.
	ComposeFile string `json:"compose_file,omitempty"`
}
