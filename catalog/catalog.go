// Package catalog holds the apps of moin's catalog, one folder each:
// app.md with what the app is, compose.yml with how it runs, and its logo.
//
// The hub shows them and the daemon runs them, and both have them built in.
// The hub names an app and its version; the daemon takes the compose file
// from its own signed release, never from the hub.
package catalog

import (
	"embed"
	"io/fs"
	"regexp"
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
