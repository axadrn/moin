package model

import "time"

// Server is a machine with a moin agent. Online is not stored: the agent hub
// knows which servers hold a session right now.
type Server struct {
	ID               string     `db:"id"`
	WorkspaceID      string     `db:"workspace_id"`
	Name             string     `db:"name"`
	InstallTokenHash *string    `db:"install_token_hash"`
	InstallExpiresAt *time.Time `db:"install_expires_at"`
	AgentTokenHash   *string    `db:"agent_token_hash"`
	Hostname         string     `db:"hostname"`
	OS               string     `db:"os"`
	Arch             string     `db:"arch"`
	AgentVersion     string     `db:"agent_version"`
	Address          string     `db:"address"`
	// Addresses are the public addresses the agent reported, space
	// separated: its IPv4 and IPv6 next to the one it connected from.
	Addresses  string     `db:"addresses"`
	LastSeenAt *time.Time `db:"last_seen_at"`
	// Tunnel carries app traffic through a Cloudflare Tunnel; TunnelToken
	// is sealed. SSHTailscale answers SSH only on the tailnet, Firewall
	// drops every other inbound connection. Security is the agent's last
	// report, as JSON.
	Tunnel       bool   `db:"tunnel"`
	TunnelID     string `db:"tunnel_id"`
	TunnelToken  string `db:"tunnel_token"`
	SSHTailscale bool   `db:"ssh_tailscale"`
	Firewall     bool   `db:"firewall"`
	Security     string `db:"security"`
	// Rules are the server's own rules for moin, as JSON; see
	// service.ServerRules.
	Rules string `db:"rules"`
	// Position is the place of the server in the workspace's order, from 0.
	Position  int       `db:"position"`
	CreatedAt time.Time `db:"created_at"`
	Online    bool      `db:"-"`
}

// Integrations are a user's keys for Cloudflare and Tailscale, sealed.
type Integrations struct {
	WorkspaceID       string    `db:"workspace_id"`
	CloudflareToken   string    `db:"cloudflare_token"`
	CloudflareAccount string    `db:"cloudflare_account"`
	TailscaleKey      string    `db:"tailscale_key"`
	UpdatedAt         time.Time `db:"updated_at"`
}
