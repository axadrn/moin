package githubrepo

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNowAsksGitHubOnceAnHour(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/repos/" + Repo:
			w.Write([]byte(`{"stargazers_count": 1234}`))
		case "/repos/" + Repo + "/releases":
			w.Write([]byte(`[{"assets": [{"download_count": 40}, {"download_count": 2}]}, {"assets": [{"download_count": 100}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	apiURL = server.URL

	if got := Now(); got != (Numbers{}) {
		t.Fatalf("before the answer: %+v", got)
	}
	want := Numbers{Stars: 1234, Downloads: 142}
	deadline := time.Now().Add(2 * time.Second)
	for Now() != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	for range 100 {
		Now()
	}
	if got := Now(); got != want || calls.Load() != 2 {
		t.Fatalf("numbers = %+v after %d calls, want %+v after 2", got, calls.Load(), want)
	}
	if Stars() != "1.2k" {
		t.Errorf("Stars() = %q", Stars())
	}
}

func TestShort(t *testing.T) {
	for stars, want := range map[int]string{7: "7", 950: "950", 1234: "1.2k", 12345: "12k"} {
		if got := Short(stars); got != want {
			t.Errorf("Short(%d) = %q, want %q", stars, got, want)
		}
	}
}
