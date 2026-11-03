package app

import (
	"fmt"
	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/internal/build"
	"log/slog"

	"github.com/axadrn/ops/controlplane/internal/brand"
	"github.com/axadrn/ops/controlplane/internal/config"
	"github.com/axadrn/ops/controlplane/internal/content"
	"github.com/axadrn/ops/controlplane/internal/db"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/secret"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/service/mailer"
	"github.com/axadrn/ops/controlplane/internal/service/payment"
	"github.com/axadrn/ops/controlplane/internal/storage"
	"github.com/jmoiron/sqlx"
)

type App struct {
	Cfg                 *config.Config
	DB                  *sqlx.DB
	AuthService         *service.AuthService
	UserService         *service.UserService
	EmailService        *service.EmailService
	LegalService        *service.LegalService
	DocsService         *service.DocsService
	FileService         *service.FileService
	SubscriptionService *service.SubscriptionService
	AdminService        *service.AdminService
	BetaService         *service.BetaService
	PaymentService      payment.Provider
	TelemetryService    *service.TelemetryService
	WorkspaceService    *service.WorkspaceService
	APITokenService     *service.APITokenService
	ServerService       *service.ServerService
	AppService          *service.AppService
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
	err = db.RunMigrations(database.DB)
	if err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	users := repository.NewUserRepository(database)
	tokens := repository.NewTokenRepository(database)
	sessions := repository.NewSessionRepository(database)
	files := repository.NewFileRepository(database)
	subscriptions := repository.NewSubscriptionRepository(database)
	adminEvents := repository.NewAdminRepository(database)
	workspaces := repository.NewWorkspaceRepository(database)
	members := repository.NewWorkspaceMemberRepository(database)
	invitations := repository.NewWorkspaceInvitationRepository(database)
	workspaceAuditEvents := repository.NewWorkspaceAuditRepository(database)
	mailProvider, err := mailer.NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize mail provider: %w", err)
	}
	email := service.NewEmailService(mailProvider, cfg.AppURL, brand.Name)
	fileStorage, err := storage.NewS3Storage(storage.S3Config{
		Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Endpoint: cfg.S3Endpoint,
		PresignExpiryPublic: cfg.S3PresignExpiryPublic, PresignExpiryPrivate: cfg.S3PresignExpiryPrivate,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize storage: %w", err)
	}
	fileService := service.NewFileService(files, fileStorage)
	subscriptionService := service.NewSubscriptionService(subscriptions, adminEvents)
	subscriptionService.UseActivity(workspaceAuditEvents)
	workspaceService := service.NewWorkspaceService(
		workspaces, members, invitations, workspaceAuditEvents, users,
		subscriptionService, email,
	)
	var paymentProvider payment.Provider
	if cfg.Billing {
		paymentProvider, err = payment.NewProvider(cfg, subscriptionService)
		if err != nil {
			return nil, fmt.Errorf("initialize payment provider: %w", err)
		}
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
	apiTokenService := service.NewAPITokenService(repository.NewAPITokenRepository(database), workspaceService)
	auth.UseAPITokens(apiTokenService)
	secrets, err := secret.New(cfg.SecretsKey)
	if err != nil {
		return nil, fmt.Errorf("initialize secrets: %w", err)
	}
	serverService := service.NewServerService(repository.NewServerRepository(database), workspaceAuditEvents, cfg.AppURL)
	appService := service.NewAppService(repository.NewAppRepository(database), serverService, secrets)
	serverService.RequirePlanFor(subscriptionService.Access)
	github, err := service.NewGitHub(cfg.GitHubAppID, cfg.GitHubAppSlug, cfg.GitHubAppPrivateKey, cfg.GitHubAppWebhookSecret, cfg.GitHubAppClientID, cfg.GitHubAppClientSecret, cfg.GitHubAPIURL, cfg.GitHubURL)
	if err != nil {
		return nil, err
	}
	if github != nil && !github.ChecksInstaller() {
		slog.Warn("GITHUB_APP_CLIENT_ID and GITHUB_APP_CLIENT_SECRET are not set: ops cannot check who installed the GitHub App, so a member could connect another account's installation by its ID. Set both and turn on \"Request user authorization (OAuth) during installation\" in the app settings")
	}
	appService.SetGitHub(github)
	// The hosted plane reaches only public buckets; in development it may
	// use the local fake S3.
	if cfg.Billing && !cfg.IsDevelopment() {
		appService.AllowOnlyPublicBuckets()
	}
	appService.SetCloudflare(service.NewCloudflare(cfg.CloudflareAPIURL))
	// Operators run ops as a service: only the hosted control plane has
	// them. On a self-hosted one, ADMIN_EMAILS only says who may sign up.
	var operators []string
	if cfg.Billing {
		operators = cfg.AdminEmails
	}
	adminService := service.NewAdminService(adminEvents, users, auth, operators)
	if cfg.Billing {
		adminService.UsePlans(subscriptions, workspaceAuditEvents)
	}
	application := &App{
		Cfg: cfg, DB: database, AuthService: auth, UserService: userService,
		EmailService:        email,
		LegalService:        service.NewLegalService(content.Root, cfg.SupportEmail),
		DocsService:         service.NewDocsService(content.Root, cfg.AppURL),
		FileService:         fileService,
		SubscriptionService: subscriptionService,
		AdminService:        adminService,
		BetaService:         beta,
		PaymentService:      paymentProvider,
		// Only a self-hosted control plane reports; the hosted one counts.
		TelemetryService: service.NewTelemetryService(repository.NewTelemetryRepository(database), cfg.TelemetryURL, build.Version, cfg.Telemetry && !cfg.Billing),
		WorkspaceService: workspaceService,
		APITokenService:  apiTokenService,
		ServerService:    serverService,
		AppService:       appService,
	}
	initialized = true
	return application, nil
}

func (a *App) Close() error {
	if a.DB != nil {
		return a.DB.Close()
	}
	return nil
}
