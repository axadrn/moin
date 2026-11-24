package service

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// Uptime. Every domain that serves something gets a watcher: a goroutine
// that visits it once a minute, like a visitor, at its own moment of the
// minute. A dead domain only ever slows down its own watcher. A domain is
// down only after two minutes without an answer. The newest results live
// in memory. The database gets a write when a state or a setting changes,
// and the counts per domain and hour in one transaction every few minutes.
const (
	// uptimeTimeout is how long a visitor waits for the answer.
	uptimeTimeout = 10 * time.Second
	// uptimeEvery is how often a watcher visits its domain.
	uptimeEvery = time.Minute
	// downAfter is how long a domain may fail, or a daemon be gone, before
	// it counts as down: a restart or a short network hiccup is not.
	downAfter = 2 * time.Minute
	// uptimeMailEvery is how often a domain may send a down alert at most.
	uptimeMailEvery = time.Hour
	// uptimeFlushEvery is how often the counts go to the database.
	uptimeFlushEvery = 5 * time.Minute
	uptimeBodyLimit  = 4 << 10
	// uptimeHoursKept covers the 30 days uptime shows; outages stay longer.
	uptimeHoursKept   = 31 * 24 * time.Hour
	uptimeOutagesKept = 90 * 24 * time.Hour
	// uptimeOutagesShown is how many outages a domain lists.
	uptimeOutagesShown = 10
)

var (
	ErrUptimeDomain = errors.New("uptime visits no such domain of the app")
	ErrUptimePath   = errors.New("the path must start with / and have no spaces")
	uptimePath      = regexp.MustCompile(`^/[^\s#]{0,255}$`)
)

// uptimeKey names uptime of one domain of an app.
type uptimeKey struct{ app, domain string }

// uptimeState is one domain in memory: what is stored, where its next
// visit goes, its watcher, and what only memory keeps.
type uptimeState struct {
	stored      model.DomainUptime
	workspaceID string
	appName     string
	// path is where the next visit goes: the set path, the health path or /.
	path string
	// stop ends the domain's watcher; nil while none runs.
	stop context.CancelFunc
	last *uptimeResult
	// failingSince is the first of the failed visits in a row; nil while
	// the domain answers.
	failingSince *time.Time
	// downMailAt is when the last down alert went out. downMailed says it
	// was sent for this outage, so its end sends an up alert.
	downMailAt time.Time
	downMailed bool
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

// uptimeRunner holds what the watchers keep in memory.
type uptimeRunner struct {
	mu     sync.Mutex
	loaded bool
	states map[uptimeKey]*uptimeState
	// hours are the counts not in the database yet, by domain and hour.
	hours map[uptimeKey]map[time.Time]*model.UptimeCounts
	// watchers are the running watchers: RunUptime waits for them before
	// it writes the last counts.
	watchers sync.WaitGroup

	client *http.Client
	// url is where a visit of a domain and path goes; tests point it at
	// an httptest server.
	url func(domain, path string) string
	// every is how often a watcher visits; tests make it shorter.
	every time.Duration
	// started is when uptime started: a daemon counts as gone only
	// from then on, since none could connect while the hub was
	// down itself.
	started time.Time
	pruned  time.Time
}

func newUptimeRunner() *uptimeRunner {
	return &uptimeRunner{
		states: map[uptimeKey]*uptimeState{},
		hours:  map[uptimeKey]map[time.Time]*model.UptimeCounts{},
		url:    func(domain, path string) string { return "https://" + domain + path },
		every:  uptimeEvery,
	}
}

// load reads the stored uptime into memory, once.
func (u *uptimeRunner) load(read func() ([]model.DomainUptime, error)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.loaded {
		return
	}
	all, err := read()
	if err != nil {
		slog.Error("load uptime failed", "error", err)
		return
	}
	for _, stored := range all {
		u.states[uptimeKey{stored.AppID, stored.Domain}] = &uptimeState{stored: stored}
	}
	u.loaded = true
}

// see changes the state after a visit: up at once, down only when the
// visits failed for downAfter, so a short blip changes nothing. It says
// whether the state changed.
func (state *uptimeState) see(result uptimeResult) bool {
	if result.up {
		state.failingSince = nil
		if state.stored.State == model.UptimeUp {
			return false
		}
		state.stored.State, state.stored.Since = model.UptimeUp, &result.at
		return true
	}
	if state.failingSince == nil {
		state.failingSince = &result.at
	}
	if state.stored.State == model.UptimeDown || result.at.Sub(*state.failingSince) < downAfter {
		return false
	}
	state.stored.State, state.stored.Since = model.UptimeDown, state.failingSince
	return true
}

// mail says whether the change see just made sends an alert: a down at
// most once an hour per domain, an up only when its down sent one.
func (state *uptimeState) mail(now time.Time) bool {
	switch state.stored.State {
	case model.UptimeDown:
		if now.Sub(state.downMailAt) < uptimeMailEvery {
			return false
		}
		state.downMailAt, state.downMailed = now, true
		return true
	case model.UptimeUp:
		mailed := state.downMailed
		state.downMailed = false
		return mailed
	}
	return false
}

// startOver forgets what the visits found, for a paused domain or one of
// an app that is not live: its uptime begins again at pending.
func (state *uptimeState) startOver() {
	state.stored.State, state.stored.Since = "", nil
	state.last, state.failingSince, state.downMailed = nil, nil, false
}

// stopWatcher ends the domain's watcher, if one runs. The caller holds
// u.mu.
func (state *uptimeState) stopWatcher() {
	if state.stop != nil {
		state.stop()
		state.stop = nil
	}
}

// untilVisit is how long a watcher sleeps until its domain's moment of the
// minute. Each domain has its own moment, the same on every start, so the
// visits spread over the minute instead of starting all at once.
func (u *uptimeRunner) untilVisit(key uptimeKey, now time.Time) time.Duration {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key.app + "/" + key.domain))
	next := now.Truncate(u.every).Add(time.Duration(hash.Sum64() % uint64(u.every)))
	if !next.After(now) {
		next = next.Add(u.every)
	}
	return next.Sub(now)
}
