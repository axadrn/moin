package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// CatalogVersion is a release of a catalog app; Date is nil when the
// registry does not say.
type CatalogVersion struct {
	Tag  string     `json:"tag"`
	Date *time.Time `json:"date"`
}

// CatalogEntry is an open source app ops installs in one click. Versions
// is only set by CatalogApp.
type CatalogEntry struct {
	Slug     string           `json:"slug"`
	Name     string           `json:"name"`
	Pitch    string           `json:"pitch"`
	Category string           `json:"category"`
	Website  string           `json:"website"`
	Source   string           `json:"source"`
	License  string           `json:"license"`
	Image    string           `json:"image"`
	Database string           `json:"database"`
	Port     int              `json:"port"`
	Login    string           `json:"login"`
	Secrets  []string         `json:"secrets"`
	Latest   *CatalogVersion  `json:"latest"`
	Versions []CatalogVersion `json:"versions"`
}

// CatalogInstall installs a catalog app; the empty fields take the newest
// release, the slug and no domain.
type CatalogInstall struct {
	Server  string `json:"server"`
	Version string `json:"version,omitempty"`
	Name    string `json:"name,omitempty"`
	Domain  string `json:"domain,omitempty"`
}

// CatalogDeploy is an installed or updated app and its started deploy.
type CatalogDeploy struct {
	App    App    `json:"app"`
	Deploy Deploy `json:"deploy"`
}

// Catalog lists the catalog with each app's newest release.
func (c *Client) Catalog(ctx context.Context) ([]CatalogEntry, error) {
	var response struct {
		Items []CatalogEntry `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/catalog", nil, &response)
	return response.Items, err
}

// CatalogApp returns a catalog app with every release, newest first.
func (c *Client) CatalogApp(ctx context.Context, slug string) (CatalogEntry, error) {
	var response CatalogEntry
	err := c.do(ctx, http.MethodGet, "/api/v1/catalog/"+url.PathEscape(slug), nil, &response)
	return response, err
}

// InstallCatalogApp creates the app from the catalog and deploys it.
func (c *Client) InstallCatalogApp(ctx context.Context, slug string, install CatalogInstall) (CatalogDeploy, error) {
	var response CatalogDeploy
	err := c.do(ctx, http.MethodPost, "/api/v1/catalog/"+url.PathEscape(slug)+"/install", install, &response)
	return response, err
}

// UpdateApp sets a catalog app to version, the newest when empty, and
// deploys it.
func (c *Client) UpdateApp(ctx context.Context, app, version string) (CatalogDeploy, error) {
	var response CatalogDeploy
	err := c.do(ctx, http.MethodPost, appPath(app, "update"), map[string]string{"version": version}, &response)
	return response, err
}
