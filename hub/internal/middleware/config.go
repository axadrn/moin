package middleware

import (
	"net/http"
	"sync"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

func RenderConfig(cfg config.RenderConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := ctxkeys.WithRenderConfig(r.Context(), cfg)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GitHubApp puts what pages show of the hub's GitHub App into the request:
// whether people sign in with GitHub, and for an admin of a hub without
// one the form that creates it, built only when a page shows it. It runs
// after the user is known.
func GitHubApp(githubApp *service.GitHubAppService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			render := ctxkeys.RenderConfig(ctx)
			render.GitHubLogin = githubApp.SignsIn()
			ctx = ctxkeys.WithRenderConfig(ctx, render)
			if r.Method == http.MethodGet && !ctxkeys.Session(ctx).IsImpersonating() {
				user := ctxkeys.User(ctx)
				ctx = ctxkeys.WithGitHubAppManifest(ctx, sync.OnceValue(func() *model.GitHubAppManifest {
					return githubApp.Manifest(user)
				}))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
