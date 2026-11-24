package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

type SettingsHandler struct {
	authService *service.AuthService
	passkeys    *service.PasskeyService
	apiTokens   *service.APITokenService
	apps        *service.AppService
	updates     *service.UpdateService
}

func NewSettingsHandler(authService *service.AuthService, passkeys *service.PasskeyService, apiTokens *service.APITokenService, apps *service.AppService, updates *service.UpdateService) *SettingsHandler {
	return &SettingsHandler{authService: authService, passkeys: passkeys, apiTokens: apiTokens, apps: apps, updates: updates}
}

// Home is Settings: you, your workspaces, this workspace, moin, sign out.
func (h *SettingsHandler) Home(w http.ResponseWriter, r *http.Request) {
	// The admins of the hub see its backup here, on every hub.
	view := pages.SettingsHomeView{HubBackup: h.updates.Info(ctxkeys.User(r.Context())).HubBackup}
	render(w, r, pages.SettingsHome(view))
}

// Integrations are the accounts the workspace connects once: GitHub,
// Cloudflare and Tailscale.
func (h *SettingsHandler) Integrations(w http.ResponseWriter, r *http.Request) {
	installations, enabled, err := h.gitHub(r)
	if err != nil {
		renderError(w, "load github installations failed", err)
		return
	}
	keys, err := h.apps.Integrations(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		renderError(w, "load integrations failed", err)
		return
	}
	render(w, r, pages.Integrations(pages.IntegrationsView{GitHub: enabled, GitHubAccounts: installations, Keys: keys}))
}

// SetIntegrations connects Cloudflare or Tailscale with the key in the
// form, replaces the key of a connected one, or disconnects the one remove
// names, and answers the page.
func (h *SettingsHandler) SetIntegrations(w http.ResponseWriter, r *http.Request) {
	var cloudflare, tailscale *string
	cloudflareToken, tailscaleKey := r.FormValue("cloudflare_token"), r.FormValue("tailscale_key")
	notDone := "Key not saved"
	switch r.FormValue("remove") {
	case "cloudflare":
		cloudflareToken, notDone = "", "Cloudflare not disconnected"
		cloudflare = &cloudflareToken
	case "tailscale":
		tailscaleKey, notDone = "", "Tailscale not disconnected"
		tailscale = &tailscaleKey
	default:
		if cloudflareToken != "" {
			cloudflare = &cloudflareToken
		}
		if tailscaleKey != "" {
			tailscale = &tailscaleKey
		}
	}
	err := h.apps.SetIntegrations(r.Context(), actor(r), service.IntegrationsInput{CloudflareToken: cloudflare, TailscaleKey: tailscale, TailscaleServer: r.FormValue("server")})
	if errors.Is(err, service.ErrCloudflareToken) {
		fieldRefused(w, r, notDone, "cloudflare-token", errorMessage(err))
		return
	}
	if err != nil {
		failed(w, r, notDone, err, map[string]string{"tailscale_key": "tailscale-key", "server": "tailscale-key-server"})
		return
	}
	h.Integrations(w, r)
}

// gitHub is the accounts the workspace connected, and whether this hub
// has the GitHub App at all.
func (h *SettingsHandler) gitHub(r *http.Request) ([]model.GitHubInstallation, bool, error) {
	if _, err := h.apps.GitHubInstallURL(actor(r)); err != nil {
		return nil, false, nil
	}
	installations, err := h.apps.GitHubInstallations(ctxkeys.Workspace(r.Context()).ID)
	return installations, true, err
}

// SettingsPage is your account: profile, how you sign in, and tokens.
func (h *SettingsHandler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	sessions, err := h.authService.Sessions(user.ID)
	if err != nil {
		slog.Error("failed to list sessions", "error", err, "user_id", user.ID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Settings unavailable", "Your settings could not be loaded. Please try again."))
		return
	}

	activeTab := r.URL.Query().Get("tab")
	if activeTab != "security" && activeTab != "tokens" {
		activeTab = "profile"
	}
	currentSession := ctxkeys.Session(r.Context())
	currentSessionID := ""
	if currentSession != nil {
		currentSessionID = currentSession.ID
	}

	tokens, err := apiTokensView(r, h.apiTokens, newAPITokenForm(r))
	if err != nil {
		slog.Error("failed to list api tokens", "error", err, "user_id", user.ID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Settings unavailable", "Your settings could not be loaded. Please try again."))
		return
	}
	passkeys, err := h.passkeys.List(user.ID)
	if err != nil {
		slog.Error("failed to list passkeys", "error", err, "user_id", user.ID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Settings unavailable", "Your settings could not be loaded. Please try again."))
		return
	}
	render(w, r, pages.Settings(sessions, currentSessionID, passkeys, activeTab, pages.SettingsAPITokensSection(tokens)))
}

func (h *SettingsHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	session := ctxkeys.Session(r.Context())
	sessionID := r.PathValue("id")

	if err := h.authService.RevokeSession(user.ID, sessionID); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			if isAction(r) {
				answerFailed(w, r, "Session not found", "That session has already ended.")
			} else {
				w.WriteHeader(http.StatusNotFound)
				render(w, r, pages.ErrorPage(http.StatusNotFound, "Session not found", "That session has already ended."))
			}
			return
		}
		slog.Error("failed to revoke session", "error", err, "user_id", user.ID, "session_id", sessionID)
		if isAction(r) {
			answerFailed(w, r, "Session not revoked", "Something went wrong. Please try again.")
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Session could not be revoked", "Please try again."))
		}
		return
	}

	if session != nil && session.ID == sessionID {
		h.authService.ClearSessionCookie(w)
		redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}
	location := ctxkeys.AppBasePath(r.Context()) + "/settings/account?tab=security"
	if !isAction(r) {
		redirect(w, r, location, http.StatusSeeOther)
		return
	}
	sessions, err := h.authService.Sessions(user.ID)
	if err != nil {
		slog.Error("failed to list sessions", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Sessions not refreshed", "Something went wrong. Please try again.")
		return
	}
	answerDone(w, r, "Session revoked", "The session has been signed out.")
	currentSessionID := ""
	if session != nil {
		currentSessionID = session.ID
	}
	render(w, r, pages.SettingsSessionsSection(sessions, currentSessionID))
}

func (h *SettingsHandler) RevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if err := h.authService.RevokeAllSessions(user.ID); err != nil {
		slog.Error("failed to revoke all sessions", "error", err, "user_id", user.ID)
		if isAction(r) {
			answerFailed(w, r, "Sessions not revoked", "Something went wrong. Please try again.")
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Sessions could not be revoked", "Please try again."))
		}
		return
	}

	h.authService.ClearSessionCookie(w)
	redirect(w, r, "/auth", http.StatusSeeOther)
}
