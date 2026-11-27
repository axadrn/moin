package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

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
	base := strings.TrimRight(appURL, "/")
	return func(w http.ResponseWriter, r *http.Request) {
		posts, err := service.BlogPosts(files)
		if err != nil {
			slog.Error("blog feed failed", "error", err)
			http.Error(w, "no blog", http.StatusNotFound)
			return
		}
		var items []feedItem
		for _, post := range posts {
			link := base + "/blog/" + post.Slug
			items = append(items, feedItem{Title: post.Title, Link: link, GUID: link, PubDate: feedDate(post.Date), Description: post.Description})
		}
		writeFeed(w, name+" blog", base+"/blog", "Deploying, servers, backups and security, from the people who build "+name+".", items)
	}
}
