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
type EnvSet struct {
	App    string            `json:"app"`
	Set    map[string]string `json:"set,omitempty"`
	Remove []string          `json:"remove,omitempty"`
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

// BucketSecret names the secret key of the bucket with the hub's ID id.
func BucketSecret(id string) string { return "bucket/" + id }

// RegistrySecret names the login for the registry host, such as ghcr.io.
func RegistrySecret(host string) string { return "registry/" + host }

// SecretOf splits a secret's name into its kind, bucket or registry, and
// what it is for: the bucket's ID or the registry's host. A name without
// a part, like tailscale, is its own kind.
func SecretOf(name string) (kind, of string) {
	kind, of, _ = strings.Cut(name, "/")
	return kind, of
}

// BucketSet has the daemon write a test file to Bucket with SecretKey, or
// with the key it keeps when SecretKey is empty, and keep the key under
// BucketSecret(Bucket.ID) when the write works.
type BucketSet struct {
	Bucket    Bucket `json:"bucket"`
	SecretKey string `json:"secret_key,omitempty"`
}

// BackupLink asks for a link to download the copy Copy of the database
// Database of App, signed by the daemon with the bucket's key, valid
// BackupLinkLife. Name starts the file's name, the app's name in moin.
type BackupLink struct {
	App      string `json:"app"`
	Database string `json:"database"`
	Copy     string `json:"copy"`
	Name     string `json:"name"`
}

// BackupLinkLife is how long a download link works.
const BackupLinkLife = 15 * time.Minute

// BackupLinkResult is the signed link.
type BackupLinkResult struct {
	Link string `json:"link"`
}
