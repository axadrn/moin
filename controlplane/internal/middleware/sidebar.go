package middleware

import (
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
)

// WithSidebarState reads the sidebar_state cookie the sidebar script writes
// when it is toggled, so pages render the sidebar collapsed or expanded as
// it was left.
func WithSidebarState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("sidebar_state")
		collapsed := err == nil && cookie.Value == "false"
		next.ServeHTTP(w, r.WithContext(ctxkeys.WithSidebarCollapsed(r.Context(), collapsed)))
	})
}
