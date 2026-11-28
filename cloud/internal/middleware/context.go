package middleware

import (
	"net/http"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/ctxkeys"
)

// RenderContext puts what templates may know into every request: the
// public settings, the path of the page
// and whether the browser is signed in to moin cloud.
func RenderContext(render config.RenderConfig, signedIn func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ctxkeys.WithRenderConfig(r.Context(), render)
			ctx = ctxkeys.WithURLPath(ctx, r.URL.Path)
			// The header differs with the session, so a cache must key on it.
			w.Header().Add("Vary", "Cookie")
			ctx = ctxkeys.WithSignedIn(ctx, signedIn(r))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
