package client

import (
	"context"
	"net/http"
)

// Integrations says which keys are stored.
type Integrations struct {
	Cloudflare        bool   `json:"cloudflare"`
	CloudflareAccount string `json:"cloudflare_account"`
	Tailscale         bool   `json:"tailscale"`
}

// Security is how a server should be reached and locked down.
type Security struct {
	Tunnel       bool `json:"tunnel"`
	SSHTailscale bool `json:"ssh_tailscale"`
	Firewall     bool `json:"firewall"`
}

// SetSecurity applies the settings and returns the server with what its
// agent set up.
func (c *Client) SetSecurity(ctx context.Context, server string, security Security) (Server, error) {
	var answer Server
	err := c.do(ctx, http.MethodPut, "/api/v1/servers/"+server+"/security", security, &answer)
	return answer, err
}

// Integrations says which keys are stored.
func (c *Client) Integrations(ctx context.Context) (Integrations, error) {
	var answer Integrations
	err := c.do(ctx, http.MethodGet, "/api/v1/integrations", nil, &answer)
	return answer, err
}

// SetIntegrations stores keys; a nil one stays, an empty one is removed.
func (c *Client) SetIntegrations(ctx context.Context, cloudflareToken, tailscaleKey *string) (Integrations, error) {
	body := map[string]*string{}
	if cloudflareToken != nil {
		body["cloudflare_token"] = cloudflareToken
	}
	if tailscaleKey != nil {
		body["tailscale_key"] = tailscaleKey
	}
	var answer Integrations
	err := c.do(ctx, http.MethodPut, "/api/v1/integrations", body, &answer)
	return answer, err
}
