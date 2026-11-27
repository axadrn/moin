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

	catalogfiles "github.com/axadrn/moin/catalog"
)

// New is the whole site: every route, wrapped in the middleware every
// request goes through.
func New(cfg *config.Config) (http.Handler, error) {
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
	mux.HandleFunc("GET /{$}", handler.Home(texts))
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
	mux.HandleFunc("GET /compare", handler.CompareIndex)
	mux.HandleFunc("GET /compare/{name}", handler.Compare)
	mux.HandleFunc("GET /legal/{page}", handler.Legal(texts))
	// The script that installs moin, and the binaries it downloads: the
	// image builds them into downloads/.
	mux.HandleFunc("GET /cli.sh", handler.CLIScript(cfg.AppURL))
	mux.Handle("GET /download/", http.StripPrefix("/download/", http.FileServer(http.Dir("downloads"))))
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
