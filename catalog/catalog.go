// Package catalog holds the apps of moin's catalog, one folder each:
// app.md with what the app is, compose.yml with how it runs, and its logo.
//
// The hub shows them and the daemon runs them, and both have them built in.
// The hub names an app and its version; the daemon takes the compose file
// from its own signed release, never from the hub.
package catalog

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

//go:embed */app.md */compose.yml */logo.*
var files embed.FS

// Files are the folders of the catalog, one per app.
var Files fs.FS = files

// slugPattern is the name of an app's folder.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Compose is the compose file of the catalog app slug, false when the
// catalog has no such app.
func Compose(slug string) ([]byte, bool) {
	if !slugPattern.MatchString(slug) {
		return nil, false
	}
	body, err := fs.ReadFile(files, slug+"/compose.yml")
	return body, err == nil
}

// VersionVar is the env var that holds the version: compose.yml uses it as
// the image tag.
const VersionVar = "VERSION"

// URLVar is the app's own address, https://<its domain>, which moin sets on
// install for an app whose compose.yml uses it: links, redirects and
// cookies of such an app need it, so it needs a domain.
const URLVar = "APP_URL"

// DomainVar is the app's domain alone, without https://, for an app that
// asks for its host name rather than its address.
const DomainVar = "APP_DOMAIN"

// variablePattern is a ${NAME} in a compose file, with what follows the
// name: a default or an error text.
var variablePattern = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)(:?[-?][^}]*)?\}`)

// Secrets lists the ${NAME}s compose uses, except the version, the URL
// vars and those with a default, sorted. The server makes a value for each
// one the app does not have yet.
func Secrets(compose []byte) []string {
	var names []string
	for _, match := range variablePattern.FindAllStringSubmatch(string(compose), -1) {
		name, rest := match[1], match[2]
		if name == VersionVar || name == URLVar || name == DomainVar || strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, ":-") {
			continue
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// NewSecret is 32 random bytes as URL-safe text: fine in a database URL,
// a header or YAML. Some apps want a key in hex: a name that ends in _HEX
// gets 32 bytes as 64 hex digits, one that ends in _HEX16 16 bytes as 32.
func NewSecret(name string) string {
	size, encode := 32, base64.RawURLEncoding.EncodeToString
	switch {
	case strings.HasSuffix(name, "_HEX16"):
		size, encode = 16, hex.EncodeToString
	case strings.HasSuffix(name, "_HEX"):
		encode = hex.EncodeToString
	}
	buffer := make([]byte, size)
	_, _ = rand.Read(buffer)
	return encode(buffer)
}
