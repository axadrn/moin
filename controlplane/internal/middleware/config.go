package middleware

import (
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/config"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
)

func RenderConfig(cfg config.RenderConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ctxkeys.WithRenderConfig(r.Context(), cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
