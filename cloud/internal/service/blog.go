package service

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/markdown"
)

// BlogPost is one article of the blog: a Markdown file in blog/, named
// after its address, with its title, description and date on top.
type BlogPost struct {
	Slug        string
	Title       string
	Description string
	Date        time.Time
	HTML        string
}

// BlogPosts reads blog/ in files, newest first. A file without a title, a
// description or a date stops the hub, so no post goes out half done.
func BlogPosts(files fs.FS) ([]BlogPost, error) {
	names, err := fs.Glob(files, "blog/*.md")
	if err != nil {
		return nil, err
	}
	parser := markdown.NewParser()
	posts := make([]BlogPost, 0, len(names))
	for _, name := range names {
		source, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		html, meta, err := parser.ParseWithFrontmatter(source)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
		text := func(key string) string {
			value, _ := meta[key].(string)
			return strings.TrimSpace(value)
		}
		date, err := time.Parse("2006-01-02", text("date"))
		if err != nil || text("title") == "" || text("description") == "" {
			return nil, fmt.Errorf("%s needs a title, a description and a date like 2026-11-25", name)
		}
		posts = append(posts, BlogPost{
			Slug:        strings.TrimSuffix(path.Base(name), ".md"),
			Title:       text("title"),
			Description: text("description"),
			Date:        date,
			HTML:        string(html),
		})
	}
	slices.SortFunc(posts, func(a, b BlogPost) int { return b.Date.Compare(a.Date) })
	return posts, nil
}

// BlogPostBySlug is one post, or false.
func BlogPostBySlug(posts []BlogPost, slug string) (BlogPost, bool) {
	for _, post := range posts {
		if post.Slug == slug {
			return post, true
		}
	}
	return BlogPost{}, false
}
