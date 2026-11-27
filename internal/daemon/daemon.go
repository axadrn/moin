// Package daemon is moin on a server: `moin daemon`, which runs the timers,
// and what `moin here …` does there for the CLI on your laptop, over SSH.
// The daemon takes no commands and listens nowhere. It copies the
// databases into the bucket, checks every day that the newest copy
// restores, alerts and reports to the watchers.
package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// DefaultStateDir is where moin keeps its state on a server. MOIN_STATE_DIR
// overrides it, for example when a daemon runs without root in development.
const DefaultStateDir = "/var/lib/" + build.Name

// StateDir returns MOIN_STATE_DIR or DefaultStateDir.
func StateDir() string {
	if dir := build.Getenv("STATE_DIR"); dir != "" {
		return dir
	}
	return DefaultStateDir
}

// securityEvery is how often the daemon checks that the server still has
// the security `moin secure` set, and puts back what drifted.
const securityEvery = 5 * time.Minute

// Run runs the timers until ctx ends: backups and their daily check, the
// cleanup of old images, the security of the server, and every minute a
// report to the watchers and the alerts.
func Run(ctx context.Context, dir string) error {
	if _, err := LoadConfig(); err != nil {
		return err
	}
	a := newApps(dir)
	// What a command that stopped left behind ends first: a restore's
	// switch is one step, so the database is whole.
	a.backups.recover(ctx)
	a.backups.sweepTemp()
	a.recoverDeploys(ctx)
	go a.backups.loop(ctx)
	go a.cleanups(ctx)
	go keepSecurity(ctx, newSecurity(dir, a))
	newWatch(a).loop(ctx)
	return nil
}

// keepSecurity checks the server's security now and every securityEvery,
// and puts back what drifted.
func keepSecurity(ctx context.Context, s *security) {
	ticker := time.NewTicker(securityEvery)
	defer ticker.Stop()
	for {
		result := s.keep(ctx)
		for _, fixed := range result.Fixed {
			slog.Warn("security: put back", "what", fixed)
		}
		for _, problem := range result.Problems {
			slog.Error("security", "problem", problem)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
