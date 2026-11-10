package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

type DocsHandler struct {
	docsService *service.DocsService
}

func NewDocsHandler(docsService *service.DocsService) *DocsHandler {
	handler := &DocsHandler{
		docsService: docsService,
	}
	// Docs might be added later: a page that is missing says so.
	_ = handler.docsService.BuildDocsTree()
	return handler
}

// ShowDocs is the docs: /docs the topics with every page, /docs/<slug> one
// page, and either with .md its Markdown. Every link is a full page load.
func (h *DocsHandler) ShowDocs(w http.ResponseWriter, r *http.Request) {
	markdown := strings.HasSuffix(r.URL.Path, ".md")
	slug := strings.Trim(strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, ".md"), "/docs"), "/")

	// The roadmap has a page of its own; its Markdown stays here.
	if slug == "roadmap" && !markdown {
		http.Redirect(w, r, "/roadmap", http.StatusMovedPermanently)
		return
	}

	if _, err := h.docsService.DocsTree(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Docs unavailable", "The docs could not be loaded. Try again."))
		return
	}
	groups := h.docsService.Groups()

	if slug == "" {
		if markdown {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			_, _ = w.Write([]byte(docsIndexMarkdown(groups)))
			return
		}
		render(w, r, pages.DocsHome(groups))
		return
	}
	page, err := h.docsService.DocPage(slug)
	if err != nil {
		if markdown {
			http.Error(w, "Docs page not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		render(w, r, pages.NotFound())
		return
	}
	// A folder (guides, reference) is no page: its pages are on the home.
	if len(page.Children) > 0 {
		http.Redirect(w, r, "/docs", http.StatusFound)
		return
	}
	if markdown {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(page.Content))
		return
	}
	_, next := h.docsService.PrevNextPages(page)
	render(w, r, pages.Docs(page, groups, next))
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
