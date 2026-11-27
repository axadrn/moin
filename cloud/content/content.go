// Package content holds the texts of the site: the blog, docs, legal
// pages, the changelog and llms.txt. The catalog is its own package, catalog/ at the
// root, which the daemon has built in too.
package content

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed all:blog all:docs all:legal changelog.md llms.txt
var files embed.FS

// FS is the texts: from disk in development, so an edit shows on reload, else
// from the binary. The site runs in cloud/.
func FS(development bool) fs.FS {
	if development {
		return os.DirFS("content")
	}
	return files
}
