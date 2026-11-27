package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Changelog shows what shipped, newest first. It reads the file on every
// request, so a new release shows without a restart in development.
func Changelog(files fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releases, err := service.ChangelogReleases(files)
		if err != nil {
			slog.Error("changelog failed", "error", err)
			w.WriteHeader(http.StatusNotFound)
			render(w, r, pages.NotFound())
			return
		}
		render(w, r, pages.Changelog(releases))
	}
}

// ChangelogFeed is the changelog as RSS, one item per release.
func ChangelogFeed(files fs.FS, appURL, name string) http.HandlerFunc {
	base := strings.TrimRight(appURL, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		releases, err := service.ChangelogReleases(files)
		if err != nil {
			slog.Error("changelog feed failed", "error", err)
			http.Error(w, "no changelog", http.StatusNotFound)
			return
		}
		var items []feedItem
		for _, release := range releases {
			items = append(items, feedItem{
				Title:       release.Version,
				Link:        base + "/changelog#" + release.Anchor(),
				GUID:        base + "/changelog#" + release.Version,
				PubDate:     feedDate(release.Date),
				Description: release.InShort,
			})
		}
		writeFeed(w, name+" changelog", base+"/changelog", "What shipped in "+name+", newest first.", items)
	}
}
