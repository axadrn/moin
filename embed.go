// Package moin holds what the repo root gives the programs in it.
package moin

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed ui
var uiFiles embed.FS

// UI is the ui folder: from disk in development, so an edit shows on reload,
// else from the binary. The hub runs in hub/.
func UI(development bool) fs.FS {
	if development {
		return os.DirFS("../ui")
	}
	files, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	return files
}
