package client

import (
	"context"
	"net/http"
	"net/url"
)

// Integrations says which keys there are: the Cloudflare API token the
// hub keeps, and the servers that keep a Tailscale key.
type Integrations struct {
	Cloudflare        bool          `json:"cloudflare"`
	CloudflareAccount string        `json:"cloudflare_account"`
	Tailscale         SecretServers `json:"tailscale"`
}

// SecretServers says which servers keep a secret and which do not.
type SecretServers struct {
	Servers   []SecretOn `json:"servers"`
	MissingOn []string   `json:"missing_on"`
}

// Security is how a server should be reached and locked down. SSH is
// tailscale, everyone or off.
type Security struct {
	Tunnel   bool   `json:"tunnel"`
	SSH      string `json:"ssh"`
	Firewall bool   `json:"firewall"`
}

// SetSecurity applies the settings and returns the server with what its
// daemon set up.
func (c *Client) SetSecurity(ctx context.Context, server string, security Security) (Server, error) {
	var answer Server
	err := c.do(ctx, http.MethodPut, "/api/v1/servers/"+url.PathEscape(server)+"/security", security, &answer)
	return answer, err
}

// Integrations says which keys are stored.
func (c *Client) Integrations(ctx context.Context) (Integrations, error) {
	var answer Integrations
	err := c.do(ctx, http.MethodGet, "/api/v1/integrations", nil, &answer)
	return answer, err
}

// SetIntegrations stores keys; a nil one stays, an empty one is removed.
// The Tailscale key goes to server, which keeps it; empty is the
// workspace's only server.
func (c *Client) SetIntegrations(ctx context.Context, cloudflareToken, tailscaleKey *string, server string) (Integrations, error) {
	body := map[string]any{}
	if cloudflareToken != nil {
		body["cloudflare_token"] = cloudflareToken
	}
	if tailscaleKey != nil {
		body["tailscale_key"] = tailscaleKey
		body["server"] = server
	}
	var answer Integrations
	err := c.do(ctx, http.MethodPut, "/api/v1/integrations", body, &answer)
	return answer, err
}
