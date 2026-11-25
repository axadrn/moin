package middleware

import (
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
)

// APIRequestsPerMinute is how many requests one connection or CI token may
// send to the API and MCP in a minute. It protects the hub from a runaway
// script; it is not a limit of a plan. The odd number is on purpose: seen
// anywhere, it can only be this limit.
const APIRequestsPerMinute = 121

// RateLimitBearers limits each connection and each CI token to
// APIRequestsPerMinute requests a minute. It runs after BearerAuth, so it
// only sees the API and MCP. A stream counts once, when it opens. Browser
// sessions have no bearer and pass: their own limits are elsewhere.
func RateLimitBearers() func(http.Handler) http.Handler {
	return rateLimitBearers(NewRateLimiter(APIRequestsPerMinute, time.Minute))
}

func rateLimitBearers(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := ctxkeys.Bearer(r.Context())
			if bearer == nil {
				next.ServeHTTP(w, r)
				return
			}
			// The key is the ID, never the token: a connection's access
			// token changes every hour, its ID stays.
			key := "connection:" + bearer.ConnectionID
			if bearer.CITokenID != "" {
				key = "ci-token:" + bearer.CITokenID
			}
			allowed, wait := limiter.allow(key)
			if allowed {
				next.ServeHTTP(w, r)
				return
			}
			seconds := int(math.Ceil(wait.Seconds()))
			if seconds < 1 {
				seconds = 1
			}
			slog.Warn("API rate limit exceeded", "key", key, "path", r.URL.Path)
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			api.Error(w, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf(
				"Too many requests: %d a minute per connection or CI token. Try again in %s.",
				APIRequestsPerMinute, secondsText(seconds)))
		})
	}
}

// secondsText is "1 second" or "N seconds".
func secondsText(seconds int) string {
	if seconds == 1 {
		return "1 second"
	}
	return strconv.Itoa(seconds) + " seconds"
}
