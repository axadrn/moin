package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin"
	"github.com/axadrn/moin/hub/internal/app"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ui/components"
	"github.com/axadrn/moin/internal/build"
)

func TestImmutableAssetPathPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "js/runtime-1.2.3.deadbeef.js", want: true},
		{path: "/js/runtime-1.2.3.deadbeef.js", want: true},
		{path: "/js/runtime-1.2.3.deadbeef.min.js", want: true},
		{path: "/js/runtime.js", want: false},
		{path: "/css/site.deadbeef.css", want: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := immutableAssetPathPattern.MatchString(test.path); got != test.want {
				t.Fatalf("immutableAssetPathPattern.MatchString(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestFontAssetPathPattern(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"fonts/geist/geist-variable.woff2":              true,
		"/fonts/bricolage/bricolage-grotesque-bold.ttf": true,
		"/fonts/geist/README.md":                        false,
		"/css/hub.css":                                  false,
	} {
		if got := fontAssetPathPattern.MatchString(path); got != want {
			t.Errorf("fontAssetPathPattern.MatchString(%q) = %t, want %t", path, got, want)
		}
	}
}

// A page links its files with the version in the path. The hub serves them
// there as at the plain path, cached for a year outside development.
func TestVersionedAssets(t *testing.T) {
	t.Parallel()

	serve := func(env config.Environment, path string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		assetRoutes(mux, &app.App{Cfg: &config.Config{AppEnv: env}, UIFiles: moin.UI(false)})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	const immutable = "public, max-age=31536000, immutable"

	css := components.AssetURL("/assets/app.css")
	if want := "/assets/" + build.Version + "/app.css"; css != want {
		t.Fatalf("AssetURL = %q, want %q", css, want)
	}
	for _, path := range []string{
		css,
		components.AssetURL("/assets/app.js"),
		components.AssetURL("/assets/img/og.png"),
		components.AssetURL("/assets/css/theme-editor.css"),
		components.AssetURL("/assets/ui/icons/check.svg"),
	} {
		w := serve(config.EnvironmentProduction, path)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != immutable {
			t.Errorf("GET %s = %d, Cache-Control %q; want 200, %q", path, w.Code, w.Header().Get("Cache-Control"), immutable)
		}
	}
	if w := serve(config.EnvironmentProduction, css); !strings.HasPrefix(w.Header().Get("Content-Type"), "text/css") || strings.Contains(w.Body.String(), "@import") {
		t.Errorf("GET %s = %q, want the one stylesheet", css, w.Header().Get("Content-Type"))
	}

	for _, path := range []string{"/assets/app.css", "/assets/css/hub.css", "/assets/ui/moin.css"} {
		w := serve(config.EnvironmentProduction, path)
		if w.Code != http.StatusOK || w.Header().Get("Cache-Control") == immutable {
			t.Errorf("GET %s = %d, Cache-Control %q; want 200, not immutable", path, w.Code, w.Header().Get("Cache-Control"))
		}
	}

	if w := serve(config.EnvironmentProduction, "/assets/"+build.Version+"/missing.css"); w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "" {
		t.Errorf("GET a missing file = %d, Cache-Control %q; want 404, uncached", w.Code, w.Header().Get("Cache-Control"))
	}

	path := components.AssetURL("/assets/ui/moin.css")
	if w := serve(config.EnvironmentDevelopment, path); w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("GET %s in development = %d, Cache-Control %q; want 200, no-store", path, w.Code, w.Header().Get("Cache-Control"))
	}
}
