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
	"syscall"
	"time"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/handler"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/routes"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

func main() {
	if err := run(); err != nil {
		slog.Error("site stopped", "error", err)
		os.Exit(1)
	}
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
	watching := watchers.New(store, mail, cfg.AppURL)
	if err := watching.Load(); err != nil {
		return err
	}
	signIn := github.New(cfg.GitHubClientID, cfg.GitHubClientSecret, cfg.GitHubURL, cfg.GitHubAPIURL)
	cloud := handler.NewCloud(store, watching, mail, signIn, cfg.AppURL, !cfg.Development)
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
