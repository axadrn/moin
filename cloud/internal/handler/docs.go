package handler

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/axadrn/moin/cloud/internal/model"
	"github.com/axadrn/moin/cloud/internal/service"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// Docs is the docs: /docs the topics with every page, /docs/<slug> one
// page, and either with .md its Markdown. Every link is a full page load.
func Docs(docs *service.DocsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// /docs/page/ goes to /docs/page, so every page has one address.
		if path := r.URL.Path; len(path) > 1 && strings.HasSuffix(path, "/") {
			target := strings.TrimRight(path, "/")
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		markdown := strings.HasSuffix(r.URL.Path, ".md")
		slug := strings.Trim(strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, ".md"), "/docs"), "/")
		// The roadmap has a page of its own; its Markdown stays here.
		if slug == "roadmap" && !markdown {
			http.Redirect(w, r, "/roadmap", http.StatusMovedPermanently)
			return
		}
		groups, err := docs.Groups()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			render(w, r, pages.StatusPage("Docs unavailable", "The docs could not be loaded. Try again.", "500 internal server error"))
			return
		}
		if slug == "" {
			if markdown {
				writeMarkdown(w, docsIndexMarkdown(groups))
				return
			}
			render(w, r, pages.DocsHome(groups))
			return
		}
		all, _ := docs.Pages()
		at := slices.IndexFunc(all, func(page *model.DocPage) bool { return page.Slug == slug })
		switch {
		case at < 0 && markdown:
			http.Error(w, "Docs page not found", http.StatusNotFound)
		case at < 0:
			NotFound(w, r)
		case markdown:
			writeMarkdown(w, all[at].Content)
		default:
			var next *model.DocPage
			if at+1 < len(all) {
				next = all[at+1]
			}
			render(w, r, pages.Docs(all[at], groups, next))
		}
	}
}

// writeMarkdown answers with a page as Markdown, for AI tools. It is a copy
// of the HTML page, so it stays out of search.
func writeMarkdown(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	_, _ = w.Write([]byte(content))
}

// docsIndexMarkdown is /docs.md: every page under its topic, linked to its
// Markdown.
func docsIndexMarkdown(groups []model.DocGroup) string {
	var b strings.Builder
	b.WriteString("# Docs\n")
	for _, group := range groups {
		fmt.Fprintf(&b, "\n## %s\n\n", group.Title)
		for _, page := range group.Pages {
			fmt.Fprintf(&b, "- [%s](/docs/%s.md)", page.Title, page.Slug)
			if page.Description != "" {
				fmt.Fprintf(&b, ": %s", page.Description)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
