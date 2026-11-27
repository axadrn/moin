package protocol

import (
	"strings"
	"time"
)

// Secrets live only on the server, where the CLI puts them over SSH:
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
// for them; the CLI shows what it says.
type AppEnv struct {
	Keys      []string  `json:"keys"`
	ChangedAt time.Time `json:"changed_at"`
}

// SecretSet keeps the secret Value under Name in the server's server.json;
// an empty Value removes it. Names say what a secret is for:
//
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

// SecretOf splits a secret's name into its kind, like registry, and what
// it is for, like the registry's host. A name without a part, like
// tailscale, is its own kind.
func SecretOf(name string) (kind, of string) {
	kind, of, _ = strings.Cut(name, "/")
	return kind, of
}
