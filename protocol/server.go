package protocol

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
// SSHOnTailscale is SSH == SSHTailscale, from before SSH.
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
