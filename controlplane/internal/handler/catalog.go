package handler

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/catalog"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// CatalogHandler serves the catalog: its public pages, the install in a
// workspace, and the update of an installed app.
type CatalogHandler struct {
	catalog    *service.CatalogService
	servers    *service.ServerService
	workspaces *service.WorkspaceService
	appURL     string
}

func NewCatalogHandler(catalog *service.CatalogService, servers *service.ServerService, workspaces *service.WorkspaceService, appURL string) *CatalogHandler {
	return &CatalogHandler{catalog: catalog, servers: servers, workspaces: workspaces, appURL: appURL}
}

// view is the catalog, filtered by the category in the query.
func (h *CatalogHandler) view(r *http.Request, base string) pages.CatalogView {
	store := h.catalog.Store()
	view := pages.CatalogView{Base: base}
	for _, category := range catalog.Categories {
		for _, entry := range store.Entries() {
			if entry.Category == category {
				view.Categories = append(view.Categories, category)
				break
			}
		}
	}
	view.Category = pages.CategoryFromSlug(r.URL.Query().Get("category"), view.Categories)
	view.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	view.Sort = pages.SortUpdated
	if r.URL.Query().Get("sort") == pages.SortName {
		view.Sort = pages.SortName
	}
	var all []pages.CatalogEntryView
	for _, entry := range store.Entries() {
		all = append(all, pages.CatalogEntryView{Entry: entry, Versions: store.Versions(entry.Slug)})
	}
	// Every app of the category stays in the page: the search hides
	// the others, so clearing it brings them back without a reload.
	view.Apps = pages.FilterCatalog(all, view.Category, "", view.Sort)
	return view
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
	render(w, r, pages.Catalog(h.view(r, "/apps")))
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

// Start sends the "Install on your server" button of the site to the
// install form in the user's first workspace.
func (h *CatalogHandler) Start(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	members, err := h.workspaces.ListForUser(user.ID)
	if err != nil || len(members) == 0 {
		redirect(w, r, "/workspaces/new", http.StatusSeeOther)
		return
	}
	redirect(w, r, fmt.Sprintf("/app/%s/catalog/%s", members[0].Slug, r.PathValue("slug")), http.StatusSeeOther)
}

// InApp is the catalog inside a workspace.
func (h *CatalogHandler) InApp(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.CatalogInApp(h.view(r, ctxkeys.AppBasePath(r.Context())+"/catalog")))
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
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/apps/"+app.Name, http.StatusSeeOther)
}

// UpdatePanel is the catalog panel of an installed app; nothing for an
// app of your own.
func (h *CatalogHandler) UpdatePanel(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, err := h.catalog.App(workspace.ID, r.PathValue("app"))
	if err != nil || app == nil {
		return
	}
	info, ok := h.catalog.Info(workspace.ID, app)
	if !ok {
		return
	}
	versions := h.catalog.Store().Versions(info.Entry.Slug)
	if info.Version != "" && !slices.ContainsFunc(versions, func(version catalog.Version) bool { return version.Tag == info.Version }) {
		versions = append([]catalog.Version{{Tag: info.Version}}, versions...)
	}
	render(w, r, pages.CatalogUpdate(pages.CatalogUpdateView{App: *app, Entry: info.Entry, Version: info.Version, Latest: info.Latest, Newer: info.Newer, Versions: versions}))
}

// Update deploys the app in the picked version and reloads its page.
func (h *CatalogHandler) Update(w http.ResponseWriter, r *http.Request) {
	app, deploy, err := h.catalog.Update(r.Context(), actor(r), r.PathValue("app"), r.FormValue("version"))
	if err != nil {
		failed(w, r, "Not updated", err, nil)
		return
	}
	toastDone(w, r, "Update started", fmt.Sprintf("v%d deploys %s after a snapshot.", deploy.Number, r.FormValue("version")))
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/apps/"+app.Name, http.StatusSeeOther)
}
