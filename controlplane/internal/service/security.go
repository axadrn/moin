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

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/protocol"
)

var (
	ErrNoCloudflare    = errors.New("add a Cloudflare API token first")
	ErrNoTailscale     = errors.New("add a Tailscale auth key first")
	ErrCloudflareToken = errors.New("Cloudflare does not accept this token: it needs Account Cloudflare Tunnel Edit, Zone DNS Edit and Zone Settings Edit")
	ErrTailscaleKey    = errors.New("a Tailscale auth key starts with tskey-")
)

// SetCloudflare turns on tunnels through this Cloudflare API client.
func (s *AppService) SetCloudflare(cloudflare *Cloudflare) { s.cloudflare = cloudflare }

// Integrations says which keys a user stored, without the keys.
type Integrations struct {
	Cloudflare        bool
	CloudflareAccount string
	Tailscale         bool
}

// Integrations returns which keys the user stored.
func (s *AppService) Integrations(workspaceID string) (Integrations, error) {
	stored, err := s.servers.servers.Integrations(workspaceID)
	if err != nil {
		return Integrations{}, err
	}
	return Integrations{Cloudflare: stored.CloudflareToken != "", CloudflareAccount: stored.CloudflareAccount, Tailscale: stored.TailscaleKey != ""}, nil
}

// SetIntegrations stores the Cloudflare API token and the Tailscale auth
// key. Nil keeps a key, empty removes it. A Cloudflare token is checked
// against the API first.
func (s *AppService) SetIntegrations(ctx context.Context, actor Actor, cloudflareToken, tailscaleKey *string) error {
	stored, err := s.servers.servers.Integrations(actor.WorkspaceID)
	if err != nil {
		return err
	}
	if cloudflareToken != nil {
		token := strings.TrimSpace(*cloudflareToken)
		stored.CloudflareToken, stored.CloudflareAccount = "", ""
		if token != "" {
			account, err := s.cloudflare.Account(ctx, token)
			if err != nil {
				slog.Info("cloudflare token check failed", "workspace_id", actor.WorkspaceID, "error", err)
				return ErrCloudflareToken
			}
			stored.CloudflareToken, stored.CloudflareAccount = s.box.Seal(token, actor.WorkspaceID+"/cloudflare"), account
		}
	}
	if tailscaleKey != nil {
		key := strings.TrimSpace(*tailscaleKey)
		stored.TailscaleKey = ""
		if key != "" {
			if !strings.HasPrefix(key, "tskey-") {
				return ErrTailscaleKey
			}
			stored.TailscaleKey = s.box.Seal(key, actor.WorkspaceID+"/tailscale")
		}
	}
	stored.WorkspaceID, stored.UpdatedAt = actor.WorkspaceID, s.now().UTC()
	err = s.servers.servers.SetIntegrations(stored)
	if err != nil {
		return err
	}
	if cloudflareToken != nil {
		s.servers.record(actor, "integration.cloudflare", "integration", "cloudflare", map[string]string{"on": strconv.FormatBool(stored.CloudflareToken != "")})
	}
	if tailscaleKey != nil {
		s.servers.record(actor, "integration.tailscale", "integration", "tailscale", map[string]string{"on": strconv.FormatBool(stored.TailscaleKey != "")})
	}
	s.servers.notify(actor.WorkspaceID)
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

// SecurityInput is how a server should be reached and locked down.
type SecurityInput struct {
	Tunnel       bool
	SSHTailscale bool
	Firewall     bool
}

// SecurityReport is what the agent set up last.
func SecurityReport(server model.Server) *protocol.ServerConfigResult {
	if server.Security == "" {
		return nil
	}
	var report protocol.ServerConfigResult
	if json.Unmarshal([]byte(server.Security), &report) != nil {
		return nil
	}
	return &report
}

// SetServerSecurity creates or removes the server's tunnel, stores the
// settings and has the agent apply them. It returns what the agent set up;
// an offline server applies them when it connects.
func (s *AppService) SetServerSecurity(ctx context.Context, actor Actor, idOrName string, input SecurityInput) (*model.Server, error) {
	server, err := s.servers.servers.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	stored, err := s.servers.servers.Integrations(actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if input.SSHTailscale && stored.TailscaleKey == "" {
		return nil, ErrNoTailscale
	}
	if input.Tunnel && server.TunnelID == "" {
		token, account, err := s.cloudflareToken(actor.WorkspaceID)
		if err != nil {
			return nil, err
		}
		tunnelID, tunnelToken, err := s.cloudflare.CreateTunnel(ctx, token, account, "ops-"+server.Name+"-"+server.ID[:8])
		if err != nil {
			return nil, fmt.Errorf("create the tunnel: %w", err)
		}
		server.TunnelID, server.TunnelToken = tunnelID, s.box.Seal(tunnelToken, server.ID+"/tunnel")
	}
	if !input.Tunnel && server.TunnelID != "" {
		token, account, err := s.cloudflareToken(actor.WorkspaceID)
		if err == nil {
			err = s.cloudflare.DeleteTunnel(ctx, token, account, server.TunnelID)
		}
		if err != nil {
			slog.Warn("delete tunnel failed", "server_id", server.ID, "error", err)
		}
		server.TunnelID, server.TunnelToken = "", ""
	}
	server.Tunnel, server.SSHTailscale, server.Firewall = input.Tunnel, input.SSHTailscale, input.Firewall
	err = s.servers.servers.SetSecurity(*server)
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "server.security_set", "server", server.Name, map[string]string{"tunnel": strconv.FormatBool(server.Tunnel), "ssh_tailscale": strconv.FormatBool(server.SSHTailscale), "firewall": strconv.FormatBool(server.Firewall)})
	var dnsProblems []string
	if server.Tunnel {
		dnsProblems, err = s.syncTunnel(ctx, server)
		if err != nil {
			return nil, err
		}
	}
	if s.servers.Online(server.ID) {
		// A closed browser must not leave the server half configured.
		s.configureServer(context.WithoutCancel(ctx), server, dnsProblems)
	}
	s.servers.notify(actor.WorkspaceID)
	return s.servers.servers.ByIDOrNameForWorkspace(server.ID, actor.WorkspaceID)
}

// serverConfig is what the agent of a server gets.
func (s *AppService) serverConfig(server *model.Server) protocol.ServerConfig {
	config := protocol.ServerConfig{SSHOnTailscale: server.SSHTailscale, Firewall: server.Firewall}
	if server.Tunnel && server.TunnelToken != "" {
		token, err := s.box.Open(server.TunnelToken, server.ID+"/tunnel")
		if err == nil {
			config.TunnelToken = token
		}
	}
	stored, err := s.servers.servers.Integrations(server.WorkspaceID)
	if err == nil && stored.TailscaleKey != "" && server.SSHTailscale {
		key, err := s.box.Open(stored.TailscaleKey, server.WorkspaceID+"/tailscale")
		if err == nil {
			config.TailscaleKey = key
		}
	}
	return config
}

// configureServer sends the server config and stores what the agent set
// up.
func (s *AppService) configureServer(ctx context.Context, server *model.Server, problems []string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var report protocol.ServerConfigResult
	err := s.servers.Request(ctx, server.ID, protocol.TypeServerConfig, s.serverConfig(server), &report)
	if err != nil {
		report = protocol.ServerConfigResult{Problems: []string{"the agent could not apply it: " + err.Error()}}
	}
	report.Problems = append(report.Problems, problems...)
	body, _ := json.Marshal(report)
	err = s.servers.servers.SetSecurityReport(server.ID, string(body))
	if err != nil {
		slog.Error("store security report failed", "server_id", server.ID, "error", err)
	}
}

// configureServerOnConnect sends the config to an agent that just
// connected, when the server has one.
func (s *AppService) configureServerOnConnect(serverID, workspaceID string) {
	server, err := s.servers.servers.ByIDOrNameForWorkspace(serverID, workspaceID)
	if err != nil || (!server.Tunnel && !server.Firewall && !server.SSHTailscale) {
		return
	}
	s.configureServer(context.Background(), server, nil)
	s.servers.notify(workspaceID)
}

// tunnelHosts are the domains of every app on a server: the ones set
// through ops and the ones x-ops set in the live release.
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
// and points their DNS at it. A domain whose DNS ops cannot set, such as
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
	for _, host := range hosts {
		err = s.cloudflare.PointAtTunnel(ctx, token, host, server.TunnelID)
		if err != nil {
			problems = append(problems, fmt.Sprintf("DNS of %s: %s; point it at %s.cfargotunnel.com yourself", host, err, server.TunnelID))
		}
	}
	return problems, nil
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
