package components

import (
	"strings"

	"github.com/axadrn/moin/internal/build"
)

// AssetURL is the path of a file under /assets/ with the version of this
// build in it: /assets/css/x.css is /assets/<version>/css/x.css. A new
// release or deploy gives every file a new URL, so a browser or a CDN never
// keeps an old one, and a relative url() in CSS keeps the version. The site
// serves these paths with routes.go's versioned.
func AssetURL(path string) string {
	rest, ok := strings.CutPrefix(path, "/assets/")
	if !ok {
		return path
	}
	return "/assets/" + build.Version + "/" + rest
}
