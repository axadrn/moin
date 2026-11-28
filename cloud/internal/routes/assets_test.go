package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/cloud/internal/ui/components"
	"github.com/axadrn/moin/internal/build"
)

// A page links its files with the version in the path. The site serves
// them there as at the plain path, cached for a year outside development.
func TestVersionedAssets(t *testing.T) {
	serve := func(development bool, path string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		assetRoutes(mux, development)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	const immutable = "public, max-age=31536000, immutable"

	css := components.AssetURL("/assets/css/site.css")
	if want := "/assets/" + build.Version + "/css/site.css"; css != want {
		t.Fatalf("AssetURL = %q, want %q", css, want)
	}
	for _, path := range []string{
		css,
		components.AssetURL("/assets/js/site.js"),
		components.AssetURL("/assets/img/og.png"),
		components.AssetURL("/assets/manifest.json"),
		components.AssetURL("/assets/icons/check.svg"),
		components.AssetURL("/assets/fonts/geist/geist-variable.woff2"),
	} {
		w := serve(false, path)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != immutable {
			t.Errorf("GET %s = %d, Cache-Control %q; want 200, %q", path, w.Code, w.Header().Get("Cache-Control"), immutable)
		}
	}
	if w := serve(false, "/assets/css/site.css"); w.Code != http.StatusOK || w.Header().Get("Cache-Control") == immutable {
		t.Errorf("GET the plain path = %d, Cache-Control %q; want 200, not immutable", w.Code, w.Header().Get("Cache-Control"))
	}
	if w := serve(false, "/favicon.ico"); w.Code != http.StatusOK {
		t.Errorf("GET /favicon.ico = %d, want 200", w.Code)
	}
}
