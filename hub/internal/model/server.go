package model

import "time"

// Server is a machine with a moin daemon. Online is not stored: the daemon hub
// knows which servers hold a session right now.
type Server struct {
	ID               string     `db:"id"`
	WorkspaceID      string     `db:"workspace_id"`
	Name             string     `db:"name"`
	InstallTokenHash *string    `db:"install_token_hash"`
	InstallExpiresAt *time.Time `db:"install_expires_at"`
	DaemonTokenHash  *string    `db:"daemon_token_hash"`
	Hostname         string     `db:"hostname"`
	OS               string     `db:"os"`
	Arch             string     `db:"arch"`
	DaemonVersion    string     `db:"daemon_version"`
	Address          string     `db:"address"`
	// Addresses are the public addresses the daemon reported, space
	// separated: its IPv4 and IPv6 next to the one it connected from.
	Addresses  string     `db:"addresses"`
	LastSeenAt *time.Time `db:"last_seen_at"`
	// Tunnel carries app traffic through a Cloudflare Tunnel; TunnelToken
	// is sealed. SSHTailscale answers SSH only on the tailnet, Firewall
	// drops every other inbound connection. Security is the daemon's last
	// report, as JSON.
	Tunnel       bool   `db:"tunnel"`
	TunnelID     string `db:"tunnel_id"`
	TunnelToken  string `db:"tunnel_token"`
	SSHTailscale bool   `db:"ssh_tailscale"`
	Firewall     bool   `db:"firewall"`
	Security     string `db:"security"`
	// Rules are the server's own rules for moin, as JSON; see
	// service.ServerRules.
	Rules     string    `db:"rules"`
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

// DaemonUpdate is a daemon update that is not done yet: the note the hub
// keeps while it runs the steps of an update (service/daemon_steps.go).
// StoppedApps are the IDs of the apps it stopped, as a JSON array.
type DaemonUpdate struct {
	ServerID    string    `db:"server_id"`
	FromVersion string    `db:"from_version"`
	StoppedApps string    `db:"stopped_apps"`
	StartedAt   time.Time `db:"started_at"`
}
