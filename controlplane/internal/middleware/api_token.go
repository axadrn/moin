package middleware

import (
	"net/http"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// APITokenAuth authenticates a personal API token sent as
// "Authorization: Bearer pat_...". It runs after AuthMiddleware, which leaves
// such requests unauthenticated. Tokens only work on the paths
// acceptsAPITokens lists: on every other path the request stays
// unauthenticated. The context gets the user and the token, never a session.
func APITokenAuth(tokens *service.APITokenService, userService *service.UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ctxkeys.User(r.Context()) != nil || !acceptsAPITokens(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			raw, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok || !strings.HasPrefix(raw, service.APITokenMarker) {
				next.ServeHTTP(w, r)
				return
			}
			token, err := tokens.Authenticate(raw)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			user, err := userService.ByID(token.UserID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			user.PasswordSet = user.HasPassword()
			user.PasswordHash = nil
			ctx := ctxkeys.WithUser(r.Context(), user)
			ctx = ctxkeys.WithAPIToken(ctx, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// acceptsAPITokens reports whether personal API tokens may authenticate a
// path: the JSON API and the MCP server.
func acceptsAPITokens(path string) bool {
	return strings.HasPrefix(path, "/api/") || path == "/mcp"
}
