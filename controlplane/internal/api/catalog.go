package api

import (
	"net/http"
	"time"

	"github.com/axadrn/ops/controlplane/internal/catalog"
	"github.com/axadrn/ops/controlplane/internal/service"
)

// CatalogHandler lists the catalog, installs its apps and updates them.
type CatalogHandler struct {
	catalog *service.CatalogService
}

func NewCatalogHandler(catalog *service.CatalogService) *CatalogHandler {
	return &CatalogHandler{catalog: catalog}
}

// CatalogVersion is a release of a catalog app.
type CatalogVersion struct {
	Tag  string     `json:"tag"`
	Date *time.Time `json:"date"`
}

// CatalogEntry is the public JSON shape of a catalog app. Versions is only
// set on the single answer; Latest is null before its registry answered.
type CatalogEntry struct {
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Pitch    string `json:"pitch"`
	Category string `json:"category"`
	Website  string `json:"website"`
	Source   string `json:"source"`
	License  string `json:"license"`
	Image    string `json:"image"`
	// Database is postgres or sqlite, which ops backs up, or mysql,
	// mongodb or clickhouse, which it does not yet.
	Database string `json:"database"`
	// DatabaseNote says so for a database ops does not back up; empty
	// otherwise.
	DatabaseNote string `json:"database_note"`
	Port         int    `json:"port"`
	// Login says how to sign in the first time.
	Login string `json:"login"`
	// NeedsDomain says the install needs a domain: ops sets APP_URL from it.
	NeedsDomain bool `json:"needs_domain"`
	// Secrets are the env vars ops generates on install.
	Secrets  []string         `json:"secrets"`
	Latest   *CatalogVersion  `json:"latest"`
	Versions []CatalogVersion `json:"versions,omitempty"`
}

// CatalogList is the answer of GET /api/v1/catalog.
type CatalogList struct {
	Items []CatalogEntry `json:"items"`
}

// CatalogInstallRequest installs a catalog app. Version, name and domain
// are optional: the newest release, the slug, no domain.
type CatalogInstallRequest struct {
	Server  string `json:"server"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Domain  string `json:"domain"`
}

// AppUpdateRequest updates a catalog app; an empty version is the newest.
type AppUpdateRequest struct {
	Version string `json:"version"`
}

// CatalogDeploy is the answer of an install or an update: the app and
// its new deploy, which streams like every deploy.
type CatalogDeploy struct {
	App    App    `json:"app"`
	Deploy Deploy `json:"deploy"`
}

func newCatalogVersion(version catalog.Version) CatalogVersion {
	out := CatalogVersion{Tag: version.Tag}
	if !version.Date.IsZero() {
		date := version.Date.UTC()
		out.Date = &date
	}
	return out
}

// NewCatalogEntry is entry with its newest release; withVersions adds them all.
func NewCatalogEntry(store *catalog.Store, entry catalog.Entry, withVersions bool) CatalogEntry {
	out := CatalogEntry{
		Slug: entry.Slug, Name: entry.Name, Pitch: entry.Pitch, Category: entry.Category,
		Website: entry.Website, Source: entry.Source, License: entry.License, Image: entry.Image,
		Database: entry.Database, DatabaseNote: entry.DatabaseNote(), NeedsDomain: entry.NeedsURL(), Port: entry.Port, Login: entry.Login, Secrets: entry.Secrets,
	}
	if out.Secrets == nil {
		out.Secrets = []string{}
	}
	if latest, ok := store.Latest(entry.Slug); ok {
		version := newCatalogVersion(latest)
		out.Latest = &version
	}
	if withVersions {
		out.Versions = []CatalogVersion{}
		for _, version := range store.Versions(entry.Slug) {
			out.Versions = append(out.Versions, newCatalogVersion(version))
		}
	}
	return out
}

// List returns every app of the catalog with its newest release.
func (h *CatalogHandler) List(w http.ResponseWriter, _ *http.Request) {
	store := h.catalog.Store()
	list := CatalogList{Items: []CatalogEntry{}}
	for _, entry := range store.Entries() {
		list.Items = append(list.Items, NewCatalogEntry(store, entry, false))
	}
	JSON(w, http.StatusOK, list)
}

// Get returns a catalog app with every release, newest first.
func (h *CatalogHandler) Get(w http.ResponseWriter, r *http.Request) {
	store := h.catalog.Store()
	entry, ok := store.Entry(r.PathValue("slug"))
	if !ok {
		ServiceError(w, service.ErrCatalogApp)
		return
	}
	JSON(w, http.StatusOK, NewCatalogEntry(store, entry, true))
}

// Install creates the app from the catalog and deploys it.
func (h *CatalogHandler) Install(w http.ResponseWriter, r *http.Request) {
	var request CatalogInstallRequest
	if !Decode(w, r, &request) {
		return
	}
	app, deploy, err := h.catalog.Install(r.Context(), actor(r), service.CatalogInstall{
		Slug: r.PathValue("slug"), Version: request.Version, Server: request.Server, Name: request.Name, Domain: request.Domain,
	})
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, CatalogDeploy{App: NewApp(*app), Deploy: NewDeploy(*deploy)})
}

// Update sets a catalog app to a version and deploys it.
func (h *CatalogHandler) Update(w http.ResponseWriter, r *http.Request) {
	var request AppUpdateRequest
	if !Decode(w, r, &request) {
		return
	}
	app, deploy, err := h.catalog.Update(r.Context(), actor(r), r.PathValue("app"), request.Version)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, CatalogDeploy{App: NewApp(*app), Deploy: NewDeploy(*deploy)})
}
