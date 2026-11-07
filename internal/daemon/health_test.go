package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthRedirect: an app that redirects to https, as Ghost does, is up.
// Following the redirect would leave the container's address.
func TestHealthRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://blog.invalid"+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer server.Close()
	if err := healthy(context.Background(), server.URL+"/", "/"); err != nil {
		t.Fatalf("a redirect failed the health check: %v", err)
	}
}
