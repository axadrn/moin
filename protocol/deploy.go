package protocol

import (
	"net/netip"
	"slices"
	"strings"
	"time"
)

// DeployStart is one deploy. Number counts per app. A rollback carries
// FromNumber and no source: the daemon starts that release's images again.
// Image replaces the build of the main service with an image built
// elsewhere, such as in CI; without a source the daemon takes the compose
// file of the running release. Without source, image and FromNumber the
// deploy applies env and domains to the running release, without a build.
// The env never travels with a deploy: every release runs with the app's
// app.env on the server as it is then (EnvSet), and a login for Image comes
// from the server too (SecretSet).
type DeployStart struct {
	DeployID string `json:"deploy_id"`
	// App names the app on the server in every message: its folder, the
	// name it was created with, which a rename never changes. Name is its
	// name now, which its Compose project and containers carry, such as
	// my-app-web-1; a hub from before renames sends none, and the daemon
	// takes App.
	App  string `json:"app"`
	Name string `json:"name,omitempty"`
	// AppID is the app's ID in the hub. Its backups live under
	// it, so an app removed and created again with the same name starts
	// with an empty backup folder.
	AppID  string `json:"app_id,omitempty"`
	Number int    `json:"number"`
	// SourceURL is a GitHub tarball of one commit; the daemon downloads
	// nothing else. SourceAuthorization is sent with the download, a GitHub
	// token; SourceStripTop drops the one folder GitHub wraps a repo in.
	// The hub never holds the source itself.
	SourceURL           string `json:"source_url,omitempty"`
	SourceAuthorization string `json:"source_authorization,omitempty"`
	SourceStripTop      bool   `json:"source_strip_top,omitempty"`
	// SourceNumber builds the source the server kept of release n again,
	// for a redeploy; nothing is downloaded.
	SourceNumber int `json:"source_number,omitempty"`
	// Catalog deploys the catalog app of this name, such as umami: the
	// daemon takes its compose file from the catalog built into it, never
	// from the hub. Daemons before v1.0.0-beta.35 do not know it.
	Catalog    string   `json:"catalog,omitempty"`
	FromNumber int      `json:"from_number,omitempty"`
	Image      string   `json:"image,omitempty"`
	Domains    *Domains `json:"routing,omitempty"`
}

// Domains is how visitors reach the app, as set in web, CLI, TUI or MCP.
// Domains, Canonical and Services are the domains entered in moin. If
// compose.yml has domains, exactly those count; otherwise these. Never a
// mix. For protect and instant, the x-moin block wins for every field it
// sets. Protect is an htpasswd line ("user:bcrypt hash") when password
// protection is on. DeployStart carries it under the JSON key routing,
// which daemons of protocol version 1 read.
type Domains struct {
	Domains   []string `json:"domains,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Protect   string   `json:"protect,omitempty"`
	// Services are the domains of the app's other services, by service.
	Services map[string][]string `json:"services,omitempty"`
	// Instant is the provider of the app's instant address, such as
	// sslip.io, "" for none; InstantIP the server's public IPv4 it names.
	// InstantProtect is the htpasswd line in front of it, "" when open.
	Instant        string `json:"instant,omitempty"`
	InstantIP      string `json:"instant_ip,omitempty"`
	InstantProtect string `json:"instant_protect,omitempty"`
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

// RegistryHost is the registry of an image reference: its first part when
// that looks like a host, else Docker Hub.
func RegistryHost(image string) string {
	first, _, found := strings.Cut(image, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
}

// ComposeDomains are the domains x-moin.domains in the compose.yml of the
// release that runs sets. Domains and Canonical are the main service's,
// Services the other services' domains by service. All empty when
// compose.yml sets none: then the domains entered in moin count.
type ComposeDomains struct {
	Domains   []string            `json:"domains,omitempty"`
	Canonical string              `json:"canonical,omitempty"`
	Services  map[string][]string `json:"services,omitempty"`
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
	// Before is when the daemon took the copies just before the new
	// release started over an older one, the time in their IDs: the hub
	// names them "Before v12".
	Before *time.Time `json:"before,omitempty"`
	// Domains are the domains of the compose.yml of the release that runs
	// after the deploy: the new one, or the one before it when the new one
	// did not come up. nil when no release runs. The hub keeps them as the
	// app's domains.
	Domains *ComposeDomains `json:"domains,omitempty"`
}
