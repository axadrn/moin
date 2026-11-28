// The site at moin.run: the landing page, the docs, the blog, the
// changelog, the catalog and the legal pages, and moin cloud.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/copies"
	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/handler"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/routes"
	"github.com/axadrn/moin/cloud/internal/stripe"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "preflight" {
		if err := preflight(); err != nil {
			slog.Error("preflight failed, the running site stays", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("site stopped", "error", err)
		os.Exit(1)
	}
}

// preflight checks the settings of moin.run before update.sh switches to
// this version: all of them read well, and none moin.run needs is empty.
func preflight() error {
	if _, err := config.Load(); err != nil {
		return err
	}
	if missing := config.Missing(); len(missing) > 0 {
		return fmt.Errorf("moin.run needs these settings in .env: %s", strings.Join(missing, ", "))
	}
	slog.Info("preflight passed")
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer database.Close()
	store := workspace.NewStore(database)
	mail := mailer.New(cfg.CloudflareAccountID, cfg.CloudflareAPIToken, cfg.EmailFrom, cfg.CloudflareAPIURL)
	if _, logs := mail.(mailer.Log); logs && !cfg.Development {
		slog.Warn("no CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN: mails go to the log, not to people")
	}
	watching := watchers.New(store, counts.New(database), mail, cfg.AppURL, cfg.SecretsKey)
	if err := watching.Load(); err != nil {
		return err
	}
	signIn := github.New(cfg.GitHubClientID, cfg.GitHubClientSecret, cfg.GitHubURL, cfg.GitHubAPIURL)
	cloud := handler.NewCloud(store, watching, mail, signIn, cfg.AppURL, !cfg.Development)
	if cfg.StripeSecretKey != "" {
		cloud.Stripe = stripe.New(stripe.Settings{
			SecretKey: cfg.StripeSecretKey, WebhookSecret: cfg.StripeWebhookSecret, API: cfg.StripeAPIURL,
			Product: cfg.StripeProduct, Monthly: cfg.StripePriceMonthly, Yearly: cfg.StripePriceYearly,
		})
	} else if !cfg.Development {
		slog.Warn("no STRIPE_SECRET_KEY: nobody can subscribe")
	}
	if cfg.CopiesBucket != "" {
		bucket := copies.NewBucket(cfg.CopiesEndpoint, cfg.CopiesBucket, cfg.CopiesAccessKey, cfg.CopiesSecretKey)
		cloud.Copies = copies.New(store, bucket, mail, cfg.AppURL)
	} else if !cfg.Development {
		slog.Warn("no COPIES_BUCKET: no server can send a copy")
	}
	site, err := routes.New(cfg, cloud)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: ":" + cfg.Port, Handler: site,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	slog.Info("site starting", "port", cfg.Port, "development", cfg.Development, "url", cfg.AppURL)

	// It runs until SIGTERM or ctrl+c, then lets open requests finish.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	watched := make(chan struct{})
	go func() { watching.Run(ctx); close(watched) }()
	go cleanUp(ctx, store)
	if cloud.Copies != nil {
		go cloud.Copies.Run(ctx)
	}
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	// The watchers save what they know once more before the database closes.
	<-watched
	slog.Info("site stopped")
	return nil
}

// cleanUp deletes the logins and sessions that ran out, every hour.
func cleanUp(ctx context.Context, store *workspace.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if err := store.DeleteOld(); err != nil {
			slog.Error("delete old logins and sessions", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
