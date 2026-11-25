// Package mcp serves the MCP server at /mcp. It is a third transport next to
// the HTML handlers and the JSON API: tools call the same services and report
// errors with the codes of the JSON API envelope. AI agents authenticate as a
// connection or with a CI token, and its rights decide which tools work.
package mcp

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/middleware"
	"github.com/axadrn/moin/hub/internal/service"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is the version the server reports to MCP clients. Raise it when the
// tools change.
const Version = "1.33.0"

// instructions tell the AI agent how the tools behave as a whole. Clients pass
// them to the model when they connect.
const instructions = "Every tool acts for the person who let this connection in, or who made the CI token in the Authorization header. " +
	"Each tool needs one right, named at the end of its description: read, deploy, env, delete or servers. " +
	"Without it a tool fails with the code missing_right; only the person changes the rights, in Settings, Connections. " +
	"The connection is bound to one workspace. Workspace tools take an optional workspace slug and default to that workspace; other workspaces are not found. " +
	"Failed calls return an error result whose text is {\"error\":{\"code\":...,\"message\":...,\"fields\":{...}}}, with the same codes as the JSON API."

// Services are the services the tools call. Add your own services here and
// pass them in internal/routes.
type Services struct {
	Users         *service.UserService
	Workspaces    *service.WorkspaceService
	Subscriptions *service.SubscriptionService
	// Invitations is the invitation limiter of the web routes and the JSON
	// API, so invite_member draws from the same budget.
	Invitations *middleware.RateLimiter
	Servers     *service.ServerService
	Apps        *service.AppService
	// Updates tells of new releases and updates a self-hosted hub.
	Updates *service.UpdateService
	// Alerts manages where alerts go besides email.
	Alerts *service.AlertService
	// Catalog lists the catalog, installs its apps and updates them.
	Catalog *service.CatalogService
	// Status answers whether everything is OK in a workspace.
	Status *service.StatusService
	// BillingOff is set on a self-hosted hub, BILLING=false:
	// get_billing says so, like the JSON API.
	BillingOff bool
}

// NewServer returns the MCP server with every built-in tool registered.
func NewServer(services Services) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: brand.Name, Version: Version}, &mcpsdk.ServerOptions{
		Instructions: instructions,
	})
	registerTools(server, services)
	return server
}

// Handler serves server over Streamable HTTP. It is stateless: every POST is
// one complete exchange, so the server keeps no sessions and scales like the
// JSON API. Requests without a connection or CI token get 401
// unauthenticated; a CI token for one app gets 403 app_only.
func Handler(server *mcpsdk.Server) http.Handler {
	streamable := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return server
	}, &mcpsdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Every request carries a bearer token and never a cookie, so DNS
		// rebinding gains nothing. The protection would also reject a reverse
		// proxy on the same host that forwards the public Host header.
		DisableLocalhostProtection: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := ctxkeys.Bearer(r.Context())
		if bearer == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			api.Error(w, http.StatusUnauthorized, "unauthenticated", unauthenticated)
			return
		}
		if bearer.AppID != "" {
			api.Error(w, http.StatusForbidden, "app_only", appOnly)
			return
		}
		streamable.ServeHTTP(w, r)
	})
}
