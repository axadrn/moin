package handler

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Roadmap shows what moin does today and what comes next, read from
// docs/roadmap.md on every request.
func Roadmap(files fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		roadmap, err := service.ReadRoadmap(files)
		if err != nil {
			slog.Error("roadmap failed", "error", err)
			w.WriteHeader(http.StatusNotFound)
			render(w, r, pages.NotFound())
			return
		}
		render(w, r, pages.Roadmap(roadmap))
	}
}
