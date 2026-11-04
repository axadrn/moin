package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

type SettingsHandler struct {
	authService *service.AuthService
	apiTokens   *service.APITokenService
	apps        *service.AppService
}

func NewSettingsHandler(authService *service.AuthService, apiTokens *service.APITokenService, apps *service.AppService) *SettingsHandler {
	return &SettingsHandler{authService: authService, apiTokens: apiTokens, apps: apps}
}

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
	registries, err := h.apps.Registries(user.ID)
	if err != nil {
		slog.Error("failed to list registries", "error", err, "user_id", user.ID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Settings unavailable", "Your settings could not be loaded. Please try again."))
		return
	}
	render(w, r, pages.Settings(sessions, currentSessionID, activeTab, r.URL.Query().Get("password_removed") == "1", pages.SettingsAPITokensSection(tokens), pages.SettingsRegistriesSection(registries)))
}

func (h *SettingsHandler) RevokeSession(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	session := ctxkeys.Session(r.Context())
	sessionID := r.PathValue("id")

	if err := h.authService.RevokeSession(user.ID, sessionID); err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			if isAction(r) {
				toastFailed(w, r, "Session not found", "That session has already ended.")
			} else {
				w.WriteHeader(http.StatusNotFound)
				render(w, r, pages.ErrorPage(http.StatusNotFound, "Session not found", "That session has already ended."))
			}
			return
		}
		slog.Error("failed to revoke session", "error", err, "user_id", user.ID, "session_id", sessionID)
		if isAction(r) {
			toastFailed(w, r, "Session not revoked", "Something went wrong. Please try again.")
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
	location := fmt.Sprintf("/app/%s/settings?tab=security", ctxkeys.Workspace(r.Context()).Slug)
	if !isAction(r) {
		redirect(w, r, location, http.StatusSeeOther)
		return
	}
	sessions, err := h.authService.Sessions(user.ID)
	if err != nil {
		slog.Error("failed to list sessions", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Sessions not refreshed", "Something went wrong. Please try again.")
		return
	}
	toastDone(w, r, "Session revoked", "The session has been signed out.")
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
			toastFailed(w, r, "Sessions not revoked", "Something went wrong. Please try again.")
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Sessions could not be revoked", "Please try again."))
		}
		return
	}

	h.authService.ClearSessionCookie(w)
	redirect(w, r, "/auth", http.StatusSeeOther)
}
