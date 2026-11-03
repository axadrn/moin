package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// Changelog shows what shipped, newest first. It reads the file on every
// request, so a new release shows without a restart in development.
func Changelog(contentDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		html, err := service.Changelog(contentDir)
		if err != nil {
			slog.Error("changelog failed", "error", err)
			w.WriteHeader(http.StatusNotFound)
			render(w, r, pages.NotFound())
			return
		}
		render(w, r, pages.Changelog(html))
	}
}
