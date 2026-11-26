package handler

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
)

// Confirm it's you: what needs the person right now (adding a passkey,
// approving a connection, more rights) asks them to confirm in place, with
// their passkey or with GitHub, and comes back to the page. Nobody signs
// out; the session notes when (service.PasskeyService.Present).

// ConfirmPasskeyOptions starts the passkey ceremony of ConfirmWithPasskey.
func (h *authHandler) ConfirmPasskeyOptions(w http.ResponseWriter, r *http.Request) {
	options, ceremony, err := h.passkeys.BeginConfirm(ctxkeys.Session(r.Context()))
	if err != nil {
		passkeyFailed(w, err)
		return
	}
	setPasskeyCeremony(w, r, ceremony)
	api.JSON(w, http.StatusOK, options)
}

// ConfirmWithPasskey checks the passkey's answer and goes back to next. The
// page there says whether it counts.
func (h *authHandler) ConfirmWithPasskey(w http.ResponseWriter, r *http.Request) {
	err := h.passkeys.Confirm(service.Presence{
		Session:  ctxkeys.Session(r.Context()),
		Ceremony: takePasskeyCeremony(w, r),
		Answer:   []byte(r.FormValue("passkey")),
	})
	if err != nil {
		slog.Warn("confirming with a passkey failed", "error", err)
	}
	redirect(w, r, localPath(r.FormValue("next")), http.StatusSeeOther)
}

// ConfirmWithGitHub sends the signed-in person to GitHub, to confirm with
// the GitHub account they sign in with, and back to next (GitHubCallback).
func (h *authHandler) ConfirmWithGitHub(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	next := localPath(r.FormValue("next"))
	if session := ctxkeys.Session(r.Context()); session == nil || session.IsImpersonating() || user.GitHubID == nil {
		redirect(w, r, next, http.StatusSeeOther)
		return
	}
	h.startGitHub(w, r, githubTrip{userID: user.ID, next: next}, http.StatusSeeOther)
}

// localPath is next when it is a page of this hub, else /app: a way back
// never leads to another site.
func localPath(next string) string {
	target, err := url.Parse(next)
	if err != nil || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n\t") || target.Scheme != "" || target.Host != "" {
		return "/app"
	}
	return next
}
