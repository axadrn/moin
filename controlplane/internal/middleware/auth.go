package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/components/toast"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// AuthMiddleware checks for a database session and adds authentication data to the context.
func AuthMiddleware(authService *service.AuthService, userService *service.UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hasBearerAuthorization(r) {
				// A bearer request never falls back to cookies. An invalid token
				// continues unauthenticated, so CSRFProtection may skip it safely.
				session, token, err := authenticateBearer(r, authService)
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
				if session.IsImpersonating() {
					next.ServeHTTP(w, r)
					return
				}
				user, err := userService.ByID(session.UserID)
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
				user.PasswordSet = user.HasPassword()
				user.PasswordHash = nil
				ctx := ctxkeys.WithUser(r.Context(), user)
				ctx = ctxkeys.WithSession(ctx, session)
				ctx = ctxkeys.WithBearer(ctx, token)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			for _, cookieName := range []string{"impersonation_token", "session_token"} {
				cookie, err := r.Cookie(cookieName)
				if err != nil {
					continue
				}

				session, err := authService.AuthenticateSession(cookie.Value)
				if err != nil {
					authService.ClearCookie(w, cookieName)
					continue
				}
				if (cookieName == "impersonation_token") != session.IsImpersonating() {
					authService.ClearCookie(w, cookieName)
					continue
				}
				user, err := userService.ByID(session.UserID)
				if err != nil {
					authService.ClearCookie(w, cookieName)
					continue
				}
				user.PasswordSet = user.HasPassword()
				user.PasswordHash = nil
				ctx := ctxkeys.WithUser(r.Context(), user)
				ctx = ctxkeys.WithSession(ctx, session)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func WithWorkspace(workspaceService *service.WorkspaceService, subscriptionService *service.SubscriptionService) func(http.HandlerFunc) http.HandlerFunc {
	withWorkspace := withWorkspaceOf(func(r *http.Request) string { return r.PathValue("workspaceSlug") }, workspaceService, subscriptionService)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return withWorkspace(RequireChanges(next))
	}
}

// RequireChanges refuses a change to a workspace whose plan allows none,
// such as after its trial, unless readOnlyAllows it. It runs after the
// workspace is in the context.
func RequireChanges(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subscription := ctxkeys.Subscription(r.Context())
		if readOnlyAllows(r) || subscription == nil || subscription.Access().Has(access.FeatureChanges) {
			next.ServeHTTP(w, r)
			return
		}
		if isAPIRequest(r) {
			api.Error(w, http.StatusPaymentRequired, "trial_ended", api.TrialEndedMessage)
			return
		}
		renderAuthError(w, r, http.StatusPaymentRequired, "Your trial has ended", api.TrialEndedMessage)
	}
}

// WithTokenWorkspace scopes an ops API route, whose path names no
// workspace, to the workspace of the personal API token. A session names
// the workspace in the Ops-Workspace header, or an EventSource, which sends
// no headers, in the workspace query parameter; otherwise it gets its first.
func WithTokenWorkspace(workspaceService *service.WorkspaceService, subscriptionService *service.SubscriptionService) func(http.HandlerFunc) http.HandlerFunc {
	withWorkspace := withWorkspaceOf(func(r *http.Request) string {
		user := ctxkeys.User(r.Context())
		workspaces, err := workspaceService.ListForUser(user.ID)
		if err != nil || len(workspaces) == 0 {
			return ""
		}
		token := ctxkeys.APIToken(r.Context())
		if token == nil {
			if slug := r.Header.Get("Ops-Workspace"); slug != "" {
				return slug
			}
			if slug := r.URL.Query().Get("workspace"); slug != "" {
				return slug
			}
			return workspaces[0].Slug
		}
		for _, workspace := range workspaces {
			if workspace.ID == token.WorkspaceID {
				return workspace.Slug
			}
		}
		return ""
	}, workspaceService, subscriptionService)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return withWorkspace(RequireChanges(next))
	}
}

func withWorkspaceOf(slugOf func(*http.Request) string, workspaceService *service.WorkspaceService, subscriptionService *service.SubscriptionService) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user := ctxkeys.User(r.Context())
			if user == nil {
				renderAuthError(w, r, http.StatusUnauthorized, "Sign in required", "Sign in to open this workspace.")
				return
			}
			workspace, member, err := workspaceService.BySlugForUser(slugOf(r), user.ID)
			if err != nil {
				if errors.Is(err, repository.ErrWorkspaceNotFound) || errors.Is(err, repository.ErrWorkspaceMemberNotFound) {
					renderAuthError(w, r, http.StatusNotFound, "Workspace not found", "The requested workspace does not exist or you no longer have access.")
					return
				}
				slog.Error("workspace could not be loaded", "error", err, "user_id", user.ID)
				renderAuthError(w, r, http.StatusInternalServerError, "Workspace unavailable", "The workspace could not be loaded. Please try again.")
				return
			}
			subscription, err := subscriptionService.EnsureSubscription(workspace.ID)
			if err != nil {
				slog.Error("workspace subscription could not be loaded", "error", err, "workspace_id", workspace.ID)
				renderAuthError(w, r, http.StatusInternalServerError, "Workspace unavailable", "The workspace subscription could not be loaded. Please try again.")
				return
			}
			workspaces, err := workspaceService.ListForUser(user.ID)
			if err != nil {
				slog.Error("workspaces could not be loaded", "error", err, "user_id", user.ID)
				renderAuthError(w, r, http.StatusInternalServerError, "Workspaces unavailable", "Your workspaces could not be loaded. Please try again.")
				return
			}
			ctx := ctxkeys.WithWorkspace(r.Context(), workspace)
			ctx = ctxkeys.WithWorkspaceMember(ctx, member)
			ctx = ctxkeys.WithWorkspaces(ctx, workspaces)
			ctx = ctxkeys.WithSubscription(ctx, subscription)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	}
}

// readOnlyAllows says whether a request may run in a workspace whose plan
// allows no changes: reading, SQL (writes are checked again), the workspace
// with its team and billing, the account, sessions and tokens, and stopping
// or starting an app.
func readOnlyAllows(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	path := r.URL.Path
	for _, part := range []string{"/api/v1/workspaces/", "/workspace/", "/billing/", "/account", "/profile/", "/settings/sessions", "/settings/api-tokens", "/data/query", "/databases/query"} {
		if strings.Contains(path, part) {
			return true
		}
	}
	// POST …/apps/{app}/stop and …/apps/{app}/start.
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	return r.Method == http.MethodPost && len(parts) >= 3 && parts[len(parts)-3] == "apps" && (last == "stop" || last == "start")
}

func renderAuthError(w http.ResponseWriter, r *http.Request, status int, title, description string) {
	if isAPIRequest(r) {
		api.Error(w, status, api.CodeForStatus(status), description)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		if _, err := fmt.Fprint(w, `<div hx-swap-oob="beforeend:#toast-container">`); err != nil {
			slog.Error("render auth error toast wrapper failed", "error", err)
			return
		}
		if err := toast.Toast(toast.Props{Title: title, Description: description, Type: toast.TypeError}).Render(r.Context(), w); err != nil {
			slog.Error("render auth error toast failed", "error", err)
			return
		}
		if _, err := fmt.Fprint(w, `</div>`); err != nil {
			slog.Error("render auth error toast wrapper failed", "error", err)
		}
		return
	}
	w.WriteHeader(status)
	if err := pages.ErrorPage(status, title, description).Render(r.Context(), w); err != nil {
		slog.Error("render auth error page failed", "error", err)
	}
}

// RequireAuth ensures the user is authenticated and has completed onboarding
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := ctxkeys.User(r.Context())
		if user == nil {
			redirect(w, r, "/auth", http.StatusSeeOther)
			return
		}

		if user.Name == "" && r.URL.Path != "/auth/onboarding" {
			redirect(w, r, "/auth/onboarding", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	}
}

// RequireGuest ensures the user is not authenticated
func RequireGuest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := ctxkeys.User(r.Context())
		if user != nil {
			redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	}
}
