package protocol

import (
	"strings"
	"time"
)

// Secrets live only on the server. The hub forwards a value once, keeps
// its name and when it changed, and forgets it:
//
//	<state>/apps/<app>/app.env  the env values of an app, 0600 (EnvSet)
//	<state>/server.json         the secrets of the server itself, 0600 (SecretSet)

// EnvSet sets the env values of Set and removes the keys of Remove in the
// app's app.env. Every compose up of the app reads that one file, so a
// change reaches the containers with the next deploy, restart or start.
// The daemon answers with TypeEnvResult: the keys the file has now.
type EnvSet struct {
	App    string            `json:"app"`
	Set    map[string]string `json:"set,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

// AppEnv names the env keys an app has on the server, from its app.env,
// and when that file last changed: never a value. The server is the truth
// for them; the hub shows what it reports.
type AppEnv struct {
	Keys      []string  `json:"keys"`
	ChangedAt time.Time `json:"changed_at"`
}

// SecretSet keeps the secret Value under Name in the server's server.json;
// an empty Value removes it. Names say what a secret is for:
//
//	BucketSecret(id)      the secret key of a bucket
//	RegistrySecret(host)  a registry login, as <username>:<password>
//	SecretTailscale       the auth key that joins the server to a tailnet
//	SecretTunnel          the token of the server's Cloudflare Tunnel
type SecretSet struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// The names of the secrets every server may keep.
const (
	SecretTailscale = "tailscale"
	SecretTunnel    = "tunnel"
)

// RegistrySecret names the login for the registry host, such as ghcr.io.
func RegistrySecret(host string) string { return "registry/" + host }

// SecretOf splits a secret's name into its kind, bucket or registry, and
// what it is for: the bucket's ID or the registry's host. A name without
// a part, like tailscale, is its own kind.
func SecretOf(name string) (kind, of string) {
	kind, of, _ = strings.Cut(name, "/")
	return kind, of
}
