package middleware

import (
	"fmt"
	"github.com/axadrn/moin/controlplane/internal/api"
	"github.com/axadrn/moin/controlplane/internal/ui/kit"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
	"log/slog"
	"net/http"
)

func renderMiddlewareError(w http.ResponseWriter, r *http.Request, status int, title, description string) {
	if isAPIRequest(r) {
		api.Error(w, status, api.CodeForStatus(status), description)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		// A line under what sent it, and the page stays as it was (DESIGN.md,
		// Feedback).
		w.Header().Set("HX-Reswap", "none")
		if _, err := fmt.Fprint(w, `<div hx-swap-oob="beforeend:#feedback">`); err != nil {
			slog.Error("render middleware error feedback wrapper failed", "error", err)
			return
		}
		if err := kit.Feedback("failed", title, description).Render(r.Context(), w); err != nil {
			slog.Error("render middleware error feedback failed", "error", err)
			return
		}
		if _, err := fmt.Fprint(w, `</div>`); err != nil {
			slog.Error("render middleware error feedback wrapper failed", "error", err)
		}
		return
	}
	w.WriteHeader(status)
	if err := pages.ErrorPage(status, title, description).Render(r.Context(), w); err != nil {
		slog.Error("render middleware error page failed", "error", err)
	}
}
