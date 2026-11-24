package protocol

import (
	"strings"
	"time"
)

// Hello describes the daemon and its server.
type Hello struct {
	ProtocolVersion int    `json:"protocol_version"`
	DaemonVersion   string `json:"daemon_version"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	// Addresses are the server's own public IPv4 and IPv6 addresses, as far
	// as it knows them. Older daemons send none; the hub then knows
	// only the address the daemon connected from.
	Addresses []string `json:"addresses,omitempty"`
	// Rules are the server's own rules for moin, from /etc/moin/rules. Nil
	// means the server has none and moin may do everything.
	Rules *Rules `json:"rules,omitempty"`
	// Secrets are the names of the secrets the daemon keeps in its
	// server.json (SecretSet), never their values.
	Secrets []string `json:"secrets,omitempty"`
}

// Rules are what the owner of a server allows moin to do there, written in
// /etc/moin/rules on the server. The hub cannot change them; the
// daemon refuses every request they do not allow.
type Rules struct {
	// DataOff refuses tables, SQL and row edits.
	DataOff bool `json:"data_off,omitempty"`
	// LogsOff refuses container logs.
	LogsOff bool `json:"logs_off,omitempty"`
	// DeployFrom lists where deploys may come from, such as
	// "github.com/acme/*", "ghcr.io/acme/*" or "catalog". Empty allows all.
	DeployFrom []string `json:"deploy_from,omitempty"`
	// BackupsTo is the only bucket backups may go to and come from, as
	// "<endpoint host>/<bucket>". Empty allows any.
	BackupsTo string `json:"backups_to,omitempty"`
	// NetworkLocked refuses changes to the tunnel, Tailscale and firewall.
	NetworkLocked bool `json:"network_locked,omitempty"`
	// ExecOff refuses running commands in containers.
	ExecOff bool `json:"exec_off,omitempty"`
	// Mounts are folders of the server an app may mount, such as
	// "/mnt/media". Without them an app mounts only its own folders.
	Mounts []string `json:"mounts,omitempty"`
	// ServerAccess allows compose settings that reach beyond the app, such
	// as privileged containers or a mount of the Docker socket. Without it
	// the daemon refuses them, with or without a rules file.
	ServerAccess bool `json:"server_access,omitempty"`
}

// Lines are the rules as written in the rules file, for every surface to
// show them the same way.
func (r *Rules) Lines() []string {
	if r == nil {
		return nil
	}
	var lines []string
	if r.DataOff {
		lines = append(lines, "data: off")
	}
	if r.LogsOff {
		lines = append(lines, "logs: off")
	}
	if len(r.DeployFrom) > 0 {
		lines = append(lines, "deploy-from: "+strings.Join(r.DeployFrom, ", "))
	}
	if r.BackupsTo != "" {
		lines = append(lines, "backups-to: "+r.BackupsTo)
	}
	if r.NetworkLocked {
		lines = append(lines, "network: locked")
	}
	if r.ExecOff {
		lines = append(lines, "exec: off")
	}
	if len(r.Mounts) > 0 {
		lines = append(lines, "mounts: "+strings.Join(r.Mounts, ", "))
	}
	if r.ServerAccess {
		lines = append(lines, "server-access: on")
	}
	return lines
}

// RegisterRequest is the body of a register call.
type RegisterRequest struct {
	Hello
}

// RegisterResponse answers a register call. The daemon stores the token and
// uses it for every session.
type RegisterResponse struct {
	ServerID    string `json:"server_id"`
	DaemonToken string `json:"daemon_token"`
}

// Who SSH answers on a server: only through Tailscale, everyone, or
// nobody, with sshd stopped.
const (
	SSHTailscale = "tailscale"
	SSHEveryone  = "everyone"
	SSHOff       = "off"
)

// ValidSSH says whether ssh is one of SSHTailscale, SSHEveryone and SSHOff.
func ValidSSH(ssh string) bool {
	return ssh == SSHTailscale || ssh == SSHEveryone || ssh == SSHOff
}

// SecurityEvery is how often the daemon checks that its server still has
// the security of its ServerConfig, puts back what drifted and sends
// TypeServerSecurity. It checks after each start too.
const SecurityEvery = 5 * time.Minute

// ServerConfig is how a server is reached and locked down. It carries no
// secret: the tunnel token and the Tailscale key are secrets the daemon
// keeps (SecretTunnel, SecretTailscale).
//
// With a tunnel token the daemon runs cloudflared, which carries all app
// traffic from Cloudflare to Traefik; the proxy then opens no port. Without
// it Traefik takes 80 and 443. The Tailscale key joins the server to the
// user's tailnet when SSH is SSHTailscale. SSH says who SSH answers: SSHTailscale only through Tailscale, SSHEveryone, or SSHOff
// with sshd stopped. Firewall drops every other inbound connection.
//
// SSHOnTailscale is SSH == SSHTailscale for daemons before SSH: the hub sends
// both, and a daemon without SSH reads SSHOnTailscale.
type ServerConfig struct {
	SSH            string `json:"ssh,omitempty"`
	SSHOnTailscale bool   `json:"ssh_on_tailscale,omitempty"`
	Firewall       bool   `json:"firewall,omitempty"`
}

// WantSSH is who SSH should answer, also for a config from before SSH.
func (c ServerConfig) WantSSH() string {
	switch {
	case ValidSSH(c.SSH):
		return c.SSH
	case c.SSHOnTailscale:
		return SSHTailscale
	}
	return SSHEveryone
}

// ServerConfigResult is what the daemon set up and what really is on the
// server. Problems are the parts it could not, in words; the rest stands.
// TailscaleIP is the server's address in a tailnet, also one joined by
// hand. SSH is who SSH answers now; daemons before it leave it empty.
// Fixed is what the daemon put back since its last report, in words, for
// the activity.
type ServerConfigResult struct {
	Proxy       string   `json:"proxy"`
	TailscaleIP string   `json:"tailscale_ip,omitempty"`
	SSH         string   `json:"ssh,omitempty"`
	Firewall    bool     `json:"firewall"`
	OpenPorts   []string `json:"open_ports"`
	Problems    []string `json:"problems,omitempty"`
	Fixed       []string `json:"fixed,omitempty"`
}

// OpenText says what the firewall leaves open to everyone, the same on
// every surface: "80, 443, 41641/udp", or "nothing". A TCP port is only
// its number.
func (r ServerConfigResult) OpenText() string {
	if len(r.OpenPorts) == 0 {
		return "nothing"
	}
	ports := make([]string, len(r.OpenPorts))
	for i, port := range r.OpenPorts {
		ports[i] = strings.TrimSuffix(port, "/tcp")
	}
	return strings.Join(ports, ", ")
}
