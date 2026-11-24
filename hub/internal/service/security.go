package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

var (
	ErrNoCloudflare    = errors.New("connect Cloudflare in Settings, Integrations first")
	ErrNoTailscale     = errors.New("this server has no Tailscale key: enter one for it in Settings, Integrations, or join the server to your tailnet by hand")
	ErrSSH             = errors.New("SSH is tailscale, everyone or off")
	ErrCloudflareToken = errors.New("Cloudflare does not accept this token: it needs Account Cloudflare Tunnel Edit and Zone DNS Edit")
	ErrTailscaleKey    = errors.New("a Tailscale auth key starts with tskey-")
	// ErrTunnelsOn keeps the Cloudflare API token while a server has a
	// tunnel: without it moin cannot delete the tunnel in Cloudflare.
	ErrTunnelsOn = errors.New("turn the tunnels off first: without the token moin cannot delete them in Cloudflare")
)

// SetCloudflare turns on tunnels through this Cloudflare API client.
func (s *AppService) SetCloudflare(cloudflare *Cloudflare) { s.cloudflare = cloudflare }

// Integrations says which keys a workspace has, without the keys: the
// Cloudflare API token, which the hub keeps and uses itself, and the
// servers that keep a Tailscale key.
type Integrations struct {
	Cloudflare        bool
	CloudflareAccount string
	Tailscale         SecretServers
}

// Integrations returns which keys the workspace has.
func (s *AppService) Integrations(workspaceID string) (Integrations, error) {
	stored, err := s.servers.servers.Integrations(workspaceID)
	if err != nil {
		return Integrations{}, err
	}
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return Integrations{}, err
	}
	return Integrations{Cloudflare: stored.CloudflareToken != "", CloudflareAccount: stored.CloudflareAccount, Tailscale: index.of(protocol.SecretTailscale)}, nil
}

// IntegrationsInput changes the keys of a workspace. Nil keeps a key,
// empty removes it. The Tailscale key goes to TailscaleServer, empty for
// the workspace's only server, which keeps it; the hub never does.
type IntegrationsInput struct {
	CloudflareToken *string
	TailscaleKey    *string
	TailscaleServer string
}

// SetIntegrations stores the Cloudflare API token, checked against the API
// first, and hands a server the Tailscale auth key.
func (s *AppService) SetIntegrations(ctx context.Context, actor Actor, input IntegrationsInput) error {
	if input.TailscaleKey != nil {
		key := strings.TrimSpace(*input.TailscaleKey)
		if key != "" && !strings.HasPrefix(key, "tskey-") {
			return ErrTailscaleKey
		}
		server, err := s.servers.secretServer(actor.WorkspaceID, input.TailscaleServer)
		if err != nil {
			return err
		}
		if err := s.servers.setSecret(ctx, server, protocol.SecretTailscale, key); err != nil {
			return err
		}
		s.servers.record(actor, "integration.tailscale", "integration", "tailscale", map[string]string{"on": strconv.FormatBool(key != ""), "server": server.Name})
	}
	if input.CloudflareToken != nil {
		stored, err := s.servers.servers.Integrations(actor.WorkspaceID)
		if err != nil {
			return err
		}
		token := strings.TrimSpace(*input.CloudflareToken)
		if token == "" && stored.CloudflareToken != "" {
			if err := s.noTunnels(actor.WorkspaceID); err != nil {
				return err
			}
		}
		stored.CloudflareToken, stored.CloudflareAccount = "", ""
		if token != "" {
			account, err := s.cloudflare.Account(ctx, token)
			if err != nil {
				slog.Info("cloudflare token check failed", "workspace_id", actor.WorkspaceID, "error", err)
				return ErrCloudflareToken
			}
			stored.CloudflareToken, stored.CloudflareAccount = s.box.Seal(token, actor.WorkspaceID+"/cloudflare"), account
		}
		stored.WorkspaceID, stored.UpdatedAt = actor.WorkspaceID, s.now().UTC()
		if err := s.servers.servers.SetIntegrations(stored); err != nil {
			return err
		}
		s.servers.record(actor, "integration.cloudflare", "integration", "cloudflare", map[string]string{"on": strconv.FormatBool(stored.CloudflareToken != "")})
	}
	s.servers.notify(actor.WorkspaceID)
	return nil
}

// noTunnels refuses with ErrTunnelsOn while a server of the workspace has
// a tunnel in Cloudflare, or should get one.
func (s *AppService) noTunnels(workspaceID string) error {
	servers, err := s.servers.servers.ListByWorkspaceID(workspaceID)
	if err != nil {
		return err
	}
	for _, server := range servers {
		if server.TunnelID != "" || server.Tunnel {
			return ErrTunnelsOn
		}
	}
	return nil
}

func (s *AppService) cloudflareToken(workspaceID string) (token, account string, err error) {
	stored, err := s.servers.servers.Integrations(workspaceID)
	if err != nil {
		return "", "", err
	}
	if stored.CloudflareToken == "" {
		return "", "", ErrNoCloudflare
	}
	token, err = s.box.Open(stored.CloudflareToken, workspaceID+"/cloudflare")
	return token, stored.CloudflareAccount, err
}

// SecurityInput is how a server should be reached and locked down. SSH is
// protocol.SSHTailscale, SSHEveryone or SSHOff.
type SecurityInput struct {
	Tunnel   bool
	SSH      string
	Firewall bool
}

// SecurityReport is what the daemon last saw on the server. A daemon from
// before it reported SSH told it only through its tailnet IP: SSH is filled
// in from that, so every surface reads one word.
func SecurityReport(server model.Server) *protocol.ServerConfigResult {
	if server.Security == "" {
		return nil
	}
	var report protocol.ServerConfigResult
	if json.Unmarshal([]byte(server.Security), &report) != nil {
		return nil
	}
	if report.SSH == "" {
		report.SSH = protocol.SSHEveryone
		if server.SSH == protocol.SSHTailscale && report.TailscaleIP != "" && !slices.ContainsFunc(report.Problems, func(problem string) bool { return strings.HasPrefix(problem, "SSH stays open") }) {
			report.SSH = protocol.SSHTailscale
		}
	}
	return &report
}

// SetServerSecurity creates or removes the server's tunnel, stores the
// settings and has the daemon apply them. It returns what the daemon set up;
// an offline server applies them when it connects. SSH only through
// Tailscale needs the Tailscale key, or a tailnet the server joined by hand.
func (s *AppService) SetServerSecurity(ctx context.Context, actor Actor, idOrName string, input SecurityInput) (*model.Server, error) {
	if !protocol.ValidSSH(input.SSH) {
		return nil, ErrSSH
	}
	server, err := s.servers.servers.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	index, err := s.servers.secretsIndex(actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	report := SecurityReport(*server)
	if input.SSH == protocol.SSHTailscale && server.SSH != protocol.SSHTailscale && !index.has(server.ID, protocol.SecretTailscale) && (report == nil || report.TailscaleIP == "") {
		return nil, ErrNoTailscale
	}
	before := securityDetails(server)
	// Cloudflare work runs to its end, also when the browser gives up:
	// a tunnel half made or half deleted helps nobody.
	cloudflare, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	// Only turning the tunnel on points the domains at it. SSH and the
	// firewall have nothing to do with DNS, and a domain added later is
	// pointed when it is added. Its token goes to the server at once, so
	// an offline server gets its tunnel when it connects.
	turnedOn := input.Tunnel && server.TunnelID == "" && s.servers.Online(server.ID)
	if turnedOn {
		if err := s.openTunnel(cloudflare, server); err != nil {
			return nil, err
		}
	}
	if !input.Tunnel && server.TunnelID != "" {
		token, account, err := s.cloudflareToken(actor.WorkspaceID)
		if err == nil {
			s.unpointTunnel(cloudflare, server, token)
			err = s.cloudflare.DeleteTunnel(cloudflare, token, account, server.TunnelID)
		}
		if err != nil {
			slog.Warn("delete tunnel failed", "server_id", server.ID, "error", err)
		}
		server.TunnelID = ""
	}
	server.Tunnel, server.SSH, server.Firewall = input.Tunnel, input.SSH, input.Firewall
	err = s.servers.servers.SetSecurity(*server)
	if err != nil {
		return nil, err
	}
	details := securityDetails(server)
	for key, value := range before {
		details[key+"_before"] = value
	}
	s.servers.record(actor, "server.security_set", "server", server.Name, details)
	var dnsProblems []string
	if turnedOn {
		dnsProblems, err = s.syncTunnel(cloudflare, server)
		if err != nil {
			return nil, err
		}
	}
	if s.servers.Online(server.ID) {
		// A closed browser must not leave the server half configured.
		s.configureServer(context.WithoutCancel(ctx), server, dnsProblems)
	}
	s.servers.notify(actor.WorkspaceID)
	return s.servers.Get(actor.WorkspaceID, server.ID)
}

// securityDetails are a server's settings for the activity.
func securityDetails(server *model.Server) map[string]string {
	return map[string]string{"tunnel": onOff(server.Tunnel), "ssh": server.SSH, "firewall": onOff(server.Firewall)}
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// openTunnel creates the server's tunnel in Cloudflare with the
// workspace's token and hands its token to the server, which keeps it:
// the hub keeps only the tunnel's ID. A server that does not take the
// token gets no tunnel.
func (s *AppService) openTunnel(ctx context.Context, server *model.Server) error {
	token, account, err := s.cloudflareToken(server.WorkspaceID)
	if err != nil {
		return err
	}
	tunnelID, tunnelToken, err := s.cloudflare.CreateTunnel(ctx, token, account, build.Name+"-"+server.Name+"-"+server.ID[:8])
	if err != nil {
		return fmt.Errorf("create the tunnel: %w", err)
	}
	err = s.servers.setSecret(ctx, server, protocol.SecretTunnel, tunnelToken)
	if err != nil {
		if deleteErr := s.cloudflare.DeleteTunnel(ctx, token, account, tunnelID); deleteErr != nil {
			slog.Warn("delete tunnel failed", "server_id", server.ID, "error", deleteErr)
		}
		return fmt.Errorf("hand the tunnel token to the server: %w", err)
	}
	server.TunnelID = tunnelID
	return nil
}

// receiveSecurity keeps what a daemon reports by itself, after a start and
// on its schedule: what really is on the server, and what it put back,
// which goes into the activity as moin.
func (s *AppService) receiveSecurity(serverID string, report protocol.ServerConfigResult) {
	workspaceID, ok := s.servers.UserOf(serverID)
	if !ok {
		return
	}
	server, err := s.servers.servers.ByIDOrNameForWorkspace(serverID, workspaceID)
	if err != nil {
		return
	}
	for _, fixed := range report.Fixed {
		s.servers.record(System(server.WorkspaceID), "server.security_fixed", "server", server.Name, map[string]string{"fixed": fixed})
	}
	report.Fixed = nil
	body, _ := json.Marshal(report)
	if string(body) == server.Security {
		return
	}
	err = s.servers.servers.SetSecurityReport(server.ID, string(body))
	if err != nil {
		slog.Error("store security report failed", "server_id", server.ID, "error", err)
		return
	}
	s.servers.notify(server.WorkspaceID)
}

// serverConfig is what the daemon of a server gets: no secret, the
// server keeps those.
func serverConfig(server *model.Server) protocol.ServerConfig {
	return protocol.ServerConfig{SSH: server.SSH, SSHOnTailscale: server.SSH == protocol.SSHTailscale, Firewall: server.Firewall}
}

// configureServer sends the server config and stores what the daemon set
// up. A server without a tunnel forgets the token of the one it had, such
// as one turned off while it was offline.
func (s *AppService) configureServer(ctx context.Context, server *model.Server, problems []string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if index, err := s.servers.secretsIndex(server.WorkspaceID); err == nil && server.TunnelID == "" && index.has(server.ID, protocol.SecretTunnel) {
		if err := s.servers.setSecret(ctx, server, protocol.SecretTunnel, ""); err != nil {
			problems = append(problems, "Cloudflare Tunnel: the server keeps the old token: "+err.Error())
		}
	}
	var report protocol.ServerConfigResult
	err := s.servers.Request(ctx, server.ID, protocol.TypeServerConfig, serverConfig(server), &report)
	if err != nil {
		report = protocol.ServerConfigResult{Problems: []string{"the daemon could not apply it: " + err.Error()}}
	}
	report.Problems = append(report.Problems, problems...)
	body, _ := json.Marshal(report)
	err = s.servers.servers.SetSecurityReport(server.ID, string(body))
	if err != nil {
		slog.Error("store security report failed", "server_id", server.ID, "error", err)
	}
}

// configureServerOnConnect sends the config to a daemon that just
// connected, when the server has one, or when the daemon last reported a
// firewall, tunnel or SSH only through Tailscale the server no longer has,
// such as one turned off while the daemon was offline. A server that
// should have a tunnel and has none yet, like a new one added while
// Cloudflare is connected, gets it first.
func (s *AppService) configureServerOnConnect(serverID, workspaceID string) {
	server, err := s.servers.servers.ByIDOrNameForWorkspace(serverID, workspaceID)
	if err != nil {
		return
	}
	report := SecurityReport(*server)
	first := s.proxyOnFirstConnect && report == nil
	if !first && !server.Tunnel && !server.Firewall && server.SSH == protocol.SSHEveryone && !leftOver(report) {
		return
	}
	ctx := context.Background()
	var problems []string
	if server.Tunnel && server.TunnelID == "" {
		err = s.openTunnel(ctx, server)
		if err == nil {
			err = s.servers.servers.SetSecurity(*server)
		}
		if err == nil {
			problems, err = s.syncTunnel(ctx, server)
		}
		if err != nil {
			problems = append(problems, "Cloudflare Tunnel: "+err.Error())
		}
	}
	s.configureServer(ctx, server, problems)
	s.servers.notify(workspaceID)
}

// ProxyOnFirstConnect has a server start its proxy as soon as its daemon
// first connects, not at its first app with a domain: for a hub
// on the same server as its apps, which the proxy serves too.
func (s *AppService) ProxyOnFirstConnect() {
	s.proxyOnFirstConnect = true
}

// leftOver says whether the daemon last saw a firewall, a tunnel or SSH
// only through Tailscale or off, which a server set to none of them must lose.
func leftOver(report *protocol.ServerConfigResult) bool {
	return report != nil && (report.Firewall || report.Proxy == "tunnel" || report.SSH == protocol.SSHTailscale || report.SSH == protocol.SSHOff)
}

// tunnelHosts are the domains of every app on a server: the ones set
// through moin and the ones x-moin set in the live deploy.
func (s *AppService) tunnelHosts(server *model.Server) ([]string, error) {
	apps, err := s.apps.ListByWorkspaceID(server.WorkspaceID)
	if err != nil {
		return nil, err
	}
	hosts := []string{}
	for _, app := range apps {
		if app.ServerID != server.ID {
			continue
		}
		domains := app.DomainList()
		if app.LiveNumber > 0 {
			deploy, err := s.apps.Deploy(app.ID, app.LiveNumber)
			if err == nil {
				detected := Detected(*deploy)
				if detected != nil && len(detected.Domains) > 0 {
					domains = detected.Domains
				}
			}
		}
		for _, domain := range domains {
			if !slices.Contains(hosts, domain) {
				hosts = append(hosts, domain)
			}
		}
	}
	slices.Sort(hosts)
	return hosts, nil
}

// syncTunnel routes every domain of the server's apps through its tunnel
// and points their DNS at it. A domain whose DNS moin cannot set, such as
// one in another Cloudflare account, is a problem to report, not a
// failure: the user can point it at the tunnel by hand.
func (s *AppService) syncTunnel(ctx context.Context, server *model.Server) ([]string, error) {
	if !server.Tunnel || server.TunnelID == "" {
		return nil, nil
	}
	token, account, err := s.cloudflareToken(server.WorkspaceID)
	if err != nil {
		return nil, err
	}
	hosts, err := s.tunnelHosts(server)
	if err != nil {
		return nil, err
	}
	err = s.cloudflare.RouteTunnel(ctx, token, account, server.TunnelID, hosts)
	if err != nil {
		return nil, fmt.Errorf("route the tunnel: %w", err)
	}
	problems := []string{}
	zones := []string{}
	for _, host := range hosts {
		zone, err := s.cloudflare.PointAtTunnel(ctx, token, host, server.TunnelID)
		if err != nil {
			problems = append(problems, fmt.Sprintf("DNS of %s: %s; point it at %s.cfargotunnel.com yourself", host, err, server.TunnelID))
			continue
		}
		if slices.Contains(zones, zone) {
			continue
		}
		zones = append(zones, zone)
		err = s.cloudflare.AlwaysUseHTTPS(ctx, token, zone)
		if err != nil {
			problems = append(problems, fmt.Sprintf("Always Use HTTPS for %s: %s; give the token Zone Settings Edit or turn it on in Cloudflare yourself", host, err))
		}
	}
	return problems, nil
}

// unpointTunnel deletes the DNS records that point the domains of the
// server's apps at its tunnel, before the tunnel goes.
func (s *AppService) unpointTunnel(ctx context.Context, server *model.Server, token string) {
	hosts, err := s.tunnelHosts(server)
	if err != nil {
		slog.Warn("list tunnel domains failed", "server_id", server.ID, "error", err)
		return
	}
	for _, host := range hosts {
		err = s.cloudflare.UnpointFromTunnel(ctx, token, host, server.TunnelID)
		if err != nil {
			slog.Warn("delete tunnel DNS record failed", "server_id", server.ID, "host", host, "error", err)
		}
	}
}

// syncTunnelOf syncs the tunnel of an app's server in the background,
// after its domains changed.
func (s *AppService) syncTunnelOf(app *model.App) {
	server, err := s.servers.servers.ByIDOrNameForWorkspace(app.ServerID, app.WorkspaceID)
	if err != nil || !server.Tunnel {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		problems, err := s.syncTunnel(ctx, server)
		if err != nil || len(problems) > 0 {
			slog.Warn("sync tunnel failed", "server_id", server.ID, "error", err, "problems", problems)
		}
	}()
}
