// Package assets holds the files the pages load: styles, scripts, fonts,
// icons and images.
package assets

import "embed"

//go:embed css js fonts icons img
var Files embed.FS
