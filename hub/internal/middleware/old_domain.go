package middleware

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// OldDomain sends every request on an old address of the site (OLD_URLS)
// to the same path on APP_URL.
func OldDomain(oldURLs []string, appURL string) func(http.Handler) http.Handler {
	appURL = strings.TrimRight(appURL, "/")
	var hosts []string
	for _, old := range oldURLs {
		if parsed, err := url.Parse(old); err == nil {
			hosts = append(hosts, strings.ToLower(parsed.Hostname()))
		}
	}
	return func(next http.Handler) http.Handler {
		if len(hosts) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := strings.ToLower(r.Host)
			if name, _, found := strings.Cut(host, ":"); found {
				host = name
			}
			if !slices.Contains(hosts, host) {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, appURL+r.URL.RequestURI(), http.StatusMovedPermanently)
		})
	}
}
