package middleware

import (
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// BearerAuth authenticates "Authorization: Bearer <token>": an access token
// of a connection (at_...) or a CI token (pat_...). It runs after
// AuthMiddleware, which leaves such requests unauthenticated. Tokens only
// work on the paths acceptsBearers lists, and a connection bound to one
// resource, like an AI agent's to /mcp, only there: everywhere else the
// request stays unauthenticated. The context gets the user and the bearer,
// never a session.
func BearerAuth(connections *service.ConnectionService, ciTokens *service.CITokenService, userService *service.UserService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ctxkeys.User(r.Context()) != nil || !acceptsBearers(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			raw, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			var bearer *model.Bearer
			var err error
			switch {
			case strings.HasPrefix(raw, service.AccessTokenMarker):
				bearer, err = connections.Authenticate(raw)
			case strings.HasPrefix(raw, service.CITokenMarker):
				bearer, err = ciTokens.Authenticate(raw)
			default:
				next.ServeHTTP(w, r)
				return
			}
			if err != nil || (bearer.Resource != "" && r.URL.Path != bearer.Resource) {
				next.ServeHTTP(w, r)
				return
			}
			user, err := userService.ByID(bearer.UserID)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := ctxkeys.WithUser(r.Context(), user)
			ctx = ctxkeys.WithBearer(ctx, bearer)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// acceptsBearers reports whether connections and CI tokens may
// authenticate a path: the JSON API and the MCP server.
func acceptsBearers(path string) bool {
	return strings.HasPrefix(path, "/api/") || path == "/mcp"
}
