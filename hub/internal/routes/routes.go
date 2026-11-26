package routes

import (
	"bytes"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/assets"
	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/app"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/handler"
	"github.com/axadrn/moin/hub/internal/mcp"
	"github.com/axadrn/moin/hub/internal/middleware"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// limiter wraps a handler in a rate limit.
type limiter = func(http.HandlerFunc) http.HandlerFunc

// SetupRoutes registers every route, area by area, and wraps them in the
// middleware every request goes through. The web, the API and MCP share
// their rate limits, so a sign-in or an invitation counts once wherever it
// comes from.
func SetupRoutes(app *app.App) http.Handler {
	clientIPs := middleware.NewClientIPResolver(app.Cfg.TrustedProxyCIDRs)
	rateLimiter := middleware.RateLimitAuth(clientIPs)
	deviceLimiter := middleware.RateLimitDeviceLogin(clientIPs)
	emailChanges := middleware.RateLimitUserEmailChanges()
	// Web, API and MCP invitations share one budget.
	invitationLimiter := middleware.NewWorkspaceInvitationLimiter()

	home := handler.NewHomeHandler(app.TelemetryService, app.Cfg.Billing, app.Texts)
	catalogPages := handler.NewCatalogHandler(app.CatalogService, app.AppService, app.ServerService, app.WorkspaceService, app.Cfg.AppURL)
	mux := http.NewServeMux()
	siteRoutes(mux, app, home, catalogPages)
	// Signing in with a passkey and adding one share one budget.
	passkeyLimiter := middleware.RateLimitPasskeys(clientIPs)
	authRoutes(mux, app, rateLimiter, deviceLimiter, middleware.RateLimitGitHubLogin(clientIPs), passkeyLimiter)
	workspaceRoutes(mux, app, emailChanges, middleware.RateLimitWorkspaceInvitations(invitationLimiter), passkeyLimiter)
	appRoutes(mux, app, catalogPages)
	adminRoutes(mux, app)
	daemonRoutes(mux, app, clientIPs)
	apiRoutes(mux, app, clientIPs, deviceLimiter, emailChanges, invitationLimiter)
	mcpRoutes(mux, app, invitationLimiter)
	mux.HandleFunc("/api/", middleware.APINotFound)
	mux.HandleFunc("/{path...}", home.NotFoundPage)

	securityHeaderConfig := middleware.SecurityHeadersConfig{
		RobotsNoIndex: app.Cfg.RobotsNoIndex,
		S3Endpoint:    app.Cfg.S3Endpoint,
		UmamiHost:     app.Cfg.UmamiHost,
		PlausibleHost: app.Cfg.PlausibleHost,
		GitHubURL:     app.Cfg.GitHubURL,
	}
	securityHeaders := func(next http.Handler) http.Handler {
		return middleware.SecurityHeaders(securityHeaderConfig, next)
	}
	return middleware.Chain(
		mux,
		middleware.RenderConfig(renderConfig(app)),
		middleware.NonceMiddleware,
		securityHeaders,
		middleware.RequestLogging(app.Cfg.LogHTTP, clientIPs),
		middleware.Client(clientIPs),
		middleware.OldDomain(app.OldDomainService.Hosts(), app.Cfg.AppURL, app.OldDomainService.Count),
		middleware.RefuseOutdatedClients(app.Cfg.AppURL),
		middleware.CORS(app.Cfg.CORSAllowedOrigins),
		middleware.CSRFProtection(app.Cfg.IsProduction(), clientIPs),
		middleware.AuthMiddleware(app.AuthService, app.UserService),
		middleware.BearerAuth(app.ConnectionService, app.CITokenService, app.UserService),
		middleware.RateLimitBearers(),
		middleware.GitHubApp(app.GitHubAppService),
		middleware.BlockImpersonatedWrites,
		middleware.AdminContext(app.AdminService),
		middleware.WithURLPath,
	)
}

// renderConfig is what templates may know of the configuration, with
// whether this hub offers passkeys.
func renderConfig(app *app.App) config.RenderConfig {
	render := app.Cfg.Render()
	render.Passkeys = app.Passkeys.On()
	return render
}

// uiAssets serves the ui folder under /assets/ui/. Only a block may be shown
// in a frame, by this site's own preview.
func uiAssets(app *app.App) http.Handler {
	files := http.FileServer(http.FS(app.UIFiles))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if app.Cfg.IsDevelopment() {
			w.Header().Set("Cache-Control", "no-store")
		}
		if strings.HasPrefix(r.URL.Path, "blocks/") {
			middleware.AllowOwnFrames(w)
		}
		files.ServeHTTP(w, r)
	})
}

// assetHandler serves the files under assets: from disk in development,
// without caching, else from the binary. Pages link them with the version
// in the path, which caches them (versioned).
func assetHandler(app *app.App) http.Handler {
	if app.Cfg.IsDevelopment() {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			http.FileServer(http.Dir("./assets")).ServeHTTP(w, r)
		})
	}
	sub, _ := fs.Sub(assets.AssetsFS, ".")
	return http.FileServer(http.FS(sub))
}

// assetRoutes serve the files under /assets/: assets/, ui/, the bundles
// app.css and app.js, each also under the version (versioned).
func assetRoutes(mux *http.ServeMux, app *app.App) {
	assetHandler := assetHandler(app)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", middleware.Gzip(assetHandler)))
	mux.Handle("GET /assets/ui/", http.StripPrefix("/assets/ui/", middleware.Gzip(uiAssets(app))))
	mux.Handle("GET /assets/app.css", middleware.Gzip(bundle(app, "text/css; charset=utf-8", assets.CSS)))
	mux.Handle("GET /assets/app.js", middleware.Gzip(bundle(app, "text/javascript; charset=utf-8", assets.JS)))
	prefix := "/assets/" + build.Version + "/"
	mux.Handle("GET "+prefix, versioned(mux, prefix, app.Cfg.IsDevelopment()))
	// Browsers, link previews and analytics ask for /favicon.ico at the root.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		asset := r.Clone(r.Context())
		asset.URL.Path = "/img/favicon.ico"
		assetHandler.ServeHTTP(w, asset)
	})
}

// bundle serves the file join makes from ui/ and assets/: made once at
// start, or on every request in development, so an edit shows on reload.
func bundle(app *app.App, contentType string, join func(ui, hub fs.FS) ([]byte, error)) http.Handler {
	development := app.Cfg.IsDevelopment()
	hub := fs.FS(assets.AssetsFS)
	if development {
		hub = os.DirFS("assets")
	}
	made, err := join(app.UIFiles, hub)
	if err != nil && !development {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := made
		if development {
			w.Header().Set("Cache-Control", "no-store")
			var err error
			if body, err = join(app.UIFiles, hub); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", contentType)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	})
}

// versioned serves /assets/<version>/x as /assets/x. The pages link files
// so (components.AssetURL), and the version changes with every release and
// deploy, so browsers and CDNs may keep them for a year.
func versioned(mux *http.ServeMux, prefix string, development bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !development {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		plain := r.Clone(r.Context())
		plain.URL.Path = "/assets/" + strings.TrimPrefix(r.URL.Path, prefix)
		plain.URL.RawPath = ""
		mux.ServeHTTP(w, plain)
	})
}

// siteRoutes are the public site: assets, the home page, docs, search, the
// changelog, the blog, the catalog and the legal pages.
func siteRoutes(mux *http.ServeMux, app *app.App, home *handler.HomeHandler, catalogPages *handler.CatalogHandler) {
	seo := handler.NewSEOHandler(app.DocsService, app.UIDocsService, app.UIFiles, app.Cfg.AppURL, app.Texts, app.Cfg.RobotsNoIndex, app.Cfg.Billing)
	legal := handler.NewLegalHandler(app.LegalService)
	docs := handler.NewDocsHandler(app.DocsService)
	assetRoutes(mux, app)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /robots.txt", seo.Robots)
	mux.HandleFunc("GET /sitemap.xml", seo.Sitemap)
	mux.HandleFunc("GET /llms.txt", seo.LLMs)
	mux.HandleFunc("GET /{$}", home.HomePage)
	mux.HandleFunc("GET /docs", docs.ShowDocs)
	mux.HandleFunc("GET /docs.md", docs.ShowDocs)
	mux.HandleFunc("GET /docs/", docs.ShowDocs)
	mux.HandleFunc("GET /next", handler.Next)
	ui := handler.NewUIHandler(app.UIDocsService, app.UIFiles)
	mux.HandleFunc("GET /ui", ui.Show)
	mux.HandleFunc("GET /ui.md", ui.Show)
	mux.HandleFunc("GET /ui/", ui.Show)
	mux.HandleFunc("GET /search", handler.NewSiteSearchHandler(app.DocsService, app.UIDocsService, app.UIFiles, app.CatalogService).Search)
	mux.HandleFunc("GET /roadmap", handler.Roadmap(app.Texts))
	mux.HandleFunc("GET /changelog", handler.Changelog(app.Texts))
	mux.HandleFunc("GET /changelog.xml", handler.ChangelogFeed(app.Texts, app.Cfg.AppURL, build.Name))
	mux.HandleFunc("GET /blog", handler.Blog(app.Texts))
	mux.HandleFunc("GET /blog/{slug}", handler.BlogPost(app.Texts))
	mux.HandleFunc("GET /blog.xml", handler.BlogFeed(app.Texts, app.Cfg.AppURL, build.Name))
	mux.HandleFunc("GET /catalog", catalogPages.List)
	mux.HandleFunc("GET /catalog/{slug}", catalogPages.Show)
	mux.HandleFunc("GET /catalog/{slug}/logo", catalogPages.Logo)
	mux.HandleFunc("GET /catalog/{slug}/install", middleware.RequireAuth(catalogPages.Start))
	// The legal pages describe the hosted service, and the comparisons sit
	// under its home page; a self-hosted hub answers them with not found.
	if app.Cfg.Billing {
		mux.HandleFunc("GET /legal/{page}", legal.ShowPage)
		mux.HandleFunc("GET /compare", handler.CompareIndex)
		mux.HandleFunc("GET /compare/{name}", handler.Compare)
	}
}

// authRoutes sign people in and out, start a workspace, take an invitation
// and approve moin login in the browser.
func authRoutes(mux *http.ServeMux, app *app.App, rateLimiter, deviceLimiter, githubLimiter, passkeyLimiter limiter) {
	auth := handler.NewAuthHandler(app.AuthService, app.WorkspaceService, app.GitHubAppService, app.Passkeys, app.Cfg)
	workspaceHandler := handler.NewWorkspaceHandler(app.WorkspaceService)
	workspaceInvitation := handler.NewWorkspaceInvitationHandler(app.WorkspaceService, app.Cfg)
	mux.HandleFunc("GET /auth", middleware.RequireGuest(auth.AuthPage))
	mux.HandleFunc("GET /auth/key", middleware.RequireGuest(auth.KeyPage))
	mux.HandleFunc("POST /auth/key", rateLimiter(middleware.RequireGuest(auth.SignInWithKey)))
	if app.Passkeys.On() {
		mux.HandleFunc("POST /auth/passkey/options", passkeyLimiter(middleware.RequireGuest(auth.PasskeyOptions)))
		mux.HandleFunc("POST /auth/passkey", passkeyLimiter(middleware.RequireGuest(auth.SignInWithPasskey)))
		mux.HandleFunc("POST /auth/confirm/passkey/options", passkeyLimiter(middleware.RequireAuth(auth.ConfirmPasskeyOptions)))
		mux.HandleFunc("POST /auth/confirm/passkey", passkeyLimiter(middleware.RequireAuth(auth.ConfirmWithPasskey)))
	}
	// Confirm it's you, in place, before what needs it: no sign-out.
	mux.HandleFunc("POST /auth/confirm/github", githubLimiter(middleware.RequireAuth(auth.ConfirmWithGitHub)))
	mux.HandleFunc("GET /auth/onboarding", middleware.RequireAuth(auth.OnboardingPage))
	mux.HandleFunc("GET /auth/github", githubLimiter(middleware.RequireGuest(auth.GitHubAuth)))
	mux.HandleFunc("POST /auth/github/connect", githubLimiter(middleware.RequireAuth(auth.ConnectGitHub)))
	mux.HandleFunc("GET /auth/github/callback", githubLimiter(auth.GitHubCallback))
	mux.HandleFunc("GET /auth/verify-email-change/{token}", auth.VerifyEmailChange)
	mux.HandleFunc("POST /auth/onboarding", middleware.RequireAuth(auth.CompleteOnboarding))
	mux.HandleFunc("GET /workspaces/new", middleware.RequireAuth(workspaceHandler.NewPage))
	mux.HandleFunc("POST /workspaces/new", middleware.RequireAuth(workspaceHandler.Create))
	mux.HandleFunc("POST /auth/logout", auth.Logout)
	mux.HandleFunc("GET /workspaces/invitations/{token}", workspaceInvitation.Show)
	mux.HandleFunc("POST /workspaces/invitations/{token}", middleware.RequireAuth(workspaceInvitation.Accept))
	// moin login in the browser: the page signs in first by itself.
	device := handler.NewDeviceHandler(app.DeviceLoginService, app.WorkspaceService, app.Passkeys, app.Cfg)
	mux.HandleFunc("GET /device", deviceLimiter(device.Show))
	if app.Passkeys.On() {
		mux.HandleFunc("POST /device/passkey/options", passkeyLimiter(middleware.RequireAuth(device.PasskeyOptions)))
	}
	mux.HandleFunc("POST /device/approve", deviceLimiter(middleware.RequireAuth(device.Approve)))
	mux.HandleFunc("POST /device/deny", deviceLimiter(middleware.RequireAuth(device.Deny)))
	// AI agents sign in on the same approval page, with OAuth; the page
	// shares the budget of moin login.
	oauth := handler.NewOAuthHandler(app.OAuthService, app.WorkspaceService, app.Passkeys, app.Cfg)
	mux.HandleFunc("GET /oauth/authorize", deviceLimiter(oauth.Authorize))
	if app.Passkeys.On() {
		mux.HandleFunc("POST /oauth/passkey/options", passkeyLimiter(middleware.RequireAuth(device.PasskeyOptions)))
	}
	mux.HandleFunc("POST /oauth/approve", deviceLimiter(middleware.RequireAuth(oauth.Approve)))
	mux.HandleFunc("POST /oauth/deny", deviceLimiter(middleware.RequireAuth(oauth.Deny)))
	// How an MCP client finds all of it: the metadata of /mcp (RFC 9728),
	// at its own path and at the root, and of the hub as an authorization
	// server (RFC 8414).
	metadata := api.NewOAuthHandler(app.OAuthService)
	mux.HandleFunc("GET "+api.ProtectedResourcePath, metadata.ProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", metadata.ProtectedResource)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", metadata.AuthorizationServer)
}

// inWorkspace is the middleware of every page under /app/{workspaceSlug}:
// signed in, and a member of the workspace.
func inWorkspace(app *app.App) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return middleware.RequireAuth(middleware.WithWorkspace(app.WorkspaceService, app.SubscriptionService)(next))
	}
}

// workspaceRoutes are the pages of a workspace around its servers and apps:
// the overview, settings, the account, billing, the team and alerts.
func workspaceRoutes(mux *http.ServeMux, app *app.App, emailChanges, limitInvitations, passkeyLimiter limiter) {
	workspace := inWorkspace(app)
	settings := handler.NewSettingsHandler(app.AuthService, app.Passkeys, app.AppService, app.UpdateService)
	connections := handler.NewConnectionsHandler(app.ConnectionService, app.CITokenService, app.AppService, app.Passkeys)
	profile := handler.NewProfileHandler(app.UserService)
	account := handler.NewAccountHandler(app.AuthService, app.UserService, app.FileService)
	workspaceHandler := handler.NewWorkspaceHandler(app.WorkspaceService)
	mux.HandleFunc("GET /app", middleware.RequireAuth(workspaceHandler.Redirect))
	// Sign-ins and invitations lead to the overview, and a workspace's own
	// path too.
	overview := handler.NewOverviewHandler(app.StatusService, app.AppService, app.ServerService)
	toOverview := workspace(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/overview", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /app/{workspaceSlug}", toOverview)
	mux.HandleFunc("GET /app/{workspaceSlug}/overview", workspace(overview.Page))
	mux.HandleFunc("GET /app/{workspaceSlug}/search", workspace(handler.NewCommandHandler(app.AppService, app.ServerService, app.DocsService, app.CatalogService).Search))
	mux.HandleFunc("GET /app/{workspaceSlug}/settings", workspace(settings.Home))
	mux.HandleFunc("GET /app/{workspaceSlug}/settings/account", workspace(settings.SettingsPage))
	mux.HandleFunc("GET /app/{workspaceSlug}/integrations", workspace(settings.Integrations))
	mux.HandleFunc("POST /app/{workspaceSlug}/integrations", workspace(settings.SetIntegrations))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/settings/sessions/{id}", workspace(settings.RevokeSession))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/settings/sessions", workspace(settings.RevokeAllSessions))
	if app.Passkeys.On() {
		mux.HandleFunc("POST /app/{workspaceSlug}/settings/passkeys/options", passkeyLimiter(workspace(settings.PasskeyOptions)))
		mux.HandleFunc("POST /app/{workspaceSlug}/settings/passkeys", passkeyLimiter(workspace(settings.AddPasskey)))
	}
	mux.HandleFunc("PATCH /app/{workspaceSlug}/settings/passkeys/{id}", workspace(settings.RenamePasskey))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/settings/passkeys/{id}", workspace(settings.RemovePasskey))
	mux.HandleFunc("GET /app/{workspaceSlug}/settings/connections", workspace(connections.Page))
	mux.HandleFunc("GET /app/{workspaceSlug}/settings/connections/{id}", workspace(connections.Connection))
	mux.HandleFunc("POST /app/{workspaceSlug}/settings/connections/{id}/rights/{right}", workspace(connections.SetConnectionRight))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/settings/connections/{id}", workspace(connections.RemoveConnection))
	mux.HandleFunc("POST /app/{workspaceSlug}/settings/ci-tokens", workspace(connections.CreateCIToken))
	mux.HandleFunc("GET /app/{workspaceSlug}/settings/ci-tokens/{id}", workspace(connections.CIToken))
	mux.HandleFunc("POST /app/{workspaceSlug}/settings/ci-tokens/{id}/rights/{right}", workspace(connections.SetCITokenRight))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/settings/ci-tokens/{id}", workspace(connections.RemoveCIToken))
	mux.HandleFunc("PATCH /app/{workspaceSlug}/profile/name", workspace(profile.UpdateName))
	mux.HandleFunc("POST /app/{workspaceSlug}/profile/email-alerts", workspace(profile.EmailAlerts))
	mux.HandleFunc("PATCH /app/{workspaceSlug}/account/email", workspace(emailChanges(account.ChangeEmail)))
	mux.HandleFunc("POST /app/{workspaceSlug}/account/avatar", workspace(account.UploadAvatar))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/account/avatar", workspace(account.DeleteAvatar))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/account", workspace(account.DeleteAccount))
	if app.Cfg.Billing {
		billing := handler.NewBillingHandler(app.PaymentService, app.WorkspaceService)
		mux.HandleFunc("GET /app/{workspaceSlug}/billing", workspace(billing.BillingPage))
		mux.HandleFunc("POST /app/{workspaceSlug}/billing/checkout", workspace(billing.CreateCheckout))
		mux.HandleFunc("GET /app/{workspaceSlug}/billing/manage", workspace(billing.BillingManagement))
		mux.HandleFunc("POST /webhooks/payment", billing.Webhook)
	}
	mux.HandleFunc("GET /app/{workspaceSlug}/workspace", workspace(workspaceHandler.Page))
	mux.HandleFunc("GET /app/{workspaceSlug}/activity", workspace(workspaceHandler.ActivityPage))
	alerts := handler.NewAlertsHandler(app.AlertService)
	mux.HandleFunc("GET /app/{workspaceSlug}/alerts", workspace(alerts.Page))
	mux.HandleFunc("POST /app/{workspaceSlug}/alerts/destinations", workspace(alerts.Add))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/alerts/destinations/{destination}", workspace(alerts.Remove))
	mux.HandleFunc("POST /app/{workspaceSlug}/alerts/destinations/{destination}/test", workspace(alerts.Test))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/name", workspace(workspaceHandler.UpdateName))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/invitations", workspace(limitInvitations(workspaceHandler.Invite)))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/invitations/{invitationID}/resend", workspace(limitInvitations(workspaceHandler.ResendInvitation)))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/invitations/{invitationID}/revoke", workspace(workspaceHandler.RevokeInvitation))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/members/{memberID}/role", workspace(workspaceHandler.ChangeRole))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/members/{memberID}/remove", workspace(workspaceHandler.RemoveMember))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/leave", workspace(workspaceHandler.Leave))
	mux.HandleFunc("POST /app/{workspaceSlug}/workspace/delete", workspace(workspaceHandler.Delete))
}

// appRoutes are the pages of servers and apps: deploys, domains, data,
// backups, uptime, metrics, buckets, registries, env, GitHub and the catalog.
func appRoutes(mux *http.ServeMux, app *app.App, catalogPages *handler.CatalogHandler) {
	workspace := inWorkspace(app)
	servers := handler.NewServersHandler(app.ServerService, app.UserService)
	mux.HandleFunc("GET /app/{workspaceSlug}/servers", workspace(servers.ServersPage))
	mux.HandleFunc("GET /app/{workspaceSlug}/server-list", workspace(servers.List))
	mux.HandleFunc("POST /app/{workspaceSlug}/servers", workspace(servers.Create))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/servers/{id}", workspace(servers.Delete))
	mux.HandleFunc("POST /app/{workspaceSlug}/servers/{id}/update", workspace(servers.UpdateDaemon))
	mux.HandleFunc("POST /app/{workspaceSlug}/servers/daemon-updates", workspace(servers.DaemonUpdates))
	updates := handler.NewUpdateHandler(app.UpdateService)
	mux.HandleFunc("GET /app/{workspaceSlug}/update", workspace(updates.Notice))
	mux.HandleFunc("POST /app/{workspaceSlug}/update", workspace(updates.Start))
	mux.HandleFunc("POST /app/{workspaceSlug}/servers/{id}/install", workspace(servers.NewInstall))
	mux.HandleFunc("GET /app/{workspaceSlug}/servers/{id}/install", workspace(servers.Install))
	apps := handler.NewAppsHandler(app.AppService, app.ServerService, app.UserService, app.CatalogService)
	mux.HandleFunc("GET /app/{workspaceSlug}/catalog", workspace(catalogPages.InApp))
	mux.HandleFunc("GET /app/{workspaceSlug}/catalog/{slug}", workspace(catalogPages.InstallPage))
	mux.HandleFunc("POST /app/{workspaceSlug}/catalog/{slug}/install", workspace(catalogPages.Install))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/catalog", workspace(catalogPages.UpdatePanel))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/update", workspace(catalogPages.Update))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps", workspace(apps.AppsPage))
	mux.HandleFunc("GET /app/{workspaceSlug}/new-app", workspace(apps.NewPage))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/pin", workspace(apps.SetPinned))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps", workspace(apps.Create))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}", workspace(apps.AppSection("app")))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/deploys", workspace(apps.AppSection("deploys")))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/deploys/{number}", workspace(apps.DeployPage))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/deploys/{number}/log", workspace(apps.DeployLog))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/domains", workspace(apps.AppSection("domains")))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/settings", workspace(apps.AppSection("settings")))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/compose.yml", workspace(apps.ComposeFile))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/name", workspace(apps.RenameApp))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/detail", workspace(apps.Detail))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/apps/{app}", workspace(apps.Delete))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/rollback", workspace(apps.Rollback))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/redeploy", workspace(apps.Redeploy))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/restart", workspace(apps.Restart))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/start", workspace(apps.Start))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/stop", workspace(apps.Stop))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/image", workspace(apps.DeployImage))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/hook", workspace(apps.RotateHook))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/apps/{app}/hook", workspace(apps.RemoveHook))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/domains/detail", workspace(apps.DomainsDetail))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/domains/add", workspace(apps.AddDomain))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/domains/remove", workspace(apps.RemoveDomain))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/domains/canonical", workspace(apps.MakeCanonical))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/domains/password", workspace(apps.Password))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/domains/instant", workspace(apps.SetInstant))
	mux.HandleFunc("GET /app/{workspaceSlug}/registries", workspace(apps.Registries))
	mux.HandleFunc("POST /app/{workspaceSlug}/registries", workspace(apps.SetRegistry))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/registries/{host}", workspace(apps.DeleteRegistry))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/logs", workspace(apps.AppSection("logs")))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/logs/lines", workspace(apps.Logs))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/data", workspace(apps.Data))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/data/cell", workspace(apps.DataCell))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/data/delete", workspace(apps.DataDelete))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/data/query", workspace(apps.DataQuery))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/data/insert", workspace(apps.DataInsert))
	mux.HandleFunc("GET /app/{workspaceSlug}/servers/{id}/security", workspace(apps.ServerSecurity))
	mux.HandleFunc("POST /app/{workspaceSlug}/servers/{id}/security", workspace(apps.SetServerSecurity))
	mux.HandleFunc("POST /app/{workspaceSlug}/servers/{id}/name", workspace(apps.RenameServer))
	mux.HandleFunc("GET /app/{workspaceSlug}/github/install", workspace(apps.GitHubInstall))
	mux.HandleFunc("GET /app/github/setup", middleware.RequireAuth(apps.GitHubSetup))
	githubApp := handler.NewGitHubAppHandler(app.GitHubAppService, app.WorkspaceService)
	mux.HandleFunc("GET /app/github/created", middleware.RequireAuth(githubApp.Created))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/repo", workspace(apps.Repo))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/repo", workspace(apps.LinkRepo))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/repo/branches", workspace(apps.RepoBranches))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/repo/auto-deploy", workspace(apps.SetAutoDeploy))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/repo/unlink", workspace(apps.UnlinkRepo))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/repo/deploy", workspace(apps.DeployRef))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/backups/restore", workspace(apps.BackupRestore))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/backups/download", workspace(apps.BackupDownload))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/backups/schedule", workspace(apps.BackupSchedule))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/uptime", workspace(apps.Uptime))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/uptime/path", workspace(apps.UptimePath))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/uptime/on", workspace(apps.UptimeOn))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/metrics", workspace(apps.AppMetrics))
	mux.HandleFunc("GET /app/{workspaceSlug}/servers/{id}/metrics", workspace(apps.ServerMetrics))
	mux.HandleFunc("GET /app/{workspaceSlug}/bucket", workspace(apps.Bucket))
	mux.HandleFunc("POST /app/{workspaceSlug}/bucket", workspace(apps.AddBucket))
	mux.HandleFunc("POST /app/{workspaceSlug}/bucket/{name}", workspace(apps.EditBucket))
	mux.HandleFunc("POST /app/{workspaceSlug}/bucket/{name}/default", workspace(apps.DefaultBucket))
	mux.HandleFunc("POST /app/{workspaceSlug}/bucket/{name}/key", workspace(apps.SetBucketKey))
	mux.HandleFunc("POST /app/{workspaceSlug}/bucket/{name}/remove", workspace(apps.RemoveBucket))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/bucket", workspace(apps.SetAppBucket))
	mux.HandleFunc("GET /app/{workspaceSlug}/apps/{app}/env", workspace(apps.Env))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/env", workspace(apps.SetEnv))
	mux.HandleFunc("POST /app/{workspaceSlug}/apps/{app}/env/text", workspace(apps.EnvText))
	mux.HandleFunc("DELETE /app/{workspaceSlug}/apps/{app}/env/{key}", workspace(apps.UnsetEnv))
}

// adminRoutes are the operator's Admin. Admin is for running moin as a
// service: only the hosted hub has it. A self-hosted one manages
// its people under Team.
func adminRoutes(mux *http.ServeMux, app *app.App) {
	if !app.Cfg.Billing {
		return
	}
	admin := handler.NewAdminHandler(app.AdminService)
	adminRoute := func(next http.HandlerFunc) http.HandlerFunc {
		return middleware.RequireAuth(middleware.RequireAdmin(next))
	}
	mux.HandleFunc("GET /admin", adminRoute(admin.Overview))
	mux.HandleFunc("GET /admin/old-domain", adminRoute(admin.OldDomain))
	mux.HandleFunc("GET /admin/activity", adminRoute(admin.Activity))
	mux.HandleFunc("GET /admin/users/{userID}", adminRoute(admin.User))
	mux.HandleFunc("POST /admin/users/{userID}/sessions/revoke", adminRoute(admin.RevokeUserSessions))
	mux.HandleFunc("POST /admin/users/{userID}/impersonate", adminRoute(admin.StartImpersonation))
	mux.HandleFunc("POST /admin/users/{userID}/forever-free", adminRoute(admin.ForeverFree))
	mux.HandleFunc("POST /admin/users/{userID}/offer", adminRoute(admin.ApplyOffer))
	mux.HandleFunc("POST /admin/users/{userID}/note", adminRoute(admin.SaveUserNote))
	mux.HandleFunc("GET /admin/offers", adminRoute(admin.Offers))
	mux.HandleFunc("POST /admin/offers", adminRoute(admin.CreateOffer))
	mux.HandleFunc("POST /admin/offers/delete", adminRoute(admin.DeleteOffer))
	mux.HandleFunc("POST /admin/offers/claim", adminRoute(admin.ClaimOffers))
	mux.HandleFunc("POST /admin/impersonation/stop", admin.StopImpersonation)
}

// daemonRoutes are what servers and CI call: the install scripts and
// binaries, the daemon's register and session, deploy hooks and the
// webhooks of GitHub and telemetry.
func daemonRoutes(mux *http.ServeMux, app *app.App, clientIPs middleware.ClientIPResolver) {
	daemon := handler.NewDaemonHandler(app.ServerService, app.Cfg.AppURL, clientIPs, app.OldDomainService.Hosts())
	mux.HandleFunc("GET /install.sh", daemon.InstallScript)
	mux.HandleFunc("GET /cli.sh", daemon.CLIScript)
	mux.Handle("GET /download/", http.StripPrefix("/download/", http.FileServer(http.Dir("downloads"))))
	// Daemons get their own budget: many servers behind one NAT must not lock
	// out sign-ins, and install tokens are too long to guess.
	registerLimiter := middleware.RateLimit(clientIPs, 30, time.Minute)
	mux.HandleFunc("POST "+protocol.RegisterPath, registerLimiter(daemon.Register))
	// CI calls deploy hooks; the token is the credential, the limit stops guessing.
	hooks := handler.NewHooksHandler(app.AppService)
	// The hosted hub counts what self-hosted ones report.
	if app.Cfg.Billing {
		mux.HandleFunc("POST /webhooks/telemetry", registerLimiter(handler.NewTelemetryHandler(app.TelemetryService).Receive))
	}
	mux.HandleFunc("POST /webhooks/deploy/{token}", registerLimiter(hooks.Deploy))
	mux.HandleFunc("POST /webhooks/github", hooks.GitHubWebhook)
	mux.HandleFunc("GET "+protocol.SessionPath, daemon.Session)
	// Daemons from before protocol 3 dial the old path until they update.
	mux.HandleFunc("GET "+protocol.LegacySessionPath, daemon.Session)
}

// apiRoutes are the JSON API: every route comes from api.Routes.
func apiRoutes(mux *http.ServeMux, app *app.App, clientIPs middleware.ClientIPResolver, deviceLimiter, emailChanges limiter, invitationLimiter *middleware.RateLimiter) {
	var apiBilling *api.BillingHandler
	if app.Cfg.Billing {
		apiBilling = api.NewBillingHandler(app.PaymentService, app.WorkspaceService)
	}
	apiHandlers := api.Handlers{
		Meta:        api.NewMetaHandler(app.Cfg.SupportEmail),
		Auth:        api.NewAuthHandler(app.AuthService, app.UserService, app.WorkspaceService),
		Account:     api.NewAccountHandler(app.AuthService, app.UserService, app.FileService),
		Sessions:    api.NewSessionsHandler(app.AuthService),
		Passkeys:    api.NewPasskeysHandler(app.Passkeys),
		Connections: api.NewConnectionsHandler(app.ConnectionService, app.Passkeys),
		CITokens:    api.NewCITokensHandler(app.CITokenService, app.Passkeys),
		Token:       api.NewTokenHandler(app.DeviceLoginService, app.ConnectionService, app.OAuthService),
		Billing:     apiBilling,
		Admin:       api.NewAdminHandler(app.AdminService),
		Workspaces:  api.NewWorkspaceHandler(app.WorkspaceService),
		Invitations: api.NewWorkspaceInvitationHandler(app.WorkspaceService),
		Servers:     api.NewServersHandler(app.ServerService),
		Apps:        api.NewAppsHandler(app.AppService),
		Version:     api.NewVersionHandler(app.UpdateService, app.ServerService),
		Alerts:      api.NewAlertsHandler(app.AlertService),
		Catalog:     api.NewCatalogHandler(app.CatalogService),
		Status:      api.NewStatusHandler(app.StatusService),
		Device:      api.NewDeviceHandler(app.DeviceLoginService, app.Cfg.AppURL),
		OAuth:       api.NewOAuthHandler(app.OAuthService),
	}
	apiMiddleware := api.Middleware{
		RequireAuth:           middleware.RequireAuthAPI,
		RequireOnboarded:      middleware.RequireOnboardedAPI,
		RateLimitEmailChanges: emailChanges,
		RequireAdmin:          middleware.RequireAdminAPI,
		Workspace: func(next http.HandlerFunc) http.HandlerFunc {
			return middleware.RequireOnboardedAPI(middleware.WithWorkspaceAPI(app.WorkspaceService, app.SubscriptionService)(next))
		},
		RateLimitInvitations: middleware.RateLimitWorkspaceInvitations(invitationLimiter),
		InWorkspace: func(next http.HandlerFunc) http.HandlerFunc {
			return middleware.RequireOnboardedAPI(middleware.WithBearerWorkspace(app.WorkspaceService, app.SubscriptionService)(next))
		},
		RateLimitDevice:     deviceLimiter,
		RateLimitDevicePoll: middleware.RateLimitDevicePoll(clientIPs),
		// MCP clients register once per sign-in. claude.ai registers from
		// a few addresses for everyone, so the budget is generous.
		RateLimitRegister: middleware.RateLimit(clientIPs, 60, time.Hour),
		AppID: func(workspaceID, name string) (string, error) {
			found, err := app.AppService.Get(workspaceID, name)
			if err != nil {
				return "", err
			}
			return found.ID, nil
		},
	}
	for _, route := range api.Routes(apiHandlers, apiMiddleware) {
		mux.HandleFunc(route.Pattern, route.Handler)
	}
}

// mcpRoutes serve the MCP server at /mcp.
func mcpRoutes(mux *http.ServeMux, app *app.App, invitationLimiter *middleware.RateLimiter) {
	mcpServer := mcp.NewServer(mcp.Services{
		Users:         app.UserService,
		Workspaces:    app.WorkspaceService,
		Subscriptions: app.SubscriptionService,
		Invitations:   invitationLimiter,
		Servers:       app.ServerService,
		Apps:          app.AppService,
		Updates:       app.UpdateService,
		Alerts:        app.AlertService,
		Catalog:       app.CatalogService,
		Status:        app.StatusService,
		BillingOff:    !app.Cfg.Billing,
	})
	mux.Handle("/mcp", mcp.Handler(mcpServer, app.OAuthService.Issuer()+api.ProtectedResourcePath))
}
