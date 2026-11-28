package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// On an old domain every request goes on to the new one; the new address
// passes untouched.
func TestOldDomain(t *testing.T) {
	handler := OldDomain([]string{"https://old.example.com"}, "https://moin.example.com/")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	for _, test := range []struct {
		host, path string
		status     int
		location   string
	}{
		{"old.example.com", "/docs?x=1", http.StatusMovedPermanently, "https://moin.example.com/docs?x=1"},
		{"OLD.example.com:443", "/cli.sh", http.StatusMovedPermanently, "https://moin.example.com/cli.sh"},
		{"moin.example.com", "/docs", http.StatusTeapot, ""},
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", "http://"+test.host+test.path, nil))
		if recorder.Code != test.status || recorder.Header().Get("Location") != test.location {
			t.Errorf("%s%s = %d %q", test.host, test.path, recorder.Code, recorder.Header().Get("Location"))
		}
	}
}
