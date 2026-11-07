package pages

import (
	"testing"

	"github.com/axadrn/moin/hub/internal/catalog"
)

func TestFilterCatalogFindsLikeThePalette(t *testing.T) {
	apps := []CatalogEntryView{
		{Entry: catalog.Entry{Name: "ntfy", Category: "Communication", Pitch: "Send notifications to your phone with a single curl."}},
		{Entry: catalog.Entry{Name: "Gotify", Category: "Communication", Pitch: "Push notifications from your scripts."}},
		{Entry: catalog.Entry{Name: "Umami", Category: "Analytics", Pitch: "Privacy friendly web analytics."}},
		// Its letters hold n, f, t, y across words: no match for nfty.
		{Entry: catalog.Entry{Name: "Nextcloud", Category: "Productivity", Pitch: "Files, calendar and contacts, like Google Drive, on your server."}},
	}
	names := func(kept []CatalogEntryView) []string {
		var out []string
		for _, app := range kept {
			out = append(out, app.Entry.Name)
		}
		return out
	}
	for query, want := range map[string][]string{
		"nfty":          {"ntfy"},
		"analytics":     {"Umami"},
		"notifications": {"Gotify", "ntfy"},
		"":              {"Gotify", "Nextcloud", "ntfy", "Umami"},
		"files drive":   {"Nextcloud"},
	} {
		got := names(FilterCatalog(apps, "", query, SortName))
		if len(got) != len(want) {
			t.Errorf("%q finds %v, want %v", query, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q finds %v, want %v", query, got, want)
				break
			}
		}
	}
	if got := names(FilterCatalog(apps, "Analytics", "", SortName)); len(got) != 1 || got[0] != "Umami" {
		t.Errorf("category Analytics keeps %v", got)
	}
	// A match on the name comes before one on the pitch.
	apps = append(apps, CatalogEntryView{Entry: catalog.Entry{Name: "Notifier", Pitch: "Sends notes."}})
	if got := names(FilterCatalog(apps, "", "notif", SortName)); got[0] != "Notifier" {
		t.Errorf("notif finds %v, the name first", got)
	}
}

func TestImageURLOpensTheRegistryPage(t *testing.T) {
	for image, want := range map[string]string{
		"wordpress":                     "https://hub.docker.com/_/wordpress",
		"grafana/grafana":               "https://hub.docker.com/r/grafana/grafana",
		"docker.io/library/postgres":    "https://hub.docker.com/_/postgres",
		"ghcr.io/umami-software/umami":  "https://ghcr.io/umami-software/umami",
		"quay.io/prometheus/prometheus": "https://quay.io/repository/prometheus/prometheus",
	} {
		if got := imageURL(image); got != want {
			t.Errorf("imageURL(%q) = %q, want %q", image, got, want)
		}
	}
	if got := websiteHost("https://www.activepieces.com"); got != "activepieces.com" {
		t.Errorf("websiteHost = %q", got)
	}
}
