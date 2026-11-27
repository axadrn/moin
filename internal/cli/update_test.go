package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestUpdateCheckOncePerDay asks GitHub once, keeps the answer for a day,
// and asks again after it.
func TestUpdateCheckOncePerDay(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	asked := 0
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		http.Redirect(w, r, "/axadrn/moin/releases/tag/v1.2.3", http.StatusFound)
	}))
	defer github.Close()
	releasesURL = github.URL + "/axadrn/moin/releases/latest"

	now := time.Now()
	for _, at := range []time.Time{now, now.Add(time.Hour), now.Add(23 * time.Hour)} {
		if got := latestRelease(t.Context(), at); got != "v1.2.3" {
			t.Fatalf("latest = %q", got)
		}
	}
	if asked != 1 {
		t.Fatalf("asked GitHub %d times in a day, want once", asked)
	}
	latestRelease(t.Context(), now.Add(25*time.Hour))
	if asked != 2 {
		t.Fatalf("asked GitHub %d times after a day, want twice", asked)
	}
}

// TestUpdateCheckOffline keeps the last answer when GitHub does not
// answer, and waits a day before it asks again.
func TestUpdateCheckOffline(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	file, _ := checkFile()
	now := time.Now()
	writeCheck(file, updateCheck{Checked: now.Add(-48 * time.Hour), Latest: "v1.0.0"})
	releasesURL = "http://127.0.0.1:1/releases/latest"
	if got := latestRelease(t.Context(), now); got != "v1.0.0" {
		t.Errorf("offline latest = %q, want the last answer", got)
	}
	if checkDue(readCheck(file), now.Add(time.Hour)) {
		t.Error("a failed check is due again within the hour, want a day")
	}
}
