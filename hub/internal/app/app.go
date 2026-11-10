package app

import (
	"fmt"
	"github.com/axadrn/moin"
	"github.com/axadrn/moin/hub/internal/access"
	"github.com/axadrn/moin/internal/build"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/axadrn/moin/hub/content"
	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/catalog"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/db"
	"github.com/axadrn/moin/hub/internal/dbbackup"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/service/mailer"
	"github.com/axadrn/moin/hub/internal/service/payment"
	"github.com/axadrn/moin/hub/internal/storage"
	"github.com/jmoiron/sqlx"
)

type App struct {
	Cfg              *config.Config
	DB               *sqlx.DB
	HistoryDB        *sqlx.DB
	OldDomainService *service.OldDomainService
	AuthService      *service.AuthService
	UserService      *service.UserService
	EmailService     *service.EmailService
	LegalService     *service.LegalService
	DocsService      *service.DocsService
	UIDocsService    *service.DocsService
	// Texts is content/ and UIFiles is ui/: from disk in development, else
	// from the binary.
	Texts               fs.FS
	UIFiles             fs.FS
	FileService         *service.FileService
	SubscriptionService *service.SubscriptionService
	AdminService        *service.AdminService
	BetaService         *service.BetaService
	PaymentService      payment.Provider
	TelemetryService    *service.TelemetryService
	UpdateService       *service.UpdateService
	WorkspaceService    *service.WorkspaceService
	APITokenService     *service.APITokenService
	DeviceLoginService  *service.DeviceLoginService
	ServerService       *service.ServerService
	AppService          *service.AppService
	CatalogService      *service.CatalogService
	StatusService       *service.StatusService
	AlertService        *service.AlertService
}

func New(cfg *config.Config) (*App, error) {
	database, err := db.Init(cfg.DBConnection)
	if err != nil {
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = database.Close()
		}
	}()
	// The data directory holds the database, its copies from before
	// migrations and the files the updater reads and writes.
	dataDir := "data"
	if file := dbbackup.File(cfg.DBConnection); file != "" {
		dataDir = filepath.Dir(file)
		if _, err := db.CopyBeforeMigrations(database.DB, dataDir, build.Version); err != nil {
			return nil, err
		}
	}
	err = db.RunMigrations(database.DB)
	if err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	// Metrics and uptime counts live in history.db next to it, without a
	// backup.
	history, err := db.OpenHistory(db.HistoryConnection(cfg.DBConnection))
	if err != nil {
		return nil, err
	}
	defer func() {
		if !initialized {
			_ = history.Close()
		}
	}()

	users := repository.NewUsers(database)
	tokens := repository.NewTokens(database)
	sessions := repository.NewSessions(database)
	files := repository.NewFiles(database)
	subscriptions := repository.NewSubscriptions(database)
	adminEvents := repository.NewAdmin(database)
	workspaces := repository.NewWorkspaces(database)
	members := repository.NewWorkspaceMembers(database)
	invitations := repository.NewWorkspaceInvitations(database)
	activity := repository.NewActivity(database)
	email := service.NewEmailService(mailer.NewProvider(cfg), cfg.AppURL, brand.Name)
	// A self-hosted hub may start without a bucket; files such as
	// avatars wait for one.
	var fileStorage storage.Storage = storage.None{}
	if cfg.S3Bucket != "" {
		fileStorage, err = storage.NewS3Storage(storage.S3Config{
			Region: cfg.S3Region, Bucket: cfg.S3Bucket,
			AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Endpoint: cfg.S3Endpoint,
			PresignExpiryPublic: cfg.S3PresignExpiryPublic, PresignExpiryPrivate: cfg.S3PresignExpiryPrivate,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize storage: %w", err)
		}
	}
	fileService := service.NewFileService(files, fileStorage)
	subscriptionService := service.NewSubscriptionService(subscriptions, adminEvents)
	subscriptionService.UseActivity(activity)
	workspaceService := service.NewWorkspaceService(
		workspaces, members, invitations, activity, users,
		subscriptionService, email,
	)
	var paymentProvider payment.Provider
	if cfg.Billing {
		paymentProvider = payment.NewStripeProvider(cfg, subscriptionService)
	} else {
		access.Product = access.SelfHosted
	}
	auth := service.NewAuthService(
		users, tokens, sessions, workspaceService, email,
		cfg.IsProduction(), cfg.SessionExpiry, cfg.TokenEmailVerifyExpiry,
		cfg.TokenPasswordResetExpiry, cfg.TokenEmailChangeExpiry, cfg.TokenMagicLinkExpiry,
	)
	beta := service.NewBetaService(cfg.SignupsOpen, cfg.AdminEmails, invitations, subscriptions)
	auth.SetBeta(beta)
	if !cfg.SignupsOpen && len(cfg.AdminEmails) == 0 {
		slog.Warn("sign-up is closed and ADMIN_EMAILS is empty: only invited people can create an account. Set ADMIN_EMAILS to your email and sign up with it to become the first admin")
	}
	userService := service.NewUserService(users,
		fileService,
		email, workspaceService,
	)
	apiTokenService := service.NewAPITokenService(repository.NewAPITokens(database), workspaceService)
	auth.UseAPITokens(apiTokenService)
	secrets, err := secret.New(cfg.SecretsKey)
	if err != nil {
		return nil, fmt.Errorf("initialize secrets: %w", err)
	}
	serverService := service.NewServerService(repository.NewServers(database), activity, cfg.AppURL)
	appService := service.NewAppService(repository.NewApps(database, history), serverService, secrets)
	serverService.RequirePlanFor(subscriptionService.Access)
	// The catalog ships in content/catalog; its releases come from the
	// registries, asked by this hub itself.
	texts := content.FS(cfg.IsDevelopment())
	uiFiles := moin.UI(cfg.IsDevelopment())
	entries, err := catalog.Load(texts)
	if err != nil {
		return nil, fmt.Errorf("load the catalog: %w", err)
	}
	catalogStore := catalog.NewStore(entries, catalog.NewRegistry())
	catalogCache := repository.NewCatalogCache(history)
	catalogStore.UseCache(catalog.Cache{Load: catalogCache.Load, Save: catalogCache.Save})
	catalogService := service.NewCatalogService(catalogStore, appService)
	alertService := service.NewAlertService(email, workspaces, members, repository.NewApps(database, history), repository.NewAlertDestinations(database), activity, secrets)
	// The hosted plane posts alerts only to public addresses.
	if cfg.Billing && !cfg.IsDevelopment() {
		alertService.AllowOnlyPublicTargets()
	}
	serverService.SendAlerts(alertService)
	// Daemons older than the hub update to its version by
	// themselves, unless their workspace turned it off.
	serverService.UpdateDaemonsTo(build.Version)
	github, err := service.NewGitHub(cfg.GitHubAppID, cfg.GitHubAppSlug, cfg.GitHubAppPrivateKey, cfg.GitHubAppWebhookSecret, cfg.GitHubAppClientID, cfg.GitHubAppClientSecret, cfg.GitHubAPIURL, cfg.GitHubURL)
	if err != nil {
		return nil, err
	}
	if github != nil && !github.ChecksInstaller() {
		slog.Warn("GITHUB_APP_CLIENT_ID and GITHUB_APP_CLIENT_SECRET are not set: moin cannot check who installed the GitHub App, so a member could connect another account's installation by its ID. Set both and turn on \"Request user authorization (OAuth) during installation\" in the app settings")
	}
	appService.SetGitHub(github)
	// The hosted plane reaches only public buckets; in development it may
	// use the local fake S3.
	if cfg.Billing && !cfg.IsDevelopment() {
		appService.AllowOnlyPublicBuckets()
	}
	appService.SetCloudflare(service.NewCloudflare(cfg.CloudflareAPIURL))
	if cfg.SameServer {
		appService.ProxyOnFirstConnect()
	}
	// Operators run moin as a service: only the hosted hub has
	// them. On a self-hosted one, ADMIN_EMAILS only says who may sign up.
	var operators []string
	if cfg.Billing {
		operators = cfg.AdminEmails
	}
	adminService := service.NewAdminService(adminEvents, users, auth, operators)
	oldDomain := service.NewOldDomainService(repository.NewOldDomainDays(history), cfg.OldURLs)
	adminService.UseOldDomain(oldDomain)
	if cfg.Billing {
		adminService.UsePlans(subscriptions, activity)
		offers := service.NewOfferService(repository.NewOffers(database), adminEvents)
		workspaceService.UseOffers(offers)
		adminService.UseOffers(offers)
	}
	application := &App{
		Cfg: cfg, DB: database, HistoryDB: history, AuthService: auth, UserService: userService,
		EmailService:        email,
		LegalService:        service.NewLegalService(texts, cfg.SupportEmail),
		DocsService:         service.NewDocsService(texts, cfg.AppURL, cfg.IsDevelopment()),
		UIDocsService:       service.NewUIDocsService(uiFiles, cfg.AppURL, cfg.IsDevelopment()),
		Texts:               texts,
		UIFiles:             uiFiles,
		FileService:         fileService,
		SubscriptionService: subscriptionService,
		AdminService:        adminService,
		OldDomainService:    oldDomain,
		BetaService:         beta,
		PaymentService:      paymentProvider,
		// Only a self-hosted hub reports; the hosted one counts.
		TelemetryService: service.NewTelemetryService(repository.NewTelemetry(database), cfg.TelemetryURL, build.Version, cfg.Telemetry && !cfg.Billing),
		// Only a self-hosted hub offers updates; the Deploy
		// workflow updates the hosted one.
		UpdateService:      service.NewUpdateService(build.Version, service.ReleasesURL, filepath.Join(dataDir, "update"), !cfg.Billing, cfg.UpdateCheck, cfg.AdminEmails, activity),
		WorkspaceService:   workspaceService,
		APITokenService:    apiTokenService,
		DeviceLoginService: service.NewDeviceLoginService(repository.NewDeviceLogins(database), apiTokenService),
		ServerService:      serverService,
		AppService:         appService,
		CatalogService:     catalogService,
		StatusService:      service.NewStatusService(appService, serverService, catalogService),
		AlertService:       alertService,
	}
	initialized = true
	if cfg.Billing {
		// Self-hosted hubs report to the hosted one, so only it
		// knows the whole network.
		adminService.UseNetwork(application.TelemetryService.Network)
	}
	return application, nil
}

func (a *App) Close() error {
	if a.HistoryDB != nil {
		_ = a.HistoryDB.Close()
	}
	if a.DB != nil {
		return a.DB.Close()
	}
	return nil
}
