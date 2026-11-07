package githubstars

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCountAsksGitHubOnceAnHour(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/repos/"+Repo || r.Header.Get("Authorization") != "" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"stargazers_count": 1234}`))
	}))
	defer server.Close()
	apiURL = server.URL

	if got := Count(); got != 0 {
		t.Fatalf("before the answer: %d", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for Count() != 1234 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	for range 100 {
		Count()
	}
	if got := Count(); got != 1234 || calls.Load() != 1 {
		t.Fatalf("count = %d after %d calls", got, calls.Load())
	}
}

func TestShort(t *testing.T) {
	for stars, want := range map[int]string{7: "7", 950: "950", 1234: "1.2k", 12345: "12k"} {
		if got := Short(stars); got != want {
			t.Errorf("Short(%d) = %q, want %q", stars, got, want)
		}
	}
}
