package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

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
	h.render(w, r)
}

func (h *WaitlistHandler) LetIn(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	err := h.beta.LetIn(email, r.FormValue("forever_free") == "true")
	if errors.Is(err, service.ErrInvalidEmail) {
		fieldError(w, "Not let in", "let-in-email", "Enter a valid email address.")
		return
	}
	if err != nil {
		toastFailed(w, r, email+" not let in", errorMessage(err))
		return
	}
	if !isAction(r) {
		redirect(w, r, "/admin/waitlist", http.StatusSeeOther)
		return
	}
	h.render(w, r)
	toastDone(w, r, email+" is in", "They got the sign-in email.")
}

func (h *WaitlistHandler) render(w http.ResponseWriter, r *http.Request) {
	entries, err := h.beta.Waitlist()
	if err != nil {
		slog.Error("waitlist failed", "error", err)
		http.Error(w, "waitlist failed", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.AdminWaitlist(entries))
}
