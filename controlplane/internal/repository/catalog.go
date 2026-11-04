package repository

import (
	"encoding/json"
	"time"

	"github.com/axadrn/moin/controlplane/internal/catalog"
	"github.com/jmoiron/sqlx"
)

// CatalogCache keeps the releases of the catalog apps in history.db, so a
// restart serves them at once. Lost, they are read from the registries
// again.
type CatalogCache struct {
	history *sqlx.DB
}

func NewCatalogCache(history *sqlx.DB) *CatalogCache {
	return &CatalogCache{history: history}
}

// Load is every cached entry by slug.
func (c *CatalogCache) Load() (map[string]catalog.Cached, error) {
	var rows []struct {
		Slug      string `db:"slug"`
		Versions  string `db:"versions"`
		CheckedAt int64  `db:"checked_at"`
	}
	err := c.history.Select(&rows, `SELECT slug, versions, checked_at FROM catalog_versions`)
	if err != nil {
		return nil, err
	}
	cached := make(map[string]catalog.Cached, len(rows))
	for _, row := range rows {
		var versions []catalog.Version
		if json.Unmarshal([]byte(row.Versions), &versions) != nil {
			continue
		}
		cached[row.Slug] = catalog.Cached{Versions: versions, Checked: time.Unix(row.CheckedAt, 0)}
	}
	return cached, nil
}

// Save stores the releases of slug.
func (c *CatalogCache) Save(slug string, cached catalog.Cached) error {
	body, err := json.Marshal(cached.Versions)
	if err != nil {
		return err
	}
	_, err = c.history.Exec(`INSERT INTO catalog_versions (slug, versions, checked_at) VALUES ($1, $2, $3)
		ON CONFLICT (slug) DO UPDATE SET versions = excluded.versions, checked_at = excluded.checked_at`, slug, string(body), cached.Checked.Unix())
	return err
}
