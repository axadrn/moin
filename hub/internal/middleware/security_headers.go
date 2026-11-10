package middleware

import (
	"net/http"
	"strings"
)

type SecurityHeadersConfig struct {
	RobotsNoIndex bool
	S3Endpoint    string
	UmamiHost     string
	PlausibleHost string
}

// SecurityHeaders adds the security headers to all responses. The nonce
// from NonceMiddleware lets the inline scripts of a page run.
func SecurityHeaders(cfg SecurityHeadersConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := GetNonce(r.Context())
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=(), magnetometer=()")
		if cfg.RobotsNoIndex {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy(cfg, nonce))
		next.ServeHTTP(w, r)
	})
}

// AllowOwnFrames lets this site show a response in a frame of its own. The
// headers are set by then, so this changes them.
func AllowOwnFrames(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	policy := w.Header().Get("Content-Security-Policy")
	w.Header().Set("Content-Security-Policy", strings.Replace(policy, "frame-ancestors 'none'", "frame-ancestors 'self'", 1))
}

// contentSecurityPolicy says what a page may load and run. Scripts need the
// nonce or one of the listed hosts, and nothing may frame the page.
func contentSecurityPolicy(cfg SecurityHeadersConfig, nonce string) string {
	umamiHost, umamiConnect := umamiSources(cfg)
	plausibleHost := plausibleSource(cfg)
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'nonce-" + nonce + "'" + frontendScriptPolicy + " https://" + umamiHost + " https://" + plausibleHost + " https://www.googletagmanager.com",
		"style-src 'self' 'unsafe-inline'",
		imageSources(cfg),
		"font-src 'self' data:",
		"connect-src 'self' " + umamiConnect + " https://" + plausibleHost + " https://*.google-analytics.com https://analytics.google.com",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self' https://sandbox.polar.sh https://polar.sh https://checkout.stripe.com",
	}, "; ")
}

// imageSources allows any HTTPS image, and the S3 endpoint when it has one.
func imageSources(cfg SecurityHeadersConfig) string {
	sources := "img-src 'self' data: https:"
	if cfg.S3Endpoint != "" {
		sources += " " + cfg.S3Endpoint
	}
	return sources
}

// umamiSources is the host that serves Umami's script and the hosts it
// sends events to: several for the cloud, one when self-hosted.
func umamiSources(cfg SecurityHeadersConfig) (string, string) {
	if cfg.UmamiHost != "" && cfg.UmamiHost != "cloud.umami.is" {
		return cfg.UmamiHost, "https://" + cfg.UmamiHost
	}
	return "cloud.umami.is", "https://cloud.umami.is https://analytics.umami.is https://gateway.umami.is https://eu.umami.is https://api-gateway-eu.umami.dev https://api-gateway.umami.dev"
}

// plausibleSource is the Plausible host: plausible.io unless self-hosted.
func plausibleSource(cfg SecurityHeadersConfig) string {
	if cfg.PlausibleHost != "" {
		return cfg.PlausibleHost
	}
	return "plausible.io"
}
