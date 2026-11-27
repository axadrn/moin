package middleware

import (
	"crypto/rand"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/axadrn/moin/cloud/internal/config"
)

// SecurityHeaders adds the security headers to every response. Each
// request gets a new nonce: the inline scripts of its page carry it
// (templ.GetNonce), and only scripts with it run.
func SecurityHeaders(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nonce := rand.Text()
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=(), magnetometer=()")
			w.Header().Set("Content-Security-Policy", contentSecurityPolicy(cfg, nonce))
			next.ServeHTTP(w, r.WithContext(templ.WithNonce(r.Context(), nonce)))
		})
	}
}

// contentSecurityPolicy says what a page may load and run. Scripts need the
// nonce, and nothing may frame the page. The Umami server may send its
// script and take the page views.
func contentSecurityPolicy(cfg *config.Config, nonce string) string {
	umami := ""
	if cfg.UmamiHost != "" {
		umami = " https://" + cfg.UmamiHost
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'nonce-" + nonce + "'" + umami,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"font-src 'self' data:",
		"connect-src 'self'" + umami,
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}, "; ")
}
