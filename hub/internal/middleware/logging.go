package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// statusWriter remembers the status of a response.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// RequestLogging logs each request: method, path, status and how long it
// took. Files under /assets/ and the favicon are left out, they would bury
// the pages.
func RequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		written := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(written, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "status", written.status, "duration_ms", time.Since(start).Milliseconds())
	})
}
