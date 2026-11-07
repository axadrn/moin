package handler

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/axadrn/moin/hub/internal/catalog"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// CatalogHandler serves the catalog: its public pages, the install in a
// workspace, and the update of an installed app.
type CatalogHandler struct {
	catalog    *service.CatalogService
	apps       *service.AppService
	servers    *service.ServerService
	workspaces *service.WorkspaceService
	appURL     string
}

func NewCatalogHandler(catalog *service.CatalogService, apps *service.AppService, servers *service.ServerService, workspaces *service.WorkspaceService, appURL string) *CatalogHandler {
	return &CatalogHandler{catalog: catalog, apps: apps, servers: servers, workspaces: workspaces, appURL: appURL}
}

// view is the catalog: the apps of the category in the query, in the
// order it asks.
func (h *CatalogHandler) view(r *http.Request, base string) pages.CatalogView {
	store := h.catalog.Store()
	view := pages.CatalogView{Base: base}
	counts := map[string]int{}
	for _, entry := range store.Entries() {
		counts[entry.Category]++
	}
	for _, category := range catalog.Categories {
		if counts[category] > 0 {
			view.Categories = append(view.Categories, category)
		}
	}
	view.Category = pages.CategoryFromSlug(r.URL.Query().Get("category"), view.Categories)
	view.Sort = pages.SortUpdated
	if r.URL.Query().Get("sort") == pages.SortName {
		view.Sort = pages.SortName
	}
	var all []pages.CatalogEntryView
	for _, entry := range store.Entries() {
		all = append(all, pages.CatalogEntryView{Entry: entry, Versions: store.Versions(entry.Slug)})
	}
	view.Apps = pages.FilterCatalog(all, view.Category, "", view.Sort)
	return view
}

// renderCatalog is the catalog page, or for a choice of its category or
// order (htmx) only the list, with the address bar set to it.
func renderCatalog(w http.ResponseWriter, r *http.Request, view pages.CatalogView, page templ.Component) {
	w.Header().Add("Vary", "HX-Request")
	if isAction(r) {
		w.Header().Set("HX-Replace-Url", view.URL())
		render(w, r, pages.CatalogResults(view))
		return
	}
	render(w, r, page)
}

// entry is the catalog app the path names, with its releases.
func (h *CatalogHandler) entry(r *http.Request) (pages.CatalogEntryView, bool) {
	store := h.catalog.Store()
	entry, ok := store.Entry(r.PathValue("slug"))
	if !ok {
		return pages.CatalogEntryView{}, false
	}
	return pages.CatalogEntryView{Entry: entry, Versions: store.Versions(entry.Slug)}, true
}

// List is the public catalog.
func (h *CatalogHandler) List(w http.ResponseWriter, r *http.Request) {
	view := h.view(r, "/catalog")
	renderCatalog(w, r, view, pages.Catalog(view))
}

// Show is the public page of a catalog app.
func (h *CatalogHandler) Show(w http.ResponseWriter, r *http.Request) {
	view, ok := h.entry(r)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, pages.NotFound())
		return
	}
	render(w, r, pages.CatalogApp(view, h.appURL))
}

// Logo serves an app's logo from the catalog, never from its project's
// site. An SVG runs no script here: the policy forbids it.
func (h *CatalogHandler) Logo(w http.ResponseWriter, r *http.Request) {
	view, ok := h.entry(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if len(view.Entry.Logo) == 0 {
		// An app without a logo gets its first letter, so every icon shows.
		first, _ := utf8.DecodeRuneInString(view.Entry.Name)
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><text x="20" y="27" text-anchor="middle" font-family="system-ui, sans-serif" font-size="22" font-weight="600" fill="#1c1c1e">%s</text></svg>`, html.EscapeString(strings.ToUpper(string(first))))
		return
	}
	w.Header().Set("Content-Type", view.Entry.LogoType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(view.Entry.Logo)
}

// Start sends the "Deploy with moin" button of the site to the
// install form in the user's first workspace.
func (h *CatalogHandler) Start(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	members, err := h.workspaces.ListForUser(user.ID)
	if err != nil || len(members) == 0 {
		redirect(w, r, "/workspaces/new", http.StatusSeeOther)
		return
	}
	redirect(w, r, members[0].Path()+"/catalog/"+r.PathValue("slug"), http.StatusSeeOther)
}

// InApp is the catalog inside a workspace.
func (h *CatalogHandler) InApp(w http.ResponseWriter, r *http.Request) {
	view := h.view(r, ctxkeys.AppBasePath(r.Context())+"/catalog")
	renderCatalog(w, r, view, pages.CatalogInApp(view))
}

// InstallPage is the install form of a catalog app.
func (h *CatalogHandler) InstallPage(w http.ResponseWriter, r *http.Request) {
	view, ok := h.entry(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	servers, err := h.servers.List(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	render(w, r, pages.CatalogInstall(pages.CatalogInstallView{App: view, Servers: servers}))
}

// Install installs the catalog app and opens its page, which follows the
// deploy.
func (h *CatalogHandler) Install(w http.ResponseWriter, r *http.Request) {
	app, _, err := h.catalog.Install(r.Context(), actor(r), service.CatalogInstall{
		Slug: r.PathValue("slug"), Version: r.FormValue("version"), Server: r.FormValue("server"),
		Name: r.FormValue("name"), Domain: r.FormValue("domain"),
	})
	if errors.Is(err, repository.ErrServerNotFound) {
		fieldError(w, "Not installed", "catalog-server", "Choose a server.")
		return
	}
	if err != nil {
		failed(w, r, "Not installed", err, map[string]string{"name": "catalog-name", "domain": "catalog-domain", "version": "catalog-version"})
		return
	}
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/apps/"+app.Link(), http.StatusSeeOther)
}

// UpdatePanel is the catalog panel of an installed app; nothing for an
// app of your own.
func (h *CatalogHandler) UpdatePanel(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.apps.Get(workspace.ID, r.PathValue("app"))
	if err != nil || app == nil {
		return
	}
	if view := catalogUpdate(h.catalog, workspace.ID, app); view != nil {
		render(w, r, pages.CatalogUpdate(*view))
	}
}

// catalogUpdate is the update card of an app from the catalog, nil for
// any other app.
func catalogUpdate(catalogs *service.CatalogService, workspaceID string, app *model.App) *pages.CatalogUpdateView {
	info, ok := catalogs.Info(workspaceID, app)
	if !ok {
		return nil
	}
	versions := catalogs.Store().Versions(info.Entry.Slug)
	if info.Version != "" && !slices.ContainsFunc(versions, func(version catalog.Version) bool { return version.Tag == info.Version }) {
		versions = append([]catalog.Version{{Tag: info.Version}}, versions...)
	}
	return &pages.CatalogUpdateView{App: *app, Entry: info.Entry, Version: info.Version, Latest: info.Latest, Newer: info.Newer, Versions: versions}
}

// Update deploys the app in the picked version and reloads its page.
func (h *CatalogHandler) Update(w http.ResponseWriter, r *http.Request) {
	app, deploy, err := h.catalog.Update(r.Context(), actor(r), r.PathValue("app"), r.FormValue("version"))
	if err != nil {
		failed(w, r, "Not updated", err, nil)
		return
	}
	answerDone(w, r, "Update started", fmt.Sprintf("v%d deploys %s after a backup.", deploy.Number, r.FormValue("version")))
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/apps/"+app.Link(), http.StatusSeeOther)
}
