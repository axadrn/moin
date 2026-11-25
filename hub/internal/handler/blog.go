package handler

import (
	"encoding/xml"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Blog lists the posts, newest first. Like the changelog it reads the files
// on every request, so a new post shows without a restart in development.
func Blog(files fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		posts, err := service.BlogPosts(files)
		if err != nil {
			renderError(w, "blog unavailable", err)
			return
		}
		render(w, r, pages.Blog(posts))
	}
}

// BlogPost shows one post at /blog/{slug}.
func BlogPost(files fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		posts, err := service.BlogPosts(files)
		if err != nil {
			renderError(w, "blog unavailable", err)
			return
		}
		post, ok := service.BlogPostBySlug(posts, r.PathValue("slug"))
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			render(w, r, pages.NotFound())
			return
		}
		render(w, r, pages.BlogPost(post, posts))
	}
}

// BlogFeed is the blog as RSS, one item per post.
func BlogFeed(files fs.FS, appURL, name string) http.HandlerFunc {
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
		posts, err := service.BlogPosts(files)
		if err != nil {
			slog.Error("blog feed failed", "error", err)
			http.Error(w, "no blog", http.StatusNotFound)
			return
		}
		feed := rss{Version: "2.0", Channel: channel{Title: name + " blog", Link: base + "/blog", Description: "Deploying, servers, backups and security, from the people who build " + name + "."}}
		for _, post := range posts {
			feed.Channel.Items = append(feed.Channel.Items, item{
				Title:       post.Title,
				Link:        base + "/blog/" + post.Slug,
				GUID:        base + "/blog/" + post.Slug,
				PubDate:     post.Date.UTC().Format(time.RFC1123Z),
				Description: post.Description,
			})
		}
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=600")
		_, _ = w.Write([]byte(xml.Header))
		_ = xml.NewEncoder(w).Encode(feed)
	}
}
