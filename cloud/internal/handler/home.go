package handler

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/cloud/internal/tweets"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// Home is the landing page. knocks are the knocks of the site's own
// server, nil when there are none; numbers what moin counted, nil while
// too few.
func Home(files fs.FS, knocks func() *pages.HomeKnocks, numbers func() *pages.HomeNumbers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Posts that cannot be read hide their section, nothing more.
		posts, err := tweets.Load(files)
		if err != nil {
			slog.Warn("home: tweets", "error", err)
		}
		render(w, r, pages.Home(posts, knocks(), numbers()))
	}
}

// NotFound answers every address the site does not have.
func NotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.NotFound())
}
