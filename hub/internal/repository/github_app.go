package repository

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/jmoiron/sqlx"
)

// ErrGitHubAppExists says the hub has its GitHub App already.
var ErrGitHubAppExists = errors.New("the hub has a GitHub App already")

// GitHubApps keeps the one GitHub App of the hub that an admin created.
type GitHubApps struct {
	db *sqlx.DB
}

func NewGitHubApps(db *sqlx.DB) *GitHubApps {
	return &GitHubApps{db: db}
}

// Get is the hub's GitHub App, nil without one.
func (r *GitHubApps) Get() (*model.GitHubApp, error) {
	var app model.GitHubApp
	err := r.db.Get(&app, `SELECT app_id, slug, client_id, client_secret, webhook_secret, private_key, created_by, created_at FROM github_app WHERE id = 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// Create stores the app, once: a second one is ErrGitHubAppExists.
func (r *GitHubApps) Create(app *model.GitHubApp) error {
	_, err := r.db.Exec(`INSERT INTO github_app (id, app_id, slug, client_id, client_secret, webhook_secret, private_key, created_by, created_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8)`,
		app.AppID, app.Slug, app.ClientID, app.ClientSecret, app.WebhookSecret, app.PrivateKey, app.CreatedBy, app.CreatedAt)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "PRIMARY KEY")) {
		return ErrGitHubAppExists
	}
	return err
}
