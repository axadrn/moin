package middleware

import (
	"net/http"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/ctxkeys"
)

// RenderContext puts what templates may know into every request: the
// public settings and the path of the page.
func RenderContext(render config.RenderConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ctxkeys.WithRenderConfig(r.Context(), render)
			ctx = ctxkeys.WithURLPath(ctx, r.URL.Path)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
