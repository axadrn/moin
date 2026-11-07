package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerUptimeTools registers the tools of uptime.
func registerUptimeTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "get_uptime", Title: "Get uptime", Annotations: readOnly("Get uptime"),
		Description: "Returns the uptime of an app: one state for all its domains (up, down, pending, paused, stopped or no_domains), whether its server is online, and per domain where the visit goes, its state, the last visit with HTTP status, response time and certificate expiry, uptime in percent over 24 hours, 7 and 30 days, and the last outages. moin visits every domain once a minute like a visitor; an answer below 500 is up. Needs the read scope.",
	}, Read(member(services, tools.getUptime)))
	writeTool := change(false, true)
	writeTool.Title = "Set uptime"
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "set_uptime", Title: "Set uptime", Annotations: writeTool,
		Description: "Changes the path uptime visits on one domain of an app (empty goes back to the health path from x-moin.health, or /), or pauses and resumes it. A missing field keeps its setting. Returns the domain. Needs a token with read and write access.",
	}, Write(changes(services, tools.setUptime)))
}

// Uptime is the output of get_uptime.
type Uptime struct {
	api.Uptime
}

func (u Uptime) Summary() string {
	if len(u.Domains) == 0 {
		return u.App + " has no domains to visit."
	}
	parts := []string{}
	for _, domain := range u.Domains {
		parts = append(parts, domainSummary(domain))
	}
	summary := u.App + " is " + u.State + ": " + strings.Join(parts, "; ") + "."
	if !u.ServerOnline {
		summary += " Its server " + u.Server + " is offline."
	}
	return summary
}

// domainSummary is one domain in a few words: "my-app.example.com up, 99.9% in 24h".
func domainSummary(domain api.UptimeDomain) string {
	text := domain.Domain + " " + domain.State
	if domain.State == "down" && domain.Error != "" {
		text += " (" + domain.Error + ")"
	}
	if domain.Uptime24h != nil {
		text += fmt.Sprintf(", %.2f%% in 24h", *domain.Uptime24h)
	}
	return text
}

// UptimeDomain is the output of set_uptime.
type UptimeDomain struct {
	api.UptimeDomain
}

func (d UptimeDomain) Summary() string {
	if d.Paused {
		return "Uptime of " + d.Domain + " is paused."
	}
	return "Uptime of " + d.Domain + " goes to " + d.Path + "."
}

// UptimeInput is the input of set_uptime.
type UptimeInput struct {
	App    string  `json:"app" jsonschema:"the app name or ID"`
	Domain string  `json:"domain" jsonschema:"one of the app's domains"`
	Path   *string `json:"path,omitempty" jsonschema:"the path to visit, like /health; empty for the health path or /"`
	Paused *bool   `json:"paused,omitempty" jsonschema:"true pauses uptime of the domain, false resumes it"`
}

func (t moinTools) getUptime(_ context.Context, caller Caller, input AppInput) (Uptime, error) {
	app, uptime, err := t.services.Apps.Uptime(caller.Token.WorkspaceID, input.app())
	if err != nil {
		return Uptime{}, err
	}
	return Uptime{api.NewUptime(app.Name, uptime)}, nil
}

func (t moinTools) setUptime(_ context.Context, caller Caller, input UptimeInput) (UptimeDomain, error) {
	_, domain, err := t.services.Apps.SetUptime(caller.actor(), input.App, input.Domain, input.Path, input.Paused)
	if err != nil {
		return UptimeDomain{}, err
	}
	return UptimeDomain{api.NewUptimeDomain(domain)}, nil
}
