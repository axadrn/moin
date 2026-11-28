// Package assets holds the files the pages load: styles, scripts, fonts,
// icons and images.
package assets

import "embed"

//go:embed brand css js fonts icons img manifest.json
var Files embed.FS
