package model

import "time"

// GitHubApp is the GitHub App an admin created for the hub with one click,
// as the database keeps it. ClientSecret, WebhookSecret and PrivateKey are
// sealed with SECRETS_KEY.
type GitHubApp struct {
	AppID         string    `db:"app_id"`
	Slug          string    `db:"slug"`
	ClientID      string    `db:"client_id"`
	ClientSecret  string    `db:"client_secret"`
	WebhookSecret string    `db:"webhook_secret"`
	PrivateKey    string    `db:"private_key"`
	CreatedBy     *string   `db:"created_by"`
	CreatedAt     time.Time `db:"created_at"`
}

// GitHubAppManifest is the form that creates the hub's GitHub App on
// GitHub: Action is where it posts, with the state, and Manifest the JSON
// that says what the app is.
type GitHubAppManifest struct {
	Action   string
	Manifest string
}
