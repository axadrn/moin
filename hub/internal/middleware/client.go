package middleware

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
)

// Client stores the client IP and user agent of each request, so the
// activity can show where a change came from.
func Client(clientIPs ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client := ctxkeys.Client{IP: clientIPs.ClientIP(r), UserAgent: r.UserAgent()}
			next.ServeHTTP(w, r.WithContext(ctxkeys.WithClient(r.Context(), client)))
		})
	}
}
