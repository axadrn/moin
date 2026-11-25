package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Connection is a client let in from the browser, as the API answers it.
type Connection struct {
	ID          string     `json:"id"`
	ClientName  string     `json:"client_name"`
	WorkspaceID string     `json:"workspace_id"`
	Rights      []string   `json:"rights"`
	Current     bool       `json:"current"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CIToken is a token made by hand for CI, as the API answers it.
type CIToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Rights     []string   `json:"rights"`
	App        *string    `json:"app"`
	CreatedBy  string     `json:"created_by"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Expired    bool       `json:"expired"`
}

// Connections lists the connections of the workspace this login is in,
// this one marked Current.
func (c *Client) Connections(ctx context.Context) ([]Connection, error) {
	var response struct {
		Items []Connection `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/account/connections", nil, &response)
	return response.Items, err
}

// RemoveConnection ends a connection and its tokens.
func (c *Client) RemoveConnection(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/account/connections/"+url.PathEscape(id), nil, nil)
}

// CITokens lists the CI tokens of the workspace you see.
func (c *Client) CITokens(ctx context.Context) ([]CIToken, error) {
	var response struct {
		Items []CIToken `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/ci-tokens", nil, &response)
	return response.Items, err
}

// RemoveCIToken deletes a CI token.
func (c *Client) RemoveCIToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/ci-tokens/"+url.PathEscape(id), nil, nil)
}
