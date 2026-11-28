// Package routes says which handler answers which address of the site.
package routes

import (
	"io/fs"
	"net/http"
	"os"

	"github.com/axadrn/moin/cloud/assets"
	"github.com/axadrn/moin/cloud/content"
	"github.com/axadrn/moin/cloud/internal/catalog"
	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/handler"
	"github.com/axadrn/moin/cloud/internal/middleware"
	"github.com/axadrn/moin/cloud/internal/service"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"

	catalogfiles "github.com/axadrn/moin/catalog"
)

// New is the whole site: every route, wrapped in the middleware every
// request goes through. cloud answers the pages and the API of moin cloud.
func New(cfg *config.Config, cloud *handler.Cloud) (http.Handler, error) {
	// content/ is read from disk in development, so an edit shows on reload.
	texts := content.FS(cfg.Development)
	entries, err := catalog.Load(catalogfiles.Files)
	if err != nil {
		return nil, err
	}
	docs := service.NewDocsService(texts, cfg.Development)
	seo := handler.NewSEOHandler(docs, entries, cfg.AppURL, texts)
	catalogPages := handler.NewCatalogHandler(entries, cfg.AppURL)

	mux := http.NewServeMux()
	assetRoutes(mux, cfg.Development)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /robots.txt", seo.Robots)
	mux.HandleFunc("GET /sitemap.xml", seo.Sitemap)
	mux.HandleFunc("GET /llms.txt", seo.LLMs)
	mux.HandleFunc("GET /{$}", handler.Home(texts,
		cloud.SiteKnocks(cfg.KnocksWorkspace, cfg.KnocksServer, cfg.Development),
		cloud.SiteNumbers(cfg.Development)))
	mux.HandleFunc("GET /docs", handler.Docs(docs))
	mux.HandleFunc("GET /docs.md", handler.Docs(docs))
	mux.HandleFunc("GET /docs/", handler.Docs(docs))
	mux.HandleFunc("GET /search", handler.NewSiteSearchHandler(docs, entries).Search)
	mux.HandleFunc("GET /roadmap", handler.Roadmap(texts))
	mux.HandleFunc("GET /changelog", handler.Changelog(texts))
	mux.HandleFunc("GET /changelog.xml", handler.ChangelogFeed(texts, cfg.AppURL, build.Name))
	mux.HandleFunc("GET /blog", handler.Blog(texts))
	mux.HandleFunc("GET /blog/{slug}", handler.BlogPost(texts))
	mux.HandleFunc("GET /blog.xml", handler.BlogFeed(texts, cfg.AppURL, build.Name))
	mux.HandleFunc("GET /catalog", catalogPages.List)
	mux.HandleFunc("GET /catalog/{slug}", catalogPages.Show)
	mux.HandleFunc("GET /catalog/{slug}/logo", catalogPages.Logo)
	mux.HandleFunc("GET /kill-your-subscriptions", handler.Kill(texts, entries))
	mux.HandleFunc("GET /compare", handler.CompareIndex)
	mux.HandleFunc("GET /compare/{name}", handler.Compare)
	mux.HandleFunc("GET /brand", handler.Brand)
	mux.HandleFunc("GET /legal/{page}", handler.Legal(texts))
	// The script that installs moin. The binary comes from the newest
	// release on GitHub, like every download of moin.
	mux.HandleFunc("GET /cli.sh", handler.CLIScript(cfg.AppURL))

	// moin cloud: sign-in, the workspace, billing, `moin cloud` in the
	// CLI, and the reports of servers.
	mux.HandleFunc("GET /login", cloud.Login)
	mux.HandleFunc("POST /login", cloud.SendLink)
	mux.HandleFunc("GET /login/link", cloud.LinkPage)
	mux.HandleFunc("POST /login/link", cloud.UseLink)
	mux.HandleFunc("GET /auth/github", cloud.GitHubStart)
	mux.HandleFunc("GET /auth/github/callback", cloud.GitHubBack)
	mux.HandleFunc("POST /logout", cloud.SignOut)
	mux.HandleFunc("GET /cloud", cloud.WorkspacePage)
	mux.HandleFunc("GET /cloud/servers/{name}", cloud.ServerPage)
	mux.HandleFunc("GET /cloud/alerts", cloud.AlertsPage)
	mux.HandleFunc("GET /cloud/copies", cloud.CopiesPage)
	mux.HandleFunc("GET /cloud/plan", cloud.PlanPage)
	mux.HandleFunc("GET /cloud/connect", cloud.ConnectPage)
	mux.HandleFunc("GET /cloud/settings", cloud.Settings)
	mux.HandleFunc("POST /cloud/delete", cloud.DeleteAccount)
	mux.HandleFunc("POST /cloud/subscribe", cloud.Subscribe)
	mux.HandleFunc("GET /cloud/billing", cloud.Billing)
	// Stripe's webhook keeps the old service's address, so its endpoint in
	// the Stripe dashboard keeps working.
	mux.HandleFunc("POST /webhooks/payment", cloud.StripeWebhook)
	mux.HandleFunc("GET /cli", cloud.CLIPage)
	mux.HandleFunc("POST /cli", cloud.AllowCLI)
	mux.HandleFunc("POST /api/cli/login", cloud.StartCLILogin)
	mux.HandleFunc("POST /api/cli/login/finish", cloud.FinishCLILogin)
	mux.HandleFunc("GET /api/cloud", cloud.CLIWorkspace)
	mux.HandleFunc("POST /api/cloud/servers", cloud.ConnectServer)
	mux.HandleFunc("DELETE /api/cloud/servers/{name}", cloud.DisconnectServer)
	mux.HandleFunc("POST "+report.Path, cloud.Report)
	mux.HandleFunc("POST /api/copies", cloud.StartCopy)
	mux.HandleFunc("POST /api/copies/{id}/done", cloud.CopyDone)
	mux.HandleFunc("GET /api/cloud/copies", cloud.CLICopies)
	mux.HandleFunc("POST /api/cloud/copies/{id}/download", cloud.CLIDownload)

	mux.HandleFunc("/", handler.NotFound)

	return middleware.Chain(
		mux,
		middleware.RequestLogging,
		middleware.OldDomain(cfg.OldURLs, cfg.AppURL),
		middleware.SecurityHeaders(cfg),
		middleware.RenderContext(cfg.RenderConfig),
	), nil
}

// assetRoutes serve the files under assets/ at /assets/: from disk in
// development, without caching, else from the binary. Pages link them with
// the version in the path (components.AssetURL), which browsers may keep
// for a year.
func assetRoutes(mux *http.ServeMux, development bool) {
	files := fs.FS(assets.Files)
	if development {
		files = os.DirFS("assets")
	}
	serve := http.FileServerFS(files)
	versioned := "/assets/" + build.Version + "/"
	mux.Handle("GET /assets/", cached(development, "", http.StripPrefix("/assets/", serve)))
	mux.Handle("GET "+versioned, cached(development, "public, max-age=31536000, immutable", http.StripPrefix(versioned, serve)))
	// Browsers, link previews and analytics ask for /favicon.ico at the root.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, files, "img/favicon.ico")
	})
}

// cached sets Cache-Control to control, or to no-store in development, so
// an edit shows on reload.
func cached(development bool, control string, next http.Handler) http.Handler {
	if development {
		control = "no-store"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if control != "" {
			w.Header().Set("Cache-Control", control)
		}
		next.ServeHTTP(w, r)
	})
}
