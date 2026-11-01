package handler

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// WaitlistHandler shows the waitlist of the private beta to admins and lets
// people in.
type WaitlistHandler struct {
	beta *service.BetaService
}

func NewWaitlistHandler(beta *service.BetaService) *WaitlistHandler {
	return &WaitlistHandler{beta: beta}
}

func (h *WaitlistHandler) Page(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, r.URL.Query().Get("notice"))
}

func (h *WaitlistHandler) LetIn(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	err := h.beta.LetIn(email, r.FormValue("forever_free") == "true")
	if err != nil {
		slog.Error("let in failed", "error", err)
		h.render(w, r, "Could not let "+email+" in: "+err.Error())
		return
	}
	redirect(w, r, "/admin/waitlist?notice="+url.QueryEscape(email+" is in and got the sign-in email."), http.StatusSeeOther)
}

func (h *WaitlistHandler) render(w http.ResponseWriter, r *http.Request, notice string) {
	entries, err := h.beta.Waitlist()
	if err != nil {
		slog.Error("waitlist failed", "error", err)
		http.Error(w, "waitlist failed", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.AdminWaitlist(entries, notice))
}
