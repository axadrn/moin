package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// UIHandler is the website of moin UI.
type UIHandler struct {
	docs  *service.DocsService
	files fs.FS
}

func NewUIHandler(docs *service.DocsService, files fs.FS) *UIHandler {
	return &UIHandler{docs: docs, files: files}
}

// Show answers everything under /ui: the tabs, one block, one component, and
// a page with .md as its Markdown.
func (h *UIHandler) Show(w http.ResponseWriter, r *http.Request) {
	address := strings.Trim(strings.TrimPrefix(r.URL.Path, "/ui"), "/")
	if _, err := h.docs.DocsTree(); err != nil {
		renderError(w, "docs of the UI unavailable", err)
		return
	}
	if strings.HasSuffix(address, ".md") || r.URL.Path == "/ui.md" {
		h.markdown(w, strings.TrimSuffix(address, ".md"))
		return
	}
	switch {
	case address == "":
		h.page(w, r, "start")
	case address == "start":
		http.Redirect(w, r, "/ui", http.StatusFound)
	case address == "components":
		render(w, r, pages.UIComponents(h.components()))
	case address == "layout":
		render(w, r, pages.UILayout(h.layout()))
	case address == "blocks":
		h.blocks(w, r)
	case strings.HasPrefix(address, "blocks/"):
		h.block(w, r, strings.TrimPrefix(address, "blocks/"))
	default:
		h.page(w, r, address)
	}
}

// components are the groups on the Components tab: all but Start and Layout.
func (h *UIHandler) components() []model.DocGroup {
	var groups []model.DocGroup
	for _, group := range h.docs.Groups() {
		if group.Title != "Start" && group.Title != "Layout" {
			groups = append(groups, group)
		}
	}
	return groups
}

func (h *UIHandler) layout() []*model.DocPage {
	for _, group := range h.docs.Groups() {
		if group.Title == "Layout" {
			return group.Pages
		}
	}
	return nil
}

func (h *UIHandler) page(w http.ResponseWriter, r *http.Request, slug string) {
	page, err := h.docs.DocPage(slug)
	if err != nil || len(page.Children) > 0 {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, pages.NotFound())
		return
	}
	switch slug {
	case "start":
		render(w, r, pages.UIStart(page))
	case "theme":
		render(w, r, pages.UITheme(page))
	default:
		_, next := h.docs.PrevNextPages(page)
		render(w, r, pages.UIDoc(page, next))
	}
}

func (h *UIHandler) markdown(w http.ResponseWriter, slug string) {
	if slug == "" {
		slug = "start"
	}
	page, err := h.docs.DocPage(slug)
	if err != nil || len(page.Children) > 0 {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(page.Content))
}

func (h *UIHandler) blocks(w http.ResponseWriter, r *http.Request) {
	groups, err := service.UIBlocks(h.files)
	if err != nil {
		renderError(w, "blocks unavailable", err)
		return
	}
	render(w, r, pages.UIBlocks(groups))
}

func (h *UIHandler) block(w http.ResponseWriter, r *http.Request, slug string) {
	block, err := service.UIBlock(h.files, slug)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, pages.NotFound())
		return
	}
	render(w, r, pages.UIBlock(block))
}
