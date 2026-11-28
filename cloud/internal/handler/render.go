package handler

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// isAction says htmx sent the request, such as the search palette.
func isAction(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		renderError(w, "render failed", err)
	}
}

func renderError(w http.ResponseWriter, message string, err error) {
	slog.Error(message, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
