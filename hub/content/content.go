// Package content holds the texts of the site: docs, legal pages, the catalog,
// the changelog and llms.txt.
package content

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed all:catalog all:docs all:legal changelog.md llms.txt
var files embed.FS

// FS is the texts: from disk in development, so an edit shows on reload, else
// from the binary. The hub runs in hub/.
func FS(development bool) fs.FS {
	if development {
		return os.DirFS("content")
	}
	return files
}
