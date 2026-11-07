package service

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"maps"
	"math"
	"net"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/internal/build"
)

// Uptime checks. Every domain of every live app gets one GET a minute, like
// a visitor, at its own second of the minute, so the checks spread evenly
// instead of starting all at once. A failed check is tried again a few
// seconds later; only when that fails too is the domain down. The newest
// results live in memory. The database gets a write when a state or a
// setting changes, and the counts per domain and hour in one transaction
// every few minutes.
const (
	// uptimeTimeout is how long a visitor waits for the answer.
	uptimeTimeout    = 10 * time.Second
	uptimeRetryAfter = 5 * time.Second
	// uptimeWorkers bounds the checks that run at once; uptimeQueue the
	// ones that wait. A check that finds no room is skipped this minute,
	// and counts neither as up nor as down.
	uptimeWorkers = 512
	uptimeQueue   = 8192
	// uptimeFlushEvery is how often the counts go to the database.
	uptimeFlushEvery = 5 * time.Minute
	uptimeBodyLimit  = 4 << 10
	// uptimeHoursKept covers the 30 days uptime shows; outages stay longer.
	uptimeHoursKept   = 31 * 24 * time.Hour
	uptimeOutagesKept = 90 * 24 * time.Hour
	// uptimeOutagesShown is how many outages a domain lists.
	uptimeOutagesShown = 10
	// serverDownAfter is how long an agent may be gone before its server
	// counts as down: a restart or a short network hiccup is not.
	serverDownAfter = 2 * time.Minute
)

var (
	ErrUptimeDomain = errors.New("the app has no such domain")
	ErrUptimePath   = errors.New("the path must start with / and have no spaces")
	uptimePath      = regexp.MustCompile(`^/[^\s#]{0,255}$`)
)

// uptimeKey names the check of one domain of an app.
type uptimeKey struct{ app, domain string }

// uptimeState is one domain in memory: what is stored, where its next
// check goes, and the newest result, which is only kept here.
type uptimeState struct {
	check       model.UptimeCheck
	workspaceID string
	appName     string
	// path is where the next check goes: the set path, the health path or /.
	path string
	// offset is the second of the minute its check runs at.
	offset   int
	inflight bool
	last     *uptimeResult
}

// uptimeResult is what one visit found.
type uptimeResult struct {
	at         time.Time
	up         bool
	statusCode int
	responseMS int
	failure    string
	certExpiry *time.Time
}

// uptimeJob is one check to run: its second try has attempt 1, and started
// is when the first try ran.
type uptimeJob struct {
	key     uptimeKey
	path    string
	started time.Time
	attempt int
}

// uptimeChecker holds what the checks keep in memory.
type uptimeChecker struct {
	mu     sync.Mutex
	loaded bool
	states map[uptimeKey]*uptimeState
	// hours are the counts not in the database yet, by domain and hour.
	hours map[uptimeKey]map[time.Time]*model.UptimeCounts

	client *http.Client
	// url is where a check of a domain and path goes; tests point it at
	// an httptest server.
	url        func(domain, path string) string
	retryAfter time.Duration
	// started is when the checker started: an agent counts as gone only
	// from then on, since none could connect while the control plane was
	// down itself.
	started time.Time
	pruned  time.Time
	skipped int
}

func newUptimeChecker() *uptimeChecker {
	return &uptimeChecker{
		states:     map[uptimeKey]*uptimeState{},
		hours:      map[uptimeKey]map[time.Time]*model.UptimeCounts{},
		url:        func(domain, path string) string { return "https://" + domain + path },
		retryAfter: uptimeRetryAfter,
	}
}

// newUptimeClient is one client for every check: a shared transport that
// keeps a bounded number of connections open, no redirects followed (a
// redirect is an answer), and on the hosted control plane only public
// addresses.
func newUptimeClient(publicOnly bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if publicOnly {
		dialer.Control = refusePrivate
	}
	return &http.Client{
		Timeout: uptimeTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: uptimeTimeout,
			MaxIdleConns:          1024,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// RunUptime checks every domain once a minute until ctx ends, then writes
// the counts it still holds. Wiring starts it once, in a goroutine.
func (s *AppService) RunUptime(ctx context.Context) {
	u := s.uptime
	u.started = s.now()
	if u.client == nil {
		u.client = newUptimeClient(s.publicBucketsOnly)
	}
	jobs := make(chan uptimeJob, uptimeQueue)
	var workers sync.WaitGroup
	for range uptimeWorkers {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-jobs:
					s.runUptimeJob(ctx, job, jobs)
				}
			}
		})
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var plan [60][]uptimeKey
	// next is the second of the minute to start the checks of; it starts
	// at the current one, so the first minute is no burst either.
	next := -1
	flushed := s.now()
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			s.flushUptime()
			return
		case <-ticker.C:
		}
		now := s.now()
		second := now.Second()
		if next < 0 {
			plan, next = s.planUptime(now), second
		}
		// Seconds the ticker skipped run late rather than not at all.
		for next != (second+1)%60 {
			if next == 0 {
				plan = s.planUptime(now)
			}
			for _, key := range plan[next] {
				job, ok := u.begin(key, now)
				if !ok {
					continue
				}
				select {
				case jobs <- job:
				default:
					u.end(key, true)
				}
			}
			next = (next + 1) % 60
		}
		if now.Sub(flushed) >= uptimeFlushEvery {
			s.flushUptime()
			flushed = now
		}
	}
}

// runUptimeJob visits a domain. A failed first try goes back in the queue
// a few seconds later, so one lost packet is no outage.
func (s *AppService) runUptimeJob(ctx context.Context, job uptimeJob, jobs chan<- uptimeJob) {
	result := s.uptime.visit(ctx, job.key.domain, job.path)
	if ctx.Err() != nil {
		return
	}
	if !result.up && job.attempt == 0 {
		job.attempt = 1
		time.AfterFunc(s.uptime.retryAfter, func() {
			select {
			case jobs <- job:
			case <-ctx.Done():
			}
		})
		return
	}
	s.finishUptime(job, result)
}

// CheckUptime checks every domain once, now, with the retry, and writes
// the counts. Tests call it; RunUptime spreads the same over the minute.
func (s *AppService) CheckUptime(ctx context.Context) {
	u := s.uptime
	if u.client == nil {
		u.client = newUptimeClient(s.publicBucketsOnly)
	}
	now := s.now()
	plan := s.planUptime(now)
	limit := make(chan struct{}, uptimeWorkers)
	var wait sync.WaitGroup
	for _, keys := range plan {
		for _, key := range keys {
			job, ok := u.begin(key, now)
			if !ok {
				continue
			}
			wait.Go(func() {
				limit <- struct{}{}
				defer func() { <-limit }()
				result := u.visit(ctx, key.domain, job.path)
				if !result.up {
					select {
					case <-time.After(u.retryAfter):
					case <-ctx.Done():
					}
					result = u.visit(ctx, key.domain, job.path)
				}
				if ctx.Err() != nil {
					u.end(key, false)
					return
				}
				s.finishUptime(job, result)
			})
		}
	}
	wait.Wait()
	s.flushUptime()
}

// load reads the stored checks into memory, once.
func (u *uptimeChecker) load(read func() ([]model.UptimeCheck, error)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.loaded {
		return
	}
	checks, err := read()
	if err != nil {
		slog.Error("load uptime checks failed", "error", err)
		return
	}
	for _, check := range checks {
		key := uptimeKey{check.AppID, check.Domain}
		u.states[key] = &uptimeState{check: check, offset: uptimeOffset(key)}
	}
	u.loaded = true
}

// begin marks a check as running and returns its job; false when it is
// paused, gone or still running from the minute before.
func (u *uptimeChecker) begin(key uptimeKey, now time.Time) (uptimeJob, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	state := u.states[key]
	if state == nil || state.check.Paused || state.inflight {
		return uptimeJob{}, false
	}
	state.inflight = true
	return uptimeJob{key: key, path: state.path, started: now}, true
}

// end marks a check as done without a result.
func (u *uptimeChecker) end(key uptimeKey, skipped bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if state := u.states[key]; state != nil {
		state.inflight = false
	}
	if skipped {
		u.skipped++
		if u.skipped%1000 == 1 {
			slog.Warn("uptime checks skipped: more checks than workers", "skipped", u.skipped)
		}
	}
}

// uptimeOffset is the second of the minute a domain is checked at, the same
// on every start.
func uptimeOffset(key uptimeKey) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key.app + "/" + key.domain))
	return int(hash.Sum32() % 60)
}

// planUptime brings the domains in memory in line with the live apps and
// returns them by the second of the minute they are checked at. It also
// watches the servers and prunes old counts once an hour.
func (s *AppService) planUptime(now time.Time) [60][]uptimeKey {
	var plan [60][]uptimeKey
	u := s.uptime
	u.load(s.apps.UptimeChecks)
	s.watchServers(now)
	if now.Sub(u.pruned) >= time.Hour {
		err := s.apps.PruneUptime(now.Add(-uptimeHoursKept), now.Add(-uptimeOutagesKept))
		if err != nil {
			slog.Error("prune uptime failed", "error", err)
		}
		u.pruned = now
	}
	targets, err := s.apps.UptimeTargets()
	if err != nil {
		slog.Error("list uptime targets failed", "error", err)
		return plan
	}
	live := map[string]bool{}
	seen := map[uptimeKey]bool{}
	var gone, reset []model.UptimeCheck
	u.mu.Lock()
	for _, target := range targets {
		live[target.AppID] = true
		domains, health := uptimeDomains(target.Domains, target.Detected)
		for _, domain := range domains {
			key := uptimeKey{target.AppID, domain}
			seen[key] = true
			state := u.states[key]
			if state == nil {
				state = &uptimeState{check: model.UptimeCheck{AppID: target.AppID, Domain: domain}, offset: uptimeOffset(key)}
				u.states[key] = state
			}
			state.workspaceID, state.appName = target.WorkspaceID, target.Name
			state.path = cmp.Or(state.check.Path, health)
			if !state.check.Paused {
				plan[state.offset] = append(plan[state.offset], key)
			}
		}
	}
	for key, state := range u.states {
		if seen[key] {
			continue
		}
		switch {
		case live[key.app]:
			// A live app no longer has the domain: its check goes.
			gone = append(gone, state.check)
			delete(u.states, key)
		case state.check.State != "":
			// A stopped or removed app is not checked; its check starts
			// over when it is live again.
			state.check.State, state.check.Since, state.last = "", nil, nil
			reset = append(reset, state.check)
		}
		if !live[key.app] && state.check.Path == "" && !state.check.Paused && !state.inflight {
			delete(u.states, key)
		}
	}
	u.mu.Unlock()
	for _, check := range gone {
		_ = s.apps.EndUptimeOutage(check.AppID, check.Domain, now)
		if err := s.apps.DeleteUptimeCheck(check.AppID, check.Domain); err != nil {
			slog.Error("delete uptime check failed", "error", err)
		}
	}
	for _, check := range reset {
		_ = s.apps.EndUptimeOutage(check.AppID, check.Domain, now)
		if err := s.apps.SaveUptimeState(check); err != nil {
			slog.Error("store uptime check failed", "error", err)
		}
	}
	return plan
}

// uptimeDomains are the domains of an app and its health path, if its
// live release has one: the domains the release answers on win over the
// ones set in moin, like for the tunnel.
func uptimeDomains(domainsJSON, detectedJSON string) (domains []string, health string) {
	_ = json.Unmarshal([]byte(domainsJSON), &domains)
	if detected := Detected(model.Deploy{Detected: detectedJSON}); detected != nil {
		if len(detected.Domains) > 0 {
			domains = detected.Domains
		}
		// The domains of the other services are checked too, in the order
		// of their services.
		for _, service := range slices.Sorted(maps.Keys(detected.ServiceDomains)) {
			domains = append(domains, detected.ServiceDomains[service]...)
		}
		if detected.Config != nil && uptimePath.MatchString(detected.Config.Health) {
			health = detected.Config.Health
		}
	}
	return domains, cmp.Or(health, "/")
}

// visit GETs a domain like a visitor: any answer below 500 is up, a 5xx, a
// timeout, a refused connection or a bad certificate is down. It reads at
// most a few KB of the body.
func (u *uptimeChecker) visit(ctx context.Context, domain, path string) uptimeResult {
	result := uptimeResult{at: time.Now()}
	ctx, cancel := context.WithTimeout(ctx, uptimeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.url(domain, path), nil)
	if err != nil {
		result.failure = "cannot check " + domain + path
		return result
	}
	request.Header.Set("User-Agent", build.Name+"-uptime/"+build.Version)
	response, err := u.client.Do(request)
	result.responseMS = int(time.Since(result.at).Milliseconds())
	if err != nil {
		result.failure = uptimeFailure(err)
		return result
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, uptimeBodyLimit))
	result.statusCode = response.StatusCode
	result.up = response.StatusCode < 500
	if !result.up {
		result.failure = "HTTP " + strconv.Itoa(response.StatusCode)
	}
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		expiry := response.TLS.PeerCertificates[0].NotAfter.UTC()
		result.certExpiry = &expiry
	}
	return result
}

// uptimeFailure says in a few words why a visit failed.
func uptimeFailure(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var hostnameErr x509.HostnameError
	var netErr net.Error
	switch {
	case errors.Is(err, ErrBucketPrivate):
		return "the domain points at a private address"
	case errors.As(err, &dnsErr):
		return "DNS: no address for " + dnsErr.Name
	case errors.As(err, &hostnameErr):
		return "the HTTPS certificate is not for this domain"
	case errors.As(err, &certErr):
		return "the HTTPS certificate is not valid: " + clipText(certErr.Err.Error(), 120)
	case errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Sprintf("no answer within %ds", int(uptimeTimeout.Seconds()))
	case strings.Contains(err.Error(), "connection refused"):
		return "connection refused"
	case strings.Contains(err.Error(), "connection reset"):
		return "connection reset"
	}
	return clipText(err.Error(), 160)
}

// finishUptime keeps a result, counts it, and when the domain went down or
// came back, stores that and writes it to the activity.
func (s *AppService) finishUptime(job uptimeJob, result uptimeResult) {
	u := s.uptime
	u.mu.Lock()
	state := u.states[job.key]
	if state == nil || state.check.Paused {
		if state != nil {
			state.inflight = false
		}
		u.mu.Unlock()
		return
	}
	state.inflight = false
	state.last = &result
	hour := job.started.UTC().Truncate(time.Hour)
	if u.hours[job.key] == nil {
		u.hours[job.key] = map[time.Time]*model.UptimeCounts{}
	}
	if u.hours[job.key][hour] == nil {
		u.hours[job.key][hour] = &model.UptimeCounts{}
	}
	u.hours[job.key][hour].Add(result.up, result.responseMS)
	before := state.check
	switch {
	case result.up && state.check.State != model.UptimeUp:
		at := job.started.UTC()
		state.check.State, state.check.Since = model.UptimeUp, &at
	case !result.up && state.check.State != model.UptimeDown:
		at := job.started.UTC()
		state.check.State, state.check.Since = model.UptimeDown, &at
	}
	check, workspaceID, appName := state.check, state.workspaceID, state.appName
	u.mu.Unlock()
	if check.State == before.State {
		return
	}
	err := s.apps.SaveUptimeState(check)
	if err != nil {
		slog.Error("store uptime state failed", "error", err)
	}
	actor := System(workspaceID)
	switch {
	case check.State == model.UptimeDown:
		_ = s.apps.StartUptimeOutage(check.AppID, check.Domain, *check.Since, result.failure)
		s.servers.record(actor, "uptime.down", "app", appName, map[string]string{"domain": check.Domain, "reason": result.failure})
	case before.State == model.UptimeDown:
		_ = s.apps.EndUptimeOutage(check.AppID, check.Domain, *check.Since)
		s.servers.record(actor, "uptime.up", "app", appName, map[string]string{"domain": check.Domain, "duration": check.Since.Sub(*before.Since).Round(time.Second).String()})
	}
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

// watchServers writes a server whose agent is gone for a while to the
// activity as down, and back up when it connects again.
func (s *AppService) watchServers(now time.Time) {
	servers, err := s.apps.ServersSeen()
	if err != nil {
		slog.Error("list servers for uptime failed", "error", err)
		return
	}
	for _, server := range servers {
		online := s.servers.Online(server.ID)
		switch {
		case online && server.DownSince != nil:
			err = s.apps.EndServerOutage(server.ID, now)
			if err == nil {
				s.servers.record(System(server.WorkspaceID), "server.up", "server", server.Name, map[string]string{"duration": now.Sub(*server.DownSince).Round(time.Second).String()})
			}
		case !online && server.DownSince == nil:
			gone := server.LastSeenAt
			if gone.Before(s.uptime.started) {
				gone = s.uptime.started
			}
			if now.Sub(gone) < serverDownAfter {
				continue
			}
			err = s.apps.StartServerOutage(server.ID, server.LastSeenAt)
			if err == nil {
				s.servers.record(System(server.WorkspaceID), "server.down", "server", server.Name, map[string]string{"since": server.LastSeenAt.UTC().Format("2006-01-02 15:04 UTC")})
			}
		}
		if err != nil {
			slog.Error("store server outage failed", "error", err, "server_id", server.ID)
		}
	}
}

// UptimeDomain is the uptime of one domain of an app.
type UptimeDomain struct {
	Domain string
	// Path is where the check goes; CustomPath says it was set, not the
	// health path or /.
	Path       string
	CustomPath bool
	Paused     bool
	// State is up, down, pending or paused, Since when it began.
	State string
	Since *time.Time
	// The newest check, nil before the first since the control plane
	// started.
	CheckedAt     *time.Time
	StatusCode    int
	ResponseMS    int
	Error         string
	CertExpiresAt *time.Time
	// Uptime in percent over the last 24 hours, 7 and 30 days; nil
	// without a check in that time.
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
	u.load(s.apps.UptimeChecks)
	key := uptimeKey{app.ID, domain}
	view := UptimeDomain{Domain: domain, Path: health, State: model.UptimePending}
	pending := map[time.Time]model.UptimeCounts{}
	u.mu.Lock()
	if state := u.states[key]; state != nil {
		view.Paused, view.CustomPath = state.check.Paused, state.check.Path != ""
		view.Path = cmp.Or(state.check.Path, health)
		if state.check.State != "" {
			view.State, view.Since = state.check.State, state.check.Since
		}
		if last := state.last; last != nil {
			at := last.at.UTC()
			view.CheckedAt, view.StatusCode, view.ResponseMS, view.Error, view.CertExpiresAt = &at, last.statusCode, last.responseMS, last.failure, last.certExpiry
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
	for _, window := range []struct {
		span time.Duration
		into **float64
	}{{24 * time.Hour, &view.Uptime24h}, {7 * 24 * time.Hour, &view.Uptime7d}, {30 * 24 * time.Hour, &view.Uptime30d}} {
		since := now.Add(-window.span).UTC().Truncate(time.Hour)
		counts, err := s.apps.UptimeCounts(app.ID, domain, since)
		if err != nil {
			return UptimeDomain{}, err
		}
		for hour, more := range pending {
			if !hour.Before(since) {
				counts.Merge(more)
			}
		}
		if counts.Checks > 0 {
			percent := math.Round(float64(counts.Up)/float64(counts.Checks)*100000) / 1000
			*window.into = &percent
			if window.span == 24*time.Hour {
				view.AverageMS = counts.ResponseMS / counts.Checks
			}
		}
	}
	outages, err := s.apps.UptimeOutages(app.ID, domain, uptimeOutagesShown)
	if err != nil {
		return UptimeDomain{}, err
	}
	view.Outages = outages
	return view, nil
}

// SetUptimeCheck changes where the check of a domain goes, nil path keeps
// it and "" goes back to the health path or /, and pauses or resumes it,
// nil keeps it. A paused check starts over when it resumes.
func (s *AppService) SetUptimeCheck(actor Actor, idOrName, domain string, path *string, paused *bool) (*model.App, UptimeDomain, error) {
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
	u.load(s.apps.UptimeChecks)
	now := s.now().UTC()
	key := uptimeKey{app.ID, domain}
	u.mu.Lock()
	state := u.states[key]
	if state == nil {
		state = &uptimeState{check: model.UptimeCheck{AppID: app.ID, Domain: domain}, offset: uptimeOffset(key)}
		u.states[key] = state
	}
	before := state.check
	if path != nil {
		state.check.Path = *path
		state.path = cmp.Or(*path, health)
	}
	if paused != nil {
		state.check.Paused = *paused
		if *paused {
			state.check.State, state.check.Since, state.last = "", nil, nil
		}
	}
	check := state.check
	u.mu.Unlock()
	if check != before {
		err = s.apps.SaveUptimeCheck(check)
		if err != nil {
			// What is not stored does not count: the page and the checks
			// keep what the database has.
			u.mu.Lock()
			state.check.Path, state.check.Paused = before.Path, before.Paused
			state.path = cmp.Or(before.Path, health)
			u.mu.Unlock()
			return nil, UptimeDomain{}, err
		}
	}
	if before.State == model.UptimeDown && check.State == "" {
		_ = s.apps.EndUptimeOutage(app.ID, domain, now)
	}
	if check.Path != before.Path {
		s.servers.record(actor, "uptime.path_set", "app", app.Name, map[string]string{"domain": domain, "path": check.Path, "from": before.Path})
	}
	if check.Paused != before.Paused {
		action := "uptime.resumed"
		if check.Paused {
			action = "uptime.paused"
		}
		s.servers.record(actor, action, "app", app.Name, map[string]string{"domain": domain})
	}
	view, err := s.uptimeDomain(app, domain, health, now)
	return app, view, err
}

// DownDomain is a domain of an app that the checks find down.
type DownDomain struct {
	App    string
	Domain string
	Since  *time.Time
}

// DownDomains lists the domains of apps that are down now, unpaused, from
// the checker's state; the overview shows them.
func (s *AppService) DownDomains(apps []model.App) []DownDomain {
	u := s.uptime
	u.load(s.apps.UptimeChecks)
	names := make(map[string]string, len(apps))
	for _, app := range apps {
		names[app.ID] = app.Name
	}
	var down []DownDomain
	u.mu.Lock()
	defer u.mu.Unlock()
	for key, state := range u.states {
		name, ok := names[key.app]
		if !ok || state.check.Paused || state.check.State != model.UptimeDown {
			continue
		}
		down = append(down, DownDomain{App: name, Domain: key.domain, Since: state.check.Since})
	}
	sort.Slice(down, func(i, j int) bool { return down[i].Domain < down[j].Domain })
	return down
}
