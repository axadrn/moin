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
	// App names the app on the server: its folder, and the Compose project
	// its containers carry, such as my-app-web-1.
	App    string `json:"app"`
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
