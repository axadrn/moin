package service

import (
	"cmp"
	"context"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// RunUptime keeps a watcher on every domain uptime visits until ctx ends,
// then writes the counts it still holds. Once a minute it starts the
// watchers of new domains, stops the ones of domains that went, and
// watches the servers. Wiring starts it once, in a goroutine.
func (s *AppService) RunUptime(ctx context.Context) {
	u := s.uptime
	u.started = s.now()
	if u.client == nil {
		u.client = newUptimeClient(s.publicOnly)
	}
	ticker := time.NewTicker(u.every)
	defer ticker.Stop()
	flushed := s.now()
	for {
		now := s.now()
		s.watchServers(now)
		s.pruneUptime(now)
		s.syncWatchers(ctx, now)
		if now.Sub(flushed) >= uptimeFlushEvery {
			s.flushUptime()
			flushed = now
		}
		select {
		case <-ctx.Done():
			u.watchers.Wait()
			s.flushUptime()
			return
		case <-ticker.C:
		}
	}
}

// syncWatchers starts a watcher for every domain to watch that has none,
// and stops the watcher of a domain that is paused.
func (s *AppService) syncWatchers(ctx context.Context, now time.Time) {
	watch, ok := s.syncDomains(now)
	if !ok {
		return
	}
	u := s.uptime
	u.mu.Lock()
	defer u.mu.Unlock()
	for key, state := range u.states {
		switch {
		case watch[key] && state.stop == nil:
			s.startWatcher(ctx, key, state)
		case !watch[key]:
			state.stopWatcher()
		}
	}
}

// startWatcher starts the watcher of one domain. The caller holds u.mu.
func (s *AppService) startWatcher(ctx context.Context, key uptimeKey, state *uptimeState) {
	watcher, stop := context.WithCancel(ctx)
	state.stop = stop
	s.uptime.watchers.Go(func() { s.watch(watcher, key) })
}

// watch is the watcher of one domain: it visits the domain at its moment
// of every minute until ctx ends. A slow visit delays only this domain.
func (s *AppService) watch(ctx context.Context, key uptimeKey) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.uptime.untilVisit(key, s.now())):
		}
		s.visitDomain(ctx, key)
	}
}

// visitDomain visits a domain once and keeps what it found, unless the
// domain is paused or gone.
func (s *AppService) visitDomain(ctx context.Context, key uptimeKey) {
	path, ok := s.uptime.pathOf(key)
	if !ok {
		return
	}
	at := s.now().UTC()
	result := s.uptime.visit(ctx, key.domain, path)
	if ctx.Err() != nil {
		return
	}
	result.at = at
	s.keepVisit(key, result)
}

// pathOf is where the next visit of a domain goes; false when the domain
// is paused or gone.
func (u *uptimeRunner) pathOf(key uptimeKey) (string, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	state := u.states[key]
	if state == nil || state.stored.Paused {
		return "", false
	}
	return state.path, true
}

// syncDomains brings the domains in memory in line with the live apps: it
// adds the domains uptime visits now, forgets the ones a live app no
// longer has, and lets the ones of a stopped or removed app start over.
// It returns the domains to watch, the ones not paused, and false when the
// apps could not be read.
func (s *AppService) syncDomains(now time.Time) (map[uptimeKey]bool, bool) {
	u := s.uptime
	if err := u.load(s.apps.DomainUptimes); err != nil {
		slog.Error("load uptime failed", "error", err)
		return nil, false
	}
	apps, err := s.apps.LiveApps()
	if err != nil {
		slog.Error("list apps for uptime failed", "error", err)
		return nil, false
	}
	live := map[string]bool{}
	current := map[uptimeKey]bool{}
	watch := map[uptimeKey]bool{}
	for _, app := range apps {
		live[app.ID] = true
		for _, domain := range s.uptimeDomains(&app) {
			key := uptimeKey{app.ID, domain}
			current[key] = true
			watch[key] = !u.track(key, app)
		}
	}
	var gone, reset []model.DomainUptime
	u.mu.Lock()
	for key, state := range u.states {
		if current[key] {
			continue
		}
		state.stopWatcher()
		switch {
		case live[key.app]:
			// A live app no longer has the domain: its uptime goes.
			gone = append(gone, state.stored)
			delete(u.states, key)
			continue
		case state.stored.State != "":
			// A stopped or removed app is not visited; its uptime starts
			// over when it is live again.
			state.startOver()
			reset = append(reset, state.stored)
		}
		if state.stored.Path == "" && !state.stored.Paused {
			delete(u.states, key)
		}
	}
	u.mu.Unlock()
	s.storeSyncedUptime(gone, reset, now)
	return watch, true
}

// track keeps a domain in memory with its app and where its visit goes,
// and says whether the domain is paused.
func (u *uptimeRunner) track(key uptimeKey, app model.App) (paused bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	state := u.states[key]
	if state == nil {
		state = &uptimeState{stored: model.DomainUptime{AppID: key.app, Domain: key.domain}}
		u.states[key] = state
	}
	state.workspaceID, state.appName = app.WorkspaceID, app.Name
	state.path = cmp.Or(state.stored.Path, defaultUptimePath)
	return state.stored.Paused
}

// storeSyncedUptime writes what syncDomains changed: the uptime of a
// domain no longer visited is deleted, the one of an app no longer live
// starts over. Both end an open outage.
func (s *AppService) storeSyncedUptime(gone, reset []model.DomainUptime, now time.Time) {
	for _, stored := range gone {
		_ = s.apps.EndUptimeOutage(stored.AppID, stored.Domain, now)
		if err := s.apps.DeleteDomainUptime(stored.AppID, stored.Domain); err != nil {
			slog.Error("delete uptime failed", "error", err)
		}
	}
	for _, stored := range reset {
		_ = s.apps.EndUptimeOutage(stored.AppID, stored.Domain, now)
		if err := s.apps.SaveUptimeState(stored); err != nil {
			slog.Error("store uptime failed", "error", err)
		}
	}
}

// pruneUptime drops counts and outages older than they are kept, once an
// hour.
func (s *AppService) pruneUptime(now time.Time) {
	u := s.uptime
	if now.Sub(u.pruned) < time.Hour {
		return
	}
	err := s.apps.PruneUptime(now.Add(-uptimeHoursKept), now.Add(-uptimeOutagesKept))
	if err != nil {
		slog.Error("prune uptime failed", "error", err)
	}
	u.pruned = now
}

// uptimeDomains are the domains of an app uptime visits: every domain
// that serves something, so not the main service's domains besides its
// canonical one, which only redirect to it. The other services' domains
// follow, by service.
func (s *AppService) uptimeDomains(app *model.App) []string {
	domains, canonical := app.DomainsOf("")
	if canonical != "" {
		domains = []string{canonical}
	}
	services := s.otherServiceDomains(app)
	for _, service := range slices.Sorted(maps.Keys(services)) {
		domains = append(domains, services[service]...)
	}
	return domains
}

// defaultUptimePath is where a visit goes unless a path is set.
const defaultUptimePath = "/"
