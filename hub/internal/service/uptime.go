package service

import (
	"errors"
	"hash/fnv"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// Uptime. Every domain of every live app gets one GET a minute, like
// a visitor, at its own second of the minute, so the visits spread evenly
// instead of starting all at once. A failed visit is tried again a few
// seconds later; only when that fails too is the domain down. The newest
// results live in memory. The database gets a write when a state or a
// setting changes, and the counts per domain and hour in one transaction
// every few minutes.
const (
	// uptimeTimeout is how long a visitor waits for the answer.
	uptimeTimeout    = 10 * time.Second
	uptimeRetryAfter = 5 * time.Second
	// uptimeWorkers bounds the visits that run at once; uptimeQueue the
	// ones that wait. A visit that finds no room is skipped this minute,
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
	// serverDownAfter is how long a daemon may be gone before its server
	// counts as down: a restart or a short network hiccup is not.
	serverDownAfter = 2 * time.Minute
)

var (
	ErrUptimeDomain = errors.New("the app has no such domain")
	ErrUptimePath   = errors.New("the path must start with / and have no spaces")
	uptimePath      = regexp.MustCompile(`^/[^\s#]{0,255}$`)
)

// uptimeKey names uptime of one domain of an app.
type uptimeKey struct{ app, domain string }

// uptimeState is one domain in memory: what is stored, where its next
// visit goes, and the newest result, which is only kept here.
type uptimeState struct {
	stored      model.DomainUptime
	workspaceID string
	appName     string
	// path is where the next visit goes: the set path, the health path or /.
	path string
	// offset is the second of the minute its uptime runs at.
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

// uptimeJob is one visit to run: its second try has attempt 1, and started
// is when the first try ran.
type uptimeJob struct {
	key     uptimeKey
	path    string
	started time.Time
	attempt int
}

// uptimeRunner holds what the visits keep in memory.
type uptimeRunner struct {
	mu     sync.Mutex
	loaded bool
	states map[uptimeKey]*uptimeState
	// hours are the counts not in the database yet, by domain and hour.
	hours map[uptimeKey]map[time.Time]*model.UptimeCounts

	client *http.Client
	// url is where a visit of a domain and path goes; tests point it at
	// an httptest server.
	url        func(domain, path string) string
	retryAfter time.Duration
	// started is when uptime started: a daemon counts as gone only
	// from then on, since none could connect while the hub was
	// down itself.
	started time.Time
	pruned  time.Time
	skipped int
}

func newUptimeRunner() *uptimeRunner {
	return &uptimeRunner{
		states:     map[uptimeKey]*uptimeState{},
		hours:      map[uptimeKey]map[time.Time]*model.UptimeCounts{},
		url:        func(domain, path string) string { return "https://" + domain + path },
		retryAfter: uptimeRetryAfter,
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
		key := uptimeKey{stored.AppID, stored.Domain}
		u.states[key] = &uptimeState{stored: stored, offset: uptimeOffset(key)}
	}
	u.loaded = true
}

// begin marks a visit as running and returns its job; false when it is
// paused, gone or still running from the minute before.
func (u *uptimeRunner) begin(key uptimeKey, now time.Time) (uptimeJob, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	state := u.states[key]
	if state == nil || state.stored.Paused || state.inflight {
		return uptimeJob{}, false
	}
	state.inflight = true
	return uptimeJob{key: key, path: state.path, started: now}, true
}

// end marks a visit as done without a result.
func (u *uptimeRunner) end(key uptimeKey, skipped bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if state := u.states[key]; state != nil {
		state.inflight = false
	}
	if skipped {
		u.skipped++
		if u.skipped%1000 == 1 {
			slog.Warn("uptime skipped: more visits than workers", "skipped", u.skipped)
		}
	}
}

// uptimeOffset is the second of the minute a domain is visited at, the same
// on every start.
func uptimeOffset(key uptimeKey) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key.app + "/" + key.domain))
	return int(hash.Sum32() % 60)
}
