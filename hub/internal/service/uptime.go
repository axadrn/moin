package service

import (
	"context"
	"errors"
	"hash/fnv"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// Uptime. Every domain that serves something gets a watcher: a goroutine
// that visits it once a minute, like a visitor, at its own moment of the
// minute. A dead domain only ever slows down its own watcher. A domain
// goes down, and back up, only after UptimeInARow visits in a row say so.
// The newest results live in memory. The database gets a write when a
// state or a setting changes, and the counts per domain and hour in one
// transaction every few minutes.
const (
	// uptimeTimeout is how long a visitor waits for the answer.
	uptimeTimeout = 10 * time.Second
	// uptimeEvery is how often a watcher visits its domain.
	uptimeEvery = time.Minute
	// UptimeInARow is how many visits in a row must say the opposite
	// before a domain goes down or comes back: a restart or a short
	// network hiccup changes nothing.
	UptimeInARow = 3
	// serverDownAfter is how long a daemon may be gone before its server
	// counts as down.
	serverDownAfter = 3 * time.Minute
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
	// changing counts the visits in a row that say the opposite of the
	// state, since the first of them; 0 while they agree with it.
	changing      int
	changingSince time.Time
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

// load reads the stored uptime into memory, once. Until it worked, uptime
// does nothing: it would start from nothing and lose what is stored.
func (u *uptimeRunner) load(read func() ([]model.DomainUptime, error)) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.loaded {
		return nil
	}
	all, err := read()
	if err != nil {
		return err
	}
	for _, stored := range all {
		u.states[uptimeKey{stored.AppID, stored.Domain}] = &uptimeState{stored: stored}
	}
	u.loaded = true
	return nil
}

// see changes the state after a visit. The state flips only when
// UptimeInARow visits in a row said the opposite, down and back up alike,
// so a blip or a flapping site changes nothing. A new domain is up at its
// first answer: there is nothing to flip yet. It says whether the state
// changed.
func (state *uptimeState) see(result uptimeResult) bool {
	seen := model.UptimeDown
	if result.up {
		seen = model.UptimeUp
	}
	if seen == state.stored.State {
		state.changing = 0
		return false
	}
	if state.stored.State == "" && result.up {
		state.changing = 0
		state.stored.State, state.stored.Since = model.UptimeUp, &result.at
		return true
	}
	if state.changing == 0 {
		state.changingSince = result.at
	}
	state.changing++
	if state.changing < UptimeInARow {
		return false
	}
	since := state.changingSince
	state.stored.State, state.stored.Since = seen, &since
	state.changing = 0
	return true
}

// startOver forgets what the visits found, for a paused domain or one of
// an app that is not live: its uptime begins again at pending.
func (state *uptimeState) startOver() {
	state.stored.State, state.stored.Since = "", nil
	state.last, state.changing = nil, 0
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
