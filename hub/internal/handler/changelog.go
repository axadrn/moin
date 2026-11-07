package handler

import (
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Changelog shows what shipped, newest first. It reads the file on every
// request, so a new release shows without a restart in development.
func Changelog(contentDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		html, err := service.Changelog(contentDir)
		if err != nil {
			slog.Error("changelog failed", "error", err)
			w.WriteHeader(http.StatusNotFound)
			render(w, r, pages.NotFound())
			return
		}
		render(w, r, pages.Changelog(html))
	}
}

// ChangelogFeed is the changelog as RSS, one item per release, so a reader
// or a Slack feed hears about each one.
func ChangelogFeed(contentDir, appURL, name string) http.HandlerFunc {
	type item struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		GUID        string `xml:"guid"`
		PubDate     string `xml:"pubDate"`
		Description string `xml:"description"`
	}
	type channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Items       []item `xml:"item"`
	}
	type rss struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Channel channel  `xml:"channel"`
	}
	base := strings.TrimRight(appURL, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		releases, err := service.ChangelogReleases(contentDir)
		if err != nil {
			slog.Error("changelog feed failed", "error", err)
			http.Error(w, "no changelog", http.StatusNotFound)
			return
		}
		feed := rss{Version: "2.0", Channel: channel{Title: name + " changelog", Link: base + "/changelog", Description: "What shipped in " + name + ", newest first."}}
		for _, release := range releases {
			anchor := strings.NewReplacer(".", "").Replace(release.Version)
			date := release.Date
			if date.IsZero() {
				date = time.Now()
			}
			feed.Channel.Items = append(feed.Channel.Items, item{
				Title:       release.Version,
				Link:        base + "/changelog#" + anchor,
				GUID:        base + "/changelog#" + release.Version,
				PubDate:     date.UTC().Format(time.RFC1123Z),
				Description: release.InShort,
			})
		}
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=600")
		_, _ = w.Write([]byte(xml.Header))
		_ = xml.NewEncoder(w).Encode(feed)
	}
}
