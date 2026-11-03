package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// AlertDestination is where the alerts of the workspace go besides email:
// Slack, Discord, Telegram or a webhook. Hint shows its host and last
// characters, never the URL or token.
type AlertDestination struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Hint         string     `json:"hint"`
	ChatID       string     `json:"chat_id"`
	Events       []string   `json:"events"`
	Failing      bool       `json:"failing"`
	FailingSince *time.Time `json:"failing_since"`
	LastError    string     `json:"last_error"`
	LastSentAt   *time.Time `json:"last_sent_at"`
	CreatedAt    time.Time  `json:"created_at"`
	// SigningSecret comes only with a new webhook.
	SigningSecret string `json:"signing_secret,omitempty"`
}

// NewAlertDestination is a destination to add. URL is for Slack, Discord
// and webhooks; BotToken and ChatID for Telegram. No events means all.
type NewAlertDestination struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	URL      string   `json:"url,omitempty"`
	BotToken string   `json:"bot_token,omitempty"`
	ChatID   string   `json:"chat_id,omitempty"`
	Events   []string `json:"events,omitempty"`
}

// AlertDestinations lists the alert destinations of the workspace.
func (c *Client) AlertDestinations(ctx context.Context) ([]AlertDestination, error) {
	var response struct {
		Items []AlertDestination `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/alert-destinations", nil, &response)
	return response.Items, err
}

// AddAlertDestination adds an alert destination; a webhook comes back with
// its signing secret.
func (c *Client) AddAlertDestination(ctx context.Context, destination NewAlertDestination) (AlertDestination, error) {
	var added AlertDestination
	err := c.do(ctx, http.MethodPost, "/api/v1/alert-destinations", destination, &added)
	return added, err
}

// RemoveAlertDestination removes an alert destination by name or ID.
func (c *Client) RemoveAlertDestination(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/alert-destinations/"+url.PathEscape(name), nil, nil)
}

// TestAlertDestination sends a test alert to a destination.
func (c *Client) TestAlertDestination(ctx context.Context, name string) (AlertDestination, error) {
	var tested AlertDestination
	err := c.do(ctx, http.MethodPost, "/api/v1/alert-destinations/"+url.PathEscape(name)+"/test", nil, &tested)
	return tested, err
}
