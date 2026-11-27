package handler

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Home is the landing page.
func Home(files fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A changelog that cannot be read hides Built in the open, nothing more.
		releases, err := service.ChangelogReleases(files)
		if err != nil {
			slog.Warn("home: changelog", "error", err)
		}
		render(w, r, pages.Home(pages.NewHomeOpen(releases)))
	}
}

// NotFound answers every address the site does not have.
func NotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.NotFound())
}
