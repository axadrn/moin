package middleware

import (
	"net/http"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
)

func AdminContext(adminService *service.AdminService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isAdmin := adminService.IsAdmin(ctxkeys.User(r.Context()), ctxkeys.Session(r.Context()))
			ctx := ctxkeys.WithAdmin(r.Context(), isAdmin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ctxkeys.IsAdmin(r.Context()) {
			renderMiddlewareError(w, r, http.StatusNotFound, "Not found", "The requested admin page was not found.")
			return
		}
		next.ServeHTTP(w, r)
	}
}

func BlockImpersonatedWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := ctxkeys.Session(r.Context())
		if session.IsImpersonating() && !allowedDuringImpersonation(r) {
			if isAPIRequest(r) {
				api.Error(w, http.StatusForbidden, "impersonation_read_only", "Impersonation is read-only. Return to Admin before making changes.")
				return
			}
			renderMiddlewareError(w, r, http.StatusForbidden, "Read-only session", "Return to Admin before making changes.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedDuringImpersonation(r *http.Request) bool {
	if r.Method == http.MethodPost {
		return r.URL.Path == "/admin/impersonation/stop" || r.URL.Path == "/auth/logout" || r.URL.Path == "/api/v1/auth/logout"
	}
	if r.Method == http.MethodOptions {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	path := r.URL.Path
	if path == "/auth/onboarding" {
		return true
	}
	// GitHub's return link connects an installation: a write behind a GET.
	if path == "/app/github/setup" {
		return false
	}
	if path == "/app" || strings.HasPrefix(path, "/app/") {
		return !strings.HasSuffix(path, "/billing/manage") && !strings.HasSuffix(path, "/billing/portal")
	}
	if strings.HasPrefix(path, "/api/v1/") {
		return !strings.HasSuffix(path, "/billing/manage") && !strings.HasSuffix(path, "/billing/portal")
	}
	return path == "/" || path == "/robots.txt" || path == "/sitemap.xml" || path == "/llms.txt" ||
		strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/components/") ||
		path == "/blog" || strings.HasPrefix(path, "/blog/") ||
		path == "/docs" || strings.HasPrefix(path, "/docs/") || strings.HasPrefix(path, "/legal/")
}
