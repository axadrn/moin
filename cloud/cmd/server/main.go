// The site at moin.run: the landing page, the docs, the blog, the
// changelog, the catalog and the legal pages.
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
	"github.com/axadrn/moin/cloud/internal/routes"
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
	site, err := routes.New(cfg)
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
	slog.Info("site stopped")
	return nil
}
