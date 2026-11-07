package service

import (
	"cmp"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// finishUptime keeps a result, counts it, and when the domain went down or
// came back, stores that and writes it to the activity.
func (s *AppService) finishUptime(job uptimeJob, result uptimeResult) {
	u := s.uptime
	u.mu.Lock()
	state := u.states[job.key]
	if state == nil || state.stored.Paused {
		if state != nil {
			state.inflight = false
		}
		u.mu.Unlock()
		return
	}
	state.inflight = false
	state.last = &result
	u.count(job.key, job.started.UTC().Truncate(time.Hour), result)
	before := state.stored
	switch {
	case result.up && state.stored.State != model.UptimeUp:
		at := job.started.UTC()
		state.stored.State, state.stored.Since = model.UptimeUp, &at
	case !result.up && state.stored.State != model.UptimeDown:
		at := job.started.UTC()
		state.stored.State, state.stored.Since = model.UptimeDown, &at
	}
	stored, workspaceID, appName := state.stored, state.workspaceID, state.appName
	u.mu.Unlock()
	if stored.State == before.State {
		return
	}
	err := s.apps.SaveUptimeState(stored)
	if err != nil {
		slog.Error("store uptime state failed", "error", err)
	}
	s.recordUptimeChange(workspaceID, appName, before, stored, result.failure)
}

// count adds a result to the counts of its domain and hour. The caller
// holds u.mu.
func (u *uptimeRunner) count(key uptimeKey, hour time.Time, result uptimeResult) {
	if u.hours[key] == nil {
		u.hours[key] = map[time.Time]*model.UptimeCounts{}
	}
	if u.hours[key][hour] == nil {
		u.hours[key][hour] = &model.UptimeCounts{}
	}
	u.hours[key][hour].Add(result.up, result.responseMS)
}

// flushUptime writes the counts in memory to the database in one go.
func (s *AppService) flushUptime() {
	u := s.uptime
	u.mu.Lock()
	pending := u.hours
	u.hours = map[uptimeKey]map[time.Time]*model.UptimeCounts{}
	u.mu.Unlock()
	var hours []model.UptimeHour
	for key, byHour := range pending {
		for hour, counts := range byHour {
			hours = append(hours, model.UptimeHour{AppID: key.app, Domain: key.domain, Hour: hour, UptimeCounts: *counts})
		}
	}
	err := s.apps.AddUptimeHours(hours)
	if err != nil {
		slog.Error("store uptime counts failed", "error", err, "rows", len(hours))
	}
}

// UptimeDomain is the uptime of one domain of an app.
type UptimeDomain struct {
	Domain string
	// Path is where the visit goes; CustomPath says it was set, not the
	// health path or /.
	Path       string
	CustomPath bool
	Paused     bool
	// State is up, down, pending or paused, Since when it began.
	State string
	Since *time.Time
	// The last visit, nil before the first since the hub
	// started.
	VisitedAt     *time.Time
	StatusCode    int
	ResponseMS    int
	Error         string
	CertExpiresAt *time.Time
	// Uptime in percent over the last 24 hours, 7 and 30 days; nil
	// without a visit in that time.
	Uptime24h, Uptime7d, Uptime30d *float64
	// AverageMS is the mean response time of the last 24 hours.
	AverageMS int
	Outages   []model.UptimeOutage
}

// AppUptime is the uptime of an app: one state for all its domains, its
// server, and each domain.
type AppUptime struct {
	State        string
	Server       string
	ServerOnline bool
	Domains      []UptimeDomain
}

// Uptime returns the uptime of every domain of an app.
func (s *AppService) Uptime(workspaceID, idOrName string) (*model.App, AppUptime, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, AppUptime{}, err
	}
	domains, health := s.appUptimeDomains(app)
	now := s.now()
	uptime := AppUptime{Server: app.ServerName, ServerOnline: s.servers.Online(app.ServerID), Domains: []UptimeDomain{}}
	for _, domain := range domains {
		view, err := s.uptimeDomain(app, domain, health, now)
		if err != nil {
			return nil, AppUptime{}, err
		}
		uptime.Domains = append(uptime.Domains, view)
	}
	uptime.State = appUptimeState(app, uptime.Domains)
	return app, uptime, nil
}

// appUptimeState sums up the domains: down when one is down, up when the
// others are up.
func appUptimeState(app *model.App, domains []UptimeDomain) string {
	switch {
	case app.Stopped:
		return model.UptimeStopped
	case len(domains) == 0:
		return model.UptimeNoDomains
	}
	states := map[string]bool{}
	for _, domain := range domains {
		states[domain.State] = true
	}
	switch {
	case states[model.UptimeDown]:
		return model.UptimeDown
	case states[model.UptimePending]:
		return model.UptimePending
	case states[model.UptimeUp]:
		return model.UptimeUp
	}
	return model.UptimePaused
}

// appUptimeDomains are the domains of an app and its health path.
func (s *AppService) appUptimeDomains(app *model.App) ([]string, string) {
	detected := ""
	if app.LiveNumber > 0 {
		if deploy, err := s.apps.Deploy(app.ID, app.LiveNumber); err == nil {
			detected = deploy.Detected
		}
	}
	return uptimeDomains(app.Domains, detected)
}

// uptimeDomain is one domain with its newest result from memory and its
// counts from the database and memory.
func (s *AppService) uptimeDomain(app *model.App, domain, health string, now time.Time) (UptimeDomain, error) {
	u := s.uptime
	u.load(s.apps.DomainUptimes)
	key := uptimeKey{app.ID, domain}
	view := UptimeDomain{Domain: domain, Path: health, State: model.UptimePending}
	pending := map[time.Time]model.UptimeCounts{}
	u.mu.Lock()
	if state := u.states[key]; state != nil {
		view.Paused, view.CustomPath = state.stored.Paused, state.stored.Path != ""
		view.Path = cmp.Or(state.stored.Path, health)
		if state.stored.State != "" {
			view.State, view.Since = state.stored.State, state.stored.Since
		}
		if last := state.last; last != nil {
			at := last.at.UTC()
			view.VisitedAt, view.StatusCode, view.ResponseMS, view.Error, view.CertExpiresAt = &at, last.statusCode, last.responseMS, last.failure, last.certExpiry
		}
	}
	for hour, counts := range u.hours[key] {
		pending[hour] = *counts
	}
	u.mu.Unlock()
	if view.Paused || app.Stopped || app.LiveNumber == 0 {
		view.State, view.Since = model.UptimePaused, nil
		if app.LiveNumber == 0 && !app.Stopped && !view.Paused {
			view.State = model.UptimePending
		}
	}
	err := s.uptimePercents(&view, app.ID, domain, pending, now)
	if err != nil {
		return UptimeDomain{}, err
	}
	outages, err := s.apps.UptimeOutages(app.ID, domain, uptimeOutagesShown)
	if err != nil {
		return UptimeDomain{}, err
	}
	view.Outages = outages
	return view, nil
}

// uptimePercents fills in the uptime over the last 24 hours, 7 and 30
// days, and the mean response time of the last 24 hours, from the counts
// in the database and the pending ones in memory.
func (s *AppService) uptimePercents(view *UptimeDomain, appID, domain string, pending map[time.Time]model.UptimeCounts, now time.Time) error {
	for _, window := range []struct {
		span time.Duration
		into **float64
	}{{24 * time.Hour, &view.Uptime24h}, {7 * 24 * time.Hour, &view.Uptime7d}, {30 * 24 * time.Hour, &view.Uptime30d}} {
		since := now.Add(-window.span).UTC().Truncate(time.Hour)
		counts, err := s.apps.UptimeCounts(appID, domain, since)
		if err != nil {
			return err
		}
		for hour, more := range pending {
			if !hour.Before(since) {
				counts.Merge(more)
			}
		}
		if counts.Visits > 0 {
			percent := math.Round(float64(counts.Up)/float64(counts.Visits)*100000) / 1000
			*window.into = &percent
			if window.span == 24*time.Hour {
				view.AverageMS = counts.ResponseMS / counts.Visits
			}
		}
	}
	return nil
}

// SetUptime changes where uptime of a domain goes, nil path keeps
// it and "" goes back to the health path or /, and pauses or resumes it,
// nil keeps it. A paused visit starts over when it resumes.
func (s *AppService) SetUptime(actor Actor, idOrName, domain string, path *string, paused *bool) (*model.App, UptimeDomain, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, UptimeDomain{}, err
	}
	domains, health := s.appUptimeDomains(app)
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if !slices.Contains(domains, domain) {
		return nil, UptimeDomain{}, ErrUptimeDomain
	}
	if path != nil {
		clean := strings.TrimSpace(*path)
		if clean != "" && !uptimePath.MatchString(clean) {
			return nil, UptimeDomain{}, ErrUptimePath
		}
		path = &clean
	}
	u := s.uptime
	u.load(s.apps.DomainUptimes)
	now := s.now().UTC()
	key := uptimeKey{app.ID, domain}
	u.mu.Lock()
	state := u.states[key]
	if state == nil {
		state = &uptimeState{stored: model.DomainUptime{AppID: app.ID, Domain: domain}, offset: uptimeOffset(key)}
		u.states[key] = state
	}
	before := state.stored
	if path != nil {
		state.stored.Path = *path
		state.path = cmp.Or(*path, health)
	}
	if paused != nil {
		state.stored.Paused = *paused
		if *paused {
			state.stored.State, state.stored.Since, state.last = "", nil, nil
		}
	}
	stored := state.stored
	u.mu.Unlock()
	if stored != before {
		err = s.apps.SaveDomainUptime(stored)
		if err != nil {
			// What is not stored does not count: the page and the visits
			// keep what the database has.
			u.mu.Lock()
			state.stored.Path, state.stored.Paused = before.Path, before.Paused
			state.path = cmp.Or(before.Path, health)
			u.mu.Unlock()
			return nil, UptimeDomain{}, err
		}
	}
	if before.State == model.UptimeDown && stored.State == "" {
		_ = s.apps.EndUptimeOutage(app.ID, domain, now)
	}
	if stored.Path != before.Path {
		s.servers.record(actor, "uptime.path_set", "app", app.Name, map[string]string{"domain": domain, "path": stored.Path, "from": before.Path})
	}
	if stored.Paused != before.Paused {
		action := "uptime.resumed"
		if stored.Paused {
			action = "uptime.paused"
		}
		s.servers.record(actor, action, "app", app.Name, map[string]string{"domain": domain})
	}
	view, err := s.uptimeDomain(app, domain, health, now)
	return app, view, err
}

// DownDomain is a domain of an app that the visits find down.
type DownDomain struct {
	// App is the app's name, Link how a link names it.
	App    string
	Link   string
	Domain string
	Since  *time.Time
}

// DownDomains lists the domains of apps that are down now, unpaused, from
// uptime's state; the overview shows them.
func (s *AppService) DownDomains(apps []model.App) []DownDomain {
	u := s.uptime
	u.load(s.apps.DomainUptimes)
	names := make(map[string]model.App, len(apps))
	for _, app := range apps {
		names[app.ID] = app
	}
	var down []DownDomain
	u.mu.Lock()
	defer u.mu.Unlock()
	for key, state := range u.states {
		app, ok := names[key.app]
		if !ok || state.stored.Paused || state.stored.State != model.UptimeDown {
			continue
		}
		down = append(down, DownDomain{App: app.Name, Link: app.Link(), Domain: key.domain, Since: state.stored.Since})
	}
	sort.Slice(down, func(i, j int) bool { return down[i].Domain < down[j].Domain })
	return down
}
