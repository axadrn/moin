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

// Domains is how visitors reach the app, as set in web, CLI, TUI or MCP:
// the only place they are set. Protect is an htpasswd line ("user:bcrypt
// hash") when the app's password is on: every address of the app asks for
// it, its domains, those of its other services and its instant address.
// DeployStart carries it under the JSON key routing, which daemons of
// protocol version 1 read.
type Domains struct {
	Domains   []string `json:"domains,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Protect   string   `json:"protect,omitempty"`
	// Services are the domains of the app's other services, by service.
	Services map[string][]string `json:"services,omitempty"`
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

// RegistryHost is the registry of an image reference: its first part when
// that looks like a host, else Docker Hub.
func RegistryHost(image string) string {
	first, _, found := strings.Cut(image, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return first
	}
	return "docker.io"
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
	// Env names the env keys the app has after the deploy, which may have
	// made the secrets of a catalog app: never a value.
	Env *AppEnv `json:"env,omitempty"`
	// ComposeFile is the compose file of a deploy that worked, YAML:
	// everything the daemon runs for the app, written to run anywhere,
	// with each ${NAME} as written and no value of the env. Daemons before
	// it send none.
	ComposeFile string `json:"compose_file,omitempty"`
}
