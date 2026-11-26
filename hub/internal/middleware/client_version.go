package middleware

import (
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/internal/build"
)

// MinClientVersion is the oldest CLI and TUI the hub takes. A release that
// changes the API in a way older ones break on raises it to its own version.
const MinClientVersion = "v1.0.0-beta.35"

// RefuseOutdatedClients answers 426 client_outdated to a CLI or TUI older
// than MinClientVersion, on every path under /api/. They name themselves in
// the user agent, like moin-cli/v1.0.0-beta.34. Builds without a release,
// like dev, and every other client pass: curl, CI, MCP clients, browsers.
// The daemon never calls /api/; the protocol version guards it.
func RefuseOutdatedClients(appURL string) func(http.Handler) http.Handler {
	message := "This " + build.Name + " is too old for this hub. It needs " + MinClientVersion + " or newer. Update it with `curl -fsSL " + strings.TrimRight(appURL, "/") + "/cli.sh | sh`"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") || !outdatedClient(r.UserAgent()) {
				next.ServeHTTP(w, r)
				return
			}
			switch r.URL.Path {
			case "/api/v1/auth/token", "/api/v1/auth/revoke", "/api/v1/auth/register":
				// The OAuth endpoints answer in the shape of OAuth.
				w.Header().Set("Cache-Control", "no-store")
				api.JSON(w, http.StatusUpgradeRequired, api.OAuthError{Error: "client_outdated", Description: message})
			default:
				api.Error(w, http.StatusUpgradeRequired, "client_outdated", message)
			}
		})
	}
}

// outdatedClient reports whether userAgent is a CLI or TUI release older
// than MinClientVersion.
func outdatedClient(userAgent string) bool {
	for _, surface := range []string{"cli", "tui"} {
		version, ok := strings.CutPrefix(userAgent, build.Name+"-"+surface+"/")
		if ok {
			return build.NewerVersion(MinClientVersion, version)
		}
	}
	return false
}
