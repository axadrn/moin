package handler

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/report"
)

// knocksCloud is moin cloud on a fresh database with one workspace and
// its server web-1, which reported these knocks, or never reported when
// knocks is nil.
func knocksCloud(t *testing.T, knocks *report.Knocks) (*Cloud, string) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := workspace.NewStore(database)
	watching := watchers.New(store, counts.New(database), mailer.Log{}, "http://site.test")
	space, err := store.SignIn("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.ConnectServer(space.ID, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := watching.Changed(space.ID); err != nil {
		t.Fatal(err)
	}
	if knocks != nil {
		if err := watching.Receive(token, report.Report{Server: "web-1", Version: "v1", EverySeconds: 60, Knocks: knocks}); err != nil {
			t.Fatal(err)
		}
	}
	return NewCloud(store, watching, mailer.Log{}, github.New("", "", "", ""), "http://site.test", false), space.ID
}

// homePage is the landing page with these knocks.
func homePage(t *testing.T, knocks func() *pages.HomeKnocks) string {
	t.Helper()
	w := httptest.NewRecorder()
	numbers := func() *pages.HomeNumbers { return nil }
	Home(os.DirFS("../../content"), knocks, numbers)(w, httptest.NewRequest("GET", "/", nil))
	body, _ := io.ReadAll(w.Result().Body)
	return string(body)
}

func TestHomeShowsTheKnocksOfTheSiteServer(t *testing.T) {
	cloud, id := knocksCloud(t, &report.Knocks{Today: 4812, Year: 90_210, Total: 120_000, TodayByName: map[string]int64{"ssh": 4000, "postgres": 812}})
	page := homePage(t, cloud.SiteKnocks(id, "web-1", false))
	for _, want := range []string{`id="knocks"`, "4,812", "90,210", "SSH", "Postgres"} {
		if !strings.Contains(page, want) {
			t.Errorf("the landing page has no %q", want)
		}
	}
	if strings.Contains(page, "Sample data") {
		t.Error("real knocks are marked as sample data")
	}
}

// TestProductionNeverShowsSampleKnocks: without a count, production shows
// no knocks at all, never made-up ones.
func TestProductionNeverShowsSampleKnocks(t *testing.T) {
	cloud, id := knocksCloud(t, nil)
	for name, knocks := range map[string]func() *pages.HomeKnocks{
		"not configured":  cloud.SiteKnocks("", "", false),
		"no count":        cloud.SiteKnocks(id, "web-1", false),
		"another server":  cloud.SiteKnocks(id, "prod", false),
		"wrong workspace": cloud.SiteKnocks("nope", "web-1", false),
	} {
		page := homePage(t, knocks)
		if strings.Contains(page, `id="knocks"`) || strings.Contains(page, "Sample data") {
			t.Errorf("%s: the landing page shows knocks", name)
		}
	}
	if page := homePage(t, cloud.SiteKnocks(id, "web-1", true)); !strings.Contains(page, "Sample data · development only") {
		t.Error("development shows no marked sample knocks")
	}
}

// TestProductionNeverShowsSampleNumbers: below 100 downloads production
// shows no numbers; development shows marked sample numbers.
func TestProductionNeverShowsSampleNumbers(t *testing.T) {
	cloud, _ := knocksCloud(t, nil)
	if numbers := cloud.SiteNumbers(false)(); numbers != nil {
		t.Errorf("production numbers = %+v, want none", numbers)
	}
	if numbers := cloud.SiteNumbers(true)(); numbers == nil || !numbers.Sample {
		t.Error("development shows no marked sample numbers")
	}
}
