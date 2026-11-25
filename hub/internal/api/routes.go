package api

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/model"
)

// Route is one endpoint of the JSON API: a ServeMux pattern, its handler with
// every middleware applied, and the right a connection or CI token needs
// there. This table is the one place that says it: the gate checks it, and
// a test checks the docs' table of rights against it.
type Route struct {
	Pattern string
	Handler http.HandlerFunc
	// Right is what a connection or CI token needs, one right or two that
	// must both be on. NoToken, the zero value, keeps them out.
	Right model.Rights
}

// Wrap adds one middleware to a route handler.
type Wrap func(http.HandlerFunc) http.HandlerFunc

// Handlers are the API handlers the routes call.
type Handlers struct {
	Meta        *MetaHandler
	Auth        *AuthHandler
	Account     *AccountHandler
	Sessions    *SessionsHandler
	Passkeys    *PasskeysHandler
	Connections *ConnectionsHandler
	CITokens    *CITokensHandler
	Token       *TokenHandler
	Billing     *BillingHandler
	Admin       *AdminHandler
	Workspaces  *WorkspaceHandler
	Invitations *WorkspaceInvitationHandler
	Servers     *ServersHandler
	Apps        *AppsHandler
	Version     *VersionHandler
	Alerts      *AlertsHandler
	Catalog     *CatalogHandler
	Status      *StatusHandler
	Device      *DeviceHandler
}

// Middleware are the route middleware from internal/middleware. They are
// passed in because internal/middleware imports this package.
type Middleware struct {
	// RequireAuth answers 401 without a session.
	RequireAuth Wrap
	// RequireOnboarded also answers 403 onboarding_required without a name.
	RequireOnboarded Wrap
	// RateLimitEmailChanges limits email change requests per user and
	// recipient.
	RateLimitEmailChanges Wrap
	// RequireAdmin answers 403 forbidden for users who are not admins.
	RequireAdmin Wrap
	// Workspace requires onboarding and scopes the route to the workspace
	// in its path.
	Workspace Wrap
	// RateLimitInvitations limits workspace invitation emails.
	RateLimitInvitations Wrap
	// InWorkspace requires onboarding and scopes a moin route to the
	// workspace of the API token, or for a session to the workspace its
	// Moin-Workspace header names.
	InWorkspace Wrap
	// RateLimitDevice limits the logins `moin login` starts per client IP.
	RateLimitDevice Wrap
	// RateLimitDevicePoll limits the polls of `moin login` and the
	// refreshes of the token endpoint per client IP.
	RateLimitDevicePoll Wrap
	// AppID finds the ID of an app in a workspace by its name or ID, for
	// CI tokens that may reach one app only.
	AppID func(workspaceID, app string) (string, error)
}

// Routes lists every endpoint under /api/v1. The router registers exactly
// these routes, and openapi_test.go checks them against openapi.yaml. Every
// handler is wrapped by the gate of its route: a connection or CI token
// without the route's right gets 403 missing_right, and NoToken routes
// answer 403 to them.
func Routes(h Handlers, m Middleware) []Route {
	routes := []Route{
		{"GET /api/v1/openapi.yaml", OpenAPI, model.RightRead},
		{"GET /api/v1/csrf", CSRFToken, model.RightRead},
		{"GET /api/v1/meta", h.Meta.Meta, model.RightRead},
		{"GET /api/v1/version", m.RequireOnboarded(h.Version.Show), model.RightRead},
		{"POST /api/v1/version/update", m.InWorkspace(h.Version.Update), model.RightServers},
		{"POST /api/v1/auth/email-change/verify", h.Auth.VerifyEmailChange, NoToken},
		{"POST /api/v1/auth/device", m.RateLimitDevice(h.Device.Start), NoToken},
		{"POST /api/v1/auth/token", m.RateLimitDevicePoll(h.Token.Token), NoToken},
		{"POST /api/v1/auth/revoke", m.RateLimitDevicePoll(h.Token.Revoke), NoToken},
		{"POST /api/v1/auth/onboarding", m.RequireAuth(h.Auth.CompleteOnboarding), NoToken},
		{"POST /api/v1/auth/logout", m.RequireAuth(h.Auth.Logout), NoToken},
		{"GET /api/v1/account", m.RequireAuth(h.Auth.Account), model.RightRead},
		{"PATCH /api/v1/account/name", m.RequireOnboarded(h.Account.UpdateName), model.RightServers},
		{"PUT /api/v1/account/email-alerts", m.RequireOnboarded(h.Account.SetEmailAlerts), model.RightServers},
		{"PATCH /api/v1/account/email", m.RequireOnboarded(m.RateLimitEmailChanges(h.Account.ChangeEmail)), NoToken},
		{"DELETE /api/v1/account", m.RequireOnboarded(h.Account.DeleteAccount), NoToken},
		{"GET /api/v1/account/sessions", m.RequireOnboarded(h.Sessions.List), NoToken},
		{"DELETE /api/v1/account/sessions/{id}", m.RequireOnboarded(h.Sessions.Revoke), NoToken},
		{"DELETE /api/v1/account/sessions", m.RequireOnboarded(h.Sessions.RevokeOthers), NoToken},
		{"GET /api/v1/account/passkeys", m.RequireOnboarded(h.Passkeys.List), NoToken},
		{"DELETE /api/v1/account/passkeys/{id}", m.RequireOnboarded(h.Passkeys.Remove), NoToken},
		{"GET /api/v1/account/connections", m.RequireOnboarded(h.Connections.List), model.RightRead},
		{"PATCH /api/v1/account/connections/{id}", m.RequireOnboarded(h.Connections.SetRights), NoToken},
		{"DELETE /api/v1/account/connections/{id}", m.RequireOnboarded(h.Connections.Remove), model.RightRead},
		{"GET /api/v1/ci-tokens", m.InWorkspace(h.CITokens.List), model.RightRead},
		{"POST /api/v1/ci-tokens", m.InWorkspace(h.CITokens.Create), NoToken},
		{"PATCH /api/v1/ci-tokens/{id}", m.InWorkspace(h.CITokens.SetRights), NoToken},
		{"DELETE /api/v1/ci-tokens/{id}", m.InWorkspace(h.CITokens.Remove), model.RightRead},
		{"POST /api/v1/account/avatar", m.RequireOnboarded(h.Account.UploadAvatar), model.RightServers},
		{"DELETE /api/v1/account/avatar", m.RequireOnboarded(h.Account.DeleteAvatar), model.RightServers},
		{"GET /api/v1/workspaces", m.RequireOnboarded(h.Workspaces.List), model.RightRead},
		{"POST /api/v1/workspaces", m.RequireOnboarded(h.Workspaces.Create), NoToken},
		{"GET /api/v1/workspaces/{workspaceSlug}", m.Workspace(h.Workspaces.Show), model.RightRead},
		{"DELETE /api/v1/workspaces/{workspaceSlug}", m.Workspace(h.Workspaces.Delete), NoToken},
		{"PATCH /api/v1/workspaces/{workspaceSlug}/name", m.Workspace(h.Workspaces.UpdateName), model.RightServers},
		{"POST /api/v1/workspaces/{workspaceSlug}/invitations", m.Workspace(m.RateLimitInvitations(h.Workspaces.Invite)), model.RightServers},
		{"POST /api/v1/workspaces/{workspaceSlug}/invitations/{invitationID}/resend", m.Workspace(m.RateLimitInvitations(h.Workspaces.ResendInvitation)), model.RightServers},
		{"DELETE /api/v1/workspaces/{workspaceSlug}/invitations/{invitationID}", m.Workspace(h.Workspaces.RevokeInvitation), model.RightServers},
		{"PATCH /api/v1/workspaces/{workspaceSlug}/members/{memberID}/role", m.Workspace(h.Workspaces.ChangeRole), NoToken},
		{"DELETE /api/v1/workspaces/{workspaceSlug}/members/{memberID}", m.Workspace(h.Workspaces.RemoveMember), NoToken},
		{"POST /api/v1/workspaces/{workspaceSlug}/leave", m.Workspace(h.Workspaces.Leave), NoToken},
		{"GET /api/v1/workspaces/{workspaceSlug}/activity", m.Workspace(h.Workspaces.Activity), model.RightRead},
		{"GET /api/v1/activity", m.InWorkspace(h.Workspaces.Activity), model.RightRead},
		{"GET /api/v1/invitations/{token}", h.Invitations.Show, model.RightRead},
		{"POST /api/v1/invitations/{token}/accept", m.RequireOnboarded(h.Invitations.Accept), NoToken},
		{"GET /api/v1/workspaces/{workspaceSlug}/billing", m.Workspace(billingOn(h.Billing, h.Billing.Billing)), model.RightRead},
		{"POST /api/v1/workspaces/{workspaceSlug}/billing/checkout", m.Workspace(billingOn(h.Billing, h.Billing.CreateCheckout)), NoToken},
		{"GET /api/v1/workspaces/{workspaceSlug}/billing/manage", m.Workspace(billingOn(h.Billing, h.Billing.BillingManagement)), NoToken},
		{"GET /api/v1/servers", m.InWorkspace(h.Servers.List), model.RightRead},
		{"POST /api/v1/servers", m.InWorkspace(h.Servers.Create), model.RightServers},
		{"DELETE /api/v1/servers/{id}", m.InWorkspace(h.Servers.Delete), model.RightServers | model.RightDelete},
		{"PATCH /api/v1/servers/{id}", m.InWorkspace(h.Servers.Rename), model.RightServers},
		{"POST /api/v1/servers/{id}/update", m.InWorkspace(h.Servers.Update), model.RightServers},
		{"POST /api/v1/servers/{id}/install", m.InWorkspace(h.Servers.NewInstall), model.RightServers},
		{"PUT /api/v1/servers/{id}/security", m.InWorkspace(h.Apps.SetSecurity), model.RightServers},
		{"GET /api/v1/servers/{id}/metrics", m.InWorkspace(h.Apps.ServerMetrics), model.RightRead},
		{"GET /api/v1/integrations", m.InWorkspace(h.Apps.GetIntegrations), model.RightRead},
		{"PUT /api/v1/integrations", m.InWorkspace(h.Apps.SetIntegrations), model.RightServers},
		{"GET /api/v1/daemon-updates", m.InWorkspace(h.Version.DaemonUpdates), model.RightRead},
		{"PUT /api/v1/daemon-updates", m.InWorkspace(h.Version.SetDaemonUpdates), model.RightServers},
		{"GET /api/v1/servers/events", m.InWorkspace(streams.wrap(h.Servers.Events)), model.RightRead},
		{"GET /api/v1/status", m.InWorkspace(h.Status.Get), model.RightRead},
		{"GET /api/v1/catalog", m.InWorkspace(h.Catalog.List), model.RightRead},
		{"GET /api/v1/catalog/{slug}", m.InWorkspace(h.Catalog.Get), model.RightRead},
		{"POST /api/v1/catalog/{slug}/install", m.InWorkspace(h.Catalog.Install), model.RightDeploy},
		{"GET /api/v1/apps", m.InWorkspace(h.Apps.List), model.RightRead},
		{"POST /api/v1/apps", m.InWorkspace(h.Apps.Create), model.RightDeploy},
		{"PUT /api/v1/apps/{app}/pin", m.InWorkspace(h.Apps.SetPinned), model.RightDeploy},
		{"GET /api/v1/apps/events", m.InWorkspace(streams.wrap(h.Apps.Events)), model.RightRead},
		{"GET /api/v1/apps/{app}", m.InWorkspace(h.Apps.Get), model.RightRead},
		{"PATCH /api/v1/apps/{app}", m.InWorkspace(h.Apps.Rename), model.RightDeploy},
		{"DELETE /api/v1/apps/{app}", m.InWorkspace(h.Apps.Delete), model.RightDelete},
		{"POST /api/v1/apps/{app}/deploys", m.InWorkspace(h.Apps.CreateDeploy), model.RightDeploy},
		{"GET /api/v1/apps/{app}/deploys/{number}", m.InWorkspace(h.Apps.GetDeploy), model.RightRead},
		{"GET /api/v1/apps/{app}/deploys/{number}/events", m.InWorkspace(streams.wrap(h.Apps.DeployEvents)), model.RightRead},
		{"POST /api/v1/apps/{app}/rollback", m.InWorkspace(h.Apps.Rollback), model.RightDeploy},
		{"POST /api/v1/apps/{app}/update", m.InWorkspace(h.Catalog.Update), model.RightDeploy},
		{"POST /api/v1/apps/{app}/redeploy", m.InWorkspace(h.Apps.Redeploy), model.RightDeploy},
		{"POST /api/v1/apps/{app}/hook", m.InWorkspace(h.Apps.RotateHook), model.RightDeploy},
		{"DELETE /api/v1/apps/{app}/hook", m.InWorkspace(h.Apps.RemoveHook), model.RightDeploy},
		{"POST /api/v1/apps/{app}/restart", m.InWorkspace(h.Apps.Restart), model.RightDeploy},
		{"POST /api/v1/apps/{app}/start", m.InWorkspace(h.Apps.Start), model.RightDeploy},
		{"POST /api/v1/apps/{app}/stop", m.InWorkspace(h.Apps.Stop), model.RightDeploy},
		{"GET /api/v1/apps/{app}/domains", m.InWorkspace(h.Apps.Domains), model.RightRead},
		{"PUT /api/v1/apps/{app}/domains", m.InWorkspace(h.Apps.SetDomains), model.RightDeploy},
		{"GET /api/v1/apps/{app}/instant", m.InWorkspace(h.Apps.GetInstant), model.RightRead},
		{"PUT /api/v1/apps/{app}/instant", m.InWorkspace(h.Apps.SetInstant), model.RightDeploy},
		{"PUT /api/v1/apps/{app}/protection", m.InWorkspace(h.Apps.SetProtection), model.RightEnv},
		{"DELETE /api/v1/apps/{app}/protection", m.InWorkspace(h.Apps.DeleteProtection), model.RightEnv},
		{"GET /api/v1/registries", m.InWorkspace(h.Apps.Registries), model.RightRead},
		{"PUT /api/v1/registries/{host}", m.InWorkspace(h.Apps.SetRegistry), model.RightServers},
		{"DELETE /api/v1/registries/{host}", m.InWorkspace(h.Apps.DeleteRegistry), model.RightServers},
		{"GET /api/v1/apps/{app}/logs", m.InWorkspace(h.Apps.Logs), model.RightRead},
		{"GET /api/v1/apps/{app}/compose", m.InWorkspace(h.Apps.ComposeFile), model.RightRead},
		{"POST /api/v1/apps/{app}/exec", m.InWorkspace(h.Apps.Exec), model.RightDeploy},
		{"GET /api/v1/apps/{app}/databases", m.InWorkspace(h.Apps.Databases), model.RightRead},
		{"GET /api/v1/apps/{app}/databases/tables", m.InWorkspace(h.Apps.Tables), model.RightRead},
		{"GET /api/v1/apps/{app}/databases/rows", m.InWorkspace(h.Apps.Rows), model.RightRead},
		{"POST /api/v1/apps/{app}/databases/rows", m.InWorkspace(h.Apps.InsertRow), model.RightDelete},
		{"PATCH /api/v1/apps/{app}/databases/rows", m.InWorkspace(h.Apps.UpdateRow), model.RightDelete},
		{"DELETE /api/v1/apps/{app}/databases/rows", m.InWorkspace(h.Apps.DeleteRow), model.RightDelete},
		{"POST /api/v1/apps/{app}/databases/query", m.InWorkspace(h.Apps.Query), model.RightRead},
		{"GET /api/v1/apps/{app}/backups", m.InWorkspace(h.Apps.Backups), model.RightRead},
		{"PUT /api/v1/apps/{app}/backups", m.InWorkspace(h.Apps.SetBackupSchedule), model.RightDeploy},
		{"POST /api/v1/apps/{app}/backups/download", m.InWorkspace(h.Apps.Download), model.RightRead},
		{"POST /api/v1/apps/{app}/restores", m.InWorkspace(h.Apps.StartRestore), model.RightDelete},
		{"GET /api/v1/apps/{app}/restores/{restore}", m.InWorkspace(h.Apps.Restore), model.RightRead},
		{"GET /api/v1/apps/{app}/uptime", m.InWorkspace(h.Apps.Uptime), model.RightRead},
		{"PUT /api/v1/apps/{app}/uptime/{domain}", m.InWorkspace(h.Apps.SetUptime), model.RightDeploy},
		{"GET /api/v1/alert-destinations", m.InWorkspace(h.Alerts.List), model.RightRead},
		{"POST /api/v1/alert-destinations", m.InWorkspace(h.Alerts.Create), model.RightServers},
		{"DELETE /api/v1/alert-destinations/{destination}", m.InWorkspace(h.Alerts.Delete), model.RightServers},
		{"POST /api/v1/alert-destinations/{destination}/test", m.InWorkspace(h.Alerts.Test), model.RightServers},
		{"GET /api/v1/apps/{app}/metrics", m.InWorkspace(h.Apps.AppMetrics), model.RightRead},
		{"GET /api/v1/github", m.InWorkspace(h.Apps.GetGitHub), model.RightRead},
		{"GET /api/v1/github/repos", m.InWorkspace(h.Apps.GetGitHubRepos), model.RightRead},
		{"GET /api/v1/github/repos/{owner}/{repo}/branches", m.InWorkspace(h.Apps.GetGitHubBranches), model.RightRead},
		{"PUT /api/v1/apps/{app}/repo", m.InWorkspace(h.Apps.SetRepo), model.RightDeploy},
		{"DELETE /api/v1/apps/{app}/repo", m.InWorkspace(h.Apps.DeleteRepo), model.RightDeploy},
		{"GET /api/v1/buckets", m.InWorkspace(h.Apps.Buckets), model.RightRead},
		{"POST /api/v1/buckets", m.InWorkspace(h.Apps.AddBucket), model.RightServers},
		{"GET /api/v1/buckets/{name}", m.InWorkspace(h.Apps.GetBucket), model.RightRead},
		{"PUT /api/v1/buckets/{name}", m.InWorkspace(h.Apps.EditBucket), model.RightServers},
		{"PUT /api/v1/buckets/{name}/key", m.InWorkspace(h.Apps.SetBucketKey), model.RightServers},
		{"DELETE /api/v1/buckets/{name}", m.InWorkspace(h.Apps.DeleteBucket), model.RightDelete},
		{"PUT /api/v1/apps/{app}/bucket", m.InWorkspace(h.Apps.SetAppBucket), model.RightDeploy},
		{"GET /api/v1/apps/{app}/env", m.InWorkspace(h.Apps.GetEnv), model.RightRead},
		{"PATCH /api/v1/apps/{app}/env", m.InWorkspace(h.Apps.UpdateEnv), model.RightEnv},
		{"PUT /api/v1/apps/{app}/env", m.InWorkspace(h.Apps.ReplaceEnv), model.RightEnv},
		{"GET /api/v1/admin/users", hostedOnly(h.Billing, m.RequireOnboarded(m.RequireAdmin(h.Admin.Users))), NoToken},
		{"GET /api/v1/admin/old-domain", hostedOnly(h.Billing, m.RequireOnboarded(m.RequireAdmin(h.Admin.OldDomain))), NoToken},
		{"POST /api/v1/admin/users/{userID}/sessions/revoke", hostedOnly(h.Billing, m.RequireOnboarded(m.RequireAdmin(h.Admin.RevokeUserSessions))), NoToken},
	}
	for index := range routes {
		routes[index].Handler = routes[index].gate(m.AppID)
	}
	return routes
}

// hostedOnly answers 404 on a self-hosted hub (billing off), for
// what only running moin as a service needs.
func hostedOnly(billing *BillingHandler, next http.HandlerFunc) http.HandlerFunc {
	if billing != nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

// billingOn answers 404 on a hub with billing off.
func billingOn(billing *BillingHandler, next http.HandlerFunc) http.HandlerFunc {
	if billing != nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		Error(w, http.StatusNotFound, "not_found", "Billing is off on this hub.")
	}
}
