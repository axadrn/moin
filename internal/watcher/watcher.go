package watcher

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

const (
	// checkEvery is how often the watcher looks for silent servers.
	checkEvery = 5 * time.Second
	// visitEvery is how often it visits every domain.
	visitEvery = time.Minute
	// visitTimeout is how long a visit waits for the answer.
	visitTimeout = 10 * time.Second
	// reportLimit is the largest report it reads.
	reportLimit = 1 << 20
	// The most a report may hold: apps, and domains and databases per app.
	maxApps      = 100
	maxDomains   = 50
	maxDatabases = 20
	// maxError is how much of a backup error the watcher keeps.
	maxError = 200
)

// DefaultDir is where the watcher keeps what it knows.
const DefaultDir = "/var/lib/" + build.Name + "/watcher"

// watcher holds the config and the state. mu guards state.
type watcher struct {
	config  Config
	path    string
	started time.Time
	client  *http.Client

	mu    sync.Mutex
	state *state
	// dirty is true when state changed since the last save.
	dirty bool
}

// newWatcher loads what the watcher knew from dir, minus the servers the
// config no longer has.
func newWatcher(config Config, dir string, now time.Time) (*watcher, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "state.json")
	s, err := loadState(path)
	if err != nil {
		return nil, err
	}
	s.forget(config, now)
	return &watcher{config: config, path: path, started: now, state: s, client: visitClient()}, nil
}

// visitClient visits like a new visitor each time: a new connection, no
// redirects followed (a redirect is an answer), and only public addresses,
// so a report cannot make the watcher knock on its own network.
func visitClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: refusePrivate}
	return &http.Client{
		Timeout: visitTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: visitTimeout,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// errPrivate is a visit to an address that is not on the public internet.
var errPrivate = errors.New("the domain points at a private address")

// sharedNetwork is 100.64.0.0/10: carrier networks and Tailscale.
var sharedNetwork = netip.MustParsePrefix("100.64.0.0/10")

// refusePrivate stops a connection to loopback, private, link-local,
// shared or unspecified addresses. It runs after DNS, so a name that
// points inside cannot slip past it.
func refusePrivate(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || sharedNetwork.Contains(ip) {
		return errPrivate
	}
	return nil
}

// Run serves reports and the page on listen, and alerts, until ctx ends.
func Run(ctx context.Context, listen, configPath, dir string) error {
	config, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	w, err := newWatcher(config, dir, time.Now())
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           w.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go w.loop(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("watcher listens", "address", listener.Addr().String(), "servers", len(config.Servers), "alerts", len(config.Alerts))
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// routes are the only two: reports in, the page out.
func (w *watcher) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+report.Path, w.receive)
	mux.HandleFunc("GET /{$}", w.page)
	return mux
}

// receive takes a report. The token names the server; an unknown token
// gets 401. The answer is a status code and nothing a daemon could act on.
func (w *watcher) receive(rw http.ResponseWriter, r *http.Request) {
	name, ok := w.serverOf(r.Header.Get("Authorization"))
	if !ok {
		http.Error(rw, "unknown token", http.StatusUnauthorized)
		return
	}
	var got report.Report
	if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, reportLimit)).Decode(&got); err != nil {
		http.Error(rw, "not a report", http.StatusBadRequest)
		return
	}
	if got.Server != name {
		http.Error(rw, "this token is for server "+name, http.StatusBadRequest)
		return
	}
	if err := checkReport(&got); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	w.mu.Lock()
	w.state.receive(name, got, time.Now())
	w.dirty = true
	w.mu.Unlock()
	rw.WriteHeader(http.StatusNoContent)
}

// hostname is a plain domain name: no port, path, user or IP address.
var hostname = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkReport refuses a report that is too big or names things oddly, and
// cuts long backup errors. The watcher visits the domains of a report, so
// each must be a plain public name.
func checkReport(r *report.Report) error {
	if len(r.Apps) > maxApps {
		return fmt.Errorf("more than %d apps", maxApps)
	}
	for i := range r.Apps {
		app := &r.Apps[i]
		if !serverName.MatchString(app.Name) {
			return fmt.Errorf("%q is not an app name", app.Name)
		}
		if len(app.Domains) > maxDomains || len(app.Databases) > maxDatabases {
			return fmt.Errorf("%s has more than %d domains or %d databases", app.Name, maxDomains, maxDatabases)
		}
		for _, d := range app.Domains {
			if len(d) > 253 || !hostname.MatchString(d) || net.ParseIP(d) != nil {
				return fmt.Errorf("%q of %s is not a plain domain name", d, app.Name)
			}
		}
		for j := range app.Databases {
			db := &app.Databases[j]
			if db.Name == "" || len(db.Name) > 200 || !utf8.ValidString(db.Name) || strings.ContainsFunc(db.Name, unicode.IsControl) {
				return fmt.Errorf("%q of %s is not a database name", db.Name, app.Name)
			}
			db.Error = clip(db.Error, maxError)
		}
	}
	return nil
}

// clip cuts text to at most n bytes, on a whole character.
func clip(text string, n int) string {
	if len(text) <= n {
		return text
	}
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n] + "…"
}

// serverOf is the server whose token the Authorization header carries.
// Every token is compared, in constant time.
func (w *watcher) serverOf(header string) (string, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return "", false
	}
	found := ""
	for name, want := range w.config.Servers {
		if subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1 {
			found = name
		}
	}
	return found, found != ""
}

// loop looks for silent servers every few seconds and visits every
// domain every minute, until ctx ends.
func (w *watcher) loop(ctx context.Context) {
	checks := time.NewTicker(checkEvery)
	visits := time.NewTicker(visitEvery)
	defer checks.Stop()
	defer visits.Stop()
	for {
		select {
		case <-ctx.Done():
			w.save()
			return
		case <-checks.C:
			w.check(ctx)
		case <-visits.C:
			w.visitAll(ctx)
			w.check(ctx)
		}
	}
}

// check finds what changed, sends its alerts once each, tries again the
// alerts a destination did not take, and saves the state.
func (w *watcher) check(ctx context.Context) {
	w.mu.Lock()
	for _, m := range w.state.alerts(w.started, time.Now()) {
		slog.Info("alert", "server", m.Server, "app", m.App, "title", m.Title)
		for _, d := range w.config.Alerts {
			w.state.Unsent = append(w.state.Unsent, unsent{To: d, Message: m})
		}
	}
	todo := w.state.Unsent
	w.state.Unsent = nil
	w.mu.Unlock()

	failed := w.send(ctx, todo)

	w.mu.Lock()
	w.state.Unsent = append(failed, w.state.Unsent...)
	w.state.Unsent = w.state.Unsent[max(0, len(w.state.Unsent)-kept):]
	w.dirty = w.dirty || len(todo) > 0
	w.mu.Unlock()
	w.save()
}

// send sends each alert and returns the ones that did not go out. After
// one failure a destination gets nothing more this round, so a dead one
// does not hold up the others.
func (w *watcher) send(ctx context.Context, todo []unsent) []unsent {
	var failed []unsent
	down := map[alert.Destination]bool{}
	for _, u := range todo {
		if down[u.To] {
			failed = append(failed, u)
			continue
		}
		if err := alert.Send(ctx, u.To, u.Message); err != nil {
			slog.Error("alert not sent, trying again soon", "kind", u.To.Kind, "title", u.Message.Title, "error", err)
			down[u.To] = true
			failed = append(failed, u)
		}
	}
	return failed
}

// save writes the state when it changed.
func (w *watcher) save() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.dirty {
		return
	}
	if err := w.state.save(w.path); err != nil {
		slog.Error("save state", "error", err)
		return
	}
	w.dirty = false
}

// visitAll visits every domain at once and counts what each visit said.
func (w *watcher) visitAll(ctx context.Context) {
	w.mu.Lock()
	names := slices.Sorted(maps.Keys(w.state.Domains))
	w.mu.Unlock()
	failures := make([]string, len(names))
	var wait sync.WaitGroup
	for i, name := range names {
		wait.Go(func() { failures[i] = w.visit(ctx, name) })
	}
	wait.Wait()
	if ctx.Err() != nil {
		return
	}
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, name := range names {
		if d := w.state.Domains[name]; d != nil {
			d.visited(failures[i] == "", failures[i], now)
		}
	}
	w.dirty = true
}

// visit GETs a domain like a visitor and says why it failed, or "" when it
// is up: any answer below 500 is up; a 5xx, a timeout, a refused
// connection or a bad certificate is down.
func (w *watcher) visit(ctx context.Context, domain string) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return "not a domain"
	}
	request.Header.Set("User-Agent", build.Name+"-watcher/"+build.Version)
	response, err := w.client.Do(request)
	if err != nil {
		return visitFailure(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode >= 500 {
		return "HTTP " + strconv.Itoa(response.StatusCode)
	}
	return ""
}

// visitFailure says in a few words why a visit got no answer.
func visitFailure(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case errors.Is(err, errPrivate):
		return errPrivate.Error()
	case errors.As(err, &dnsErr):
		return "DNS has no address for " + dnsErr.Name
	case errors.As(err, &certErr):
		return "the HTTPS certificate is not valid"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "no answer within " + visitTimeout.String()
	case errors.As(err, &urlErr):
		return clip(urlErr.Err.Error(), maxError)
	}
	return clip(err.Error(), maxError)
}
