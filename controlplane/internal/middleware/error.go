package middleware

import (
	"fmt"
	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/ui/components/toast"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
	"log/slog"
	"net/http"
)

func renderMiddlewareError(w http.ResponseWriter, r *http.Request, status int, title, description string) {
	if isAPIRequest(r) {
		api.Error(w, status, api.CodeForStatus(status), description)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		if _, err := fmt.Fprint(w, `<div hx-swap-oob="beforeend:#toast-container">`); err != nil {
			slog.Error("render middleware error toast wrapper failed", "error", err)
			return
		}
		if err := toast.Toast(toast.Props{Title: title, Description: description, Type: toast.TypeError}).Render(r.Context(), w); err != nil {
			slog.Error("render middleware error toast failed", "error", err)
			return
		}
		if _, err := fmt.Fprint(w, `</div>`); err != nil {
			slog.Error("render middleware error toast wrapper failed", "error", err)
		}
		return
	}
	w.WriteHeader(status)
	if err := pages.ErrorPage(status, title, description).Render(r.Context(), w); err != nil {
		slog.Error("render middleware error page failed", "error", err)
	}
}
