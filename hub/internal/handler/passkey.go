package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Passkeys go through two requests from passkey.js: the options, which
// start a ceremony, and the browser's answer. The ceremony's id travels in
// a cookie between them. Both answer passkey.js: JSON options, the JSON
// location to go to on success, or the reason in plain text.

// passkeyCeremonyCookie carries the id of a passkey ceremony from its
// options to the answer, for this site only and never to scripts.
const passkeyCeremonyCookie = "passkey_ceremony"

// maxPasskeyAnswer is the most a browser's answer may weigh.
const maxPasskeyAnswer = 64 << 10

// PasskeyOptions starts a sign-in with a passkey.
func (h *authHandler) PasskeyOptions(w http.ResponseWriter, r *http.Request) {
	options, ceremony, err := h.passkeys.BeginSignIn()
	if err != nil {
		passkeyFailed(w, err)
		return
	}
	setPasskeyCeremony(w, r, ceremony)
	api.JSON(w, http.StatusOK, options)
}

// SignInWithPasskey takes the browser's answer, starts a session and says
// where to go, like every sign-in.
func (h *authHandler) SignInWithPasskey(w http.ResponseWriter, r *http.Request) {
	user, err := h.passkeys.SignIn(takePasskeyCeremony(w, r), http.MaxBytesReader(w, r.Body, maxPasskeyAnswer))
	if err != nil {
		passkeyFailed(w, err)
		return
	}
	if _, err := h.authService.StartSession(w, r, user.ID); err != nil {
		passkeyFailed(w, err)
		return
	}
	slog.Info("user signed in with a passkey", "user_id", user.ID)
	api.JSON(w, http.StatusOK, passkeyDone{Location: h.afterAuth(w, r, user)})
}

// PasskeyOptions starts adding a passkey to the signed-in user.
func (h *SettingsHandler) PasskeyOptions(w http.ResponseWriter, r *http.Request) {
	options, ceremony, err := h.passkeys.BeginAdd(ctxkeys.Session(r.Context()))
	if err != nil {
		passkeyFailed(w, err)
		return
	}
	setPasskeyCeremony(w, r, ceremony)
	api.JSON(w, http.StatusOK, options)
}

// AddPasskey takes the browser's answer, stores the passkey and says to
// go back to the security settings, which list it and ask for its name.
func (h *SettingsHandler) AddPasskey(w http.ResponseWriter, r *http.Request) {
	passkey, err := h.passkeys.Add(ctxkeys.Session(r.Context()), takePasskeyCeremony(w, r), http.MaxBytesReader(w, r.Body, maxPasskeyAnswer))
	if err != nil {
		passkeyFailed(w, err)
		return
	}
	api.JSON(w, http.StatusOK, passkeyDone{Location: securityPath(r) + "&passkey=" + url.QueryEscape(passkey.ID)})
}

// RenamePasskey names a passkey of the signed-in user and answers the
// passkeys as they are now, without the name form.
func (h *SettingsHandler) RenamePasskey(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	_, err := h.passkeys.Rename(ctxkeys.Session(r.Context()), r.PathValue("id"), r.FormValue("name"))
	switch {
	case errors.Is(err, repository.ErrPasskeyNotFound):
		answerFailed(w, r, "Passkey not found", "It was removed already.")
		return
	case err != nil:
		failed(w, r, "Name not saved", err, map[string]string{"name": "passkey-name"})
		return
	}
	passkeys, err := h.passkeys.List(user.ID)
	if err != nil {
		slog.Error("failed to list passkeys", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Passkeys not refreshed", "Something went wrong. Please try again.")
		return
	}
	w.Header().Set("HX-Replace-Url", securityPath(r))
	answerDone(w, r, "Name saved", "")
	render(w, r, pages.SettingsPasskeysSection(passkeys, ""))
}

// securityPath is the Security tab of the account settings.
func securityPath(r *http.Request) string {
	return ctxkeys.AppBasePath(r.Context()) + "/settings/account?tab=security"
}

// passkeyDone tells passkey.js where to go after a ceremony worked.
type passkeyDone struct {
	Location string `json:"location"`
}

// RemovePasskey removes a passkey of the signed-in user, unless it is
// their last way in, and answers the passkeys as they are now.
func (h *SettingsHandler) RemovePasskey(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	err := h.passkeys.Remove(ctxkeys.Session(r.Context()), r.PathValue("id"))
	switch {
	case errors.Is(err, repository.ErrPasskeyLastWayIn):
		answerFailed(w, r, "Passkey not removed", "This is your last way in. Add another first.")
		return
	case errors.Is(err, repository.ErrPasskeyNotFound):
		answerFailed(w, r, "Passkey not found", "It was removed already.")
		return
	case err != nil:
		failed(w, r, "Passkey not removed", err, nil)
		return
	}
	passkeys, err := h.passkeys.List(user.ID)
	if err != nil {
		slog.Error("failed to list passkeys", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Passkeys not refreshed", "Something went wrong. Please try again.")
		return
	}
	answerDone(w, r, "Passkey removed", "It no longer signs you in.")
	render(w, r, pages.SettingsPasskeysSection(passkeys, ""))
}

func setPasskeyCeremony(w http.ResponseWriter, r *http.Request, ceremony string) {
	http.SetCookie(w, &http.Cookie{
		Name:     passkeyCeremonyCookie,
		Value:    ceremony,
		Path:     "/",
		MaxAge:   int(service.PasskeyCeremonyExpiry.Seconds()),
		HttpOnly: true,
		Secure:   ctxkeys.RenderConfig(r.Context()).IsProduction(),
		SameSite: http.SameSiteStrictMode,
	})
}

// takePasskeyCeremony is the id of the browser's ceremony, whose cookie it
// clears: a ceremony works once.
func takePasskeyCeremony(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(passkeyCeremonyCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     passkeyCeremonyCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   ctxkeys.RenderConfig(r.Context()).IsProduction(),
		SameSite: http.SameSiteStrictMode,
	})
	return cookie.Value
}

// passkeyFailed tells passkey.js why, in plain text.
func passkeyFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrPasskeyExpired):
		http.Error(w, "This took too long. Please try again.", http.StatusBadRequest)
	case errors.Is(err, service.ErrPasskeyRefused):
		slog.Warn("passkey refused", "error", err)
		http.Error(w, "This passkey did not work. Try another way in.", http.StatusBadRequest)
	case errors.Is(err, repository.ErrPasskeyTaken):
		http.Error(w, "This passkey is added already.", http.StatusConflict)
	case errors.Is(err, service.ErrImpersonating):
		http.Error(w, "Impersonation is read-only.", http.StatusForbidden)
	case errors.Is(err, service.ErrConfirm):
		// passkey.js opens the page's Confirm it's you dialog.
		http.Error(w, "Confirm it's you first.", http.StatusPreconditionRequired)
	case errors.Is(err, repository.ErrSessionNotFound):
		http.Error(w, "Your session ended. Sign in again.", http.StatusUnauthorized)
	case errors.Is(err, service.ErrPasskeysOff):
		http.Error(w, "Passkeys need this hub on HTTPS with a domain name.", http.StatusConflict)
	default:
		slog.Error("passkey failed", "error", err)
		http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
	}
}
