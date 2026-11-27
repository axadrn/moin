package watcher

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

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
)

// DefaultDir is where the watcher keeps what it knows.
const DefaultDir = "/var/lib/moin/watcher"

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

func newWatcher(config Config, dir string, now time.Time) (*watcher, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "state.json")
	s, err := loadState(path)
	if err != nil {
		return nil, err
	}
	s.forget(config)
	return &watcher{
		config: config, path: path, started: now, state: s,
		client: &http.Client{
			Timeout:       visitTimeout,
			Transport:     &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: visitTimeout},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
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
	server := &http.Server{Handler: w.routes(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
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
	if err := json.NewDecoder(io.LimitReader(r.Body, reportLimit)).Decode(&got); err != nil {
		http.Error(rw, "not a report", http.StatusBadRequest)
		return
	}
	if got.Server != name {
		http.Error(rw, "this token is for server "+name, http.StatusBadRequest)
		return
	}
	w.mu.Lock()
	w.state.receive(name, got, time.Now())
	w.dirty = true
	w.mu.Unlock()
	rw.WriteHeader(http.StatusNoContent)
}

// serverOf is the server whose token the Authorization header carries.
// Every token is compared, in constant time.
func (w *watcher) serverOf(header string) (string, bool) {
	token, ok := cutBearer(header)
	if !ok {
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

func cutBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return "", false
	}
	return header[len(prefix):], true
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
			return
		case <-checks.C:
			w.check(ctx)
		case <-visits.C:
			w.visitAll(ctx)
			w.check(ctx)
		}
	}
}

// check saves the state when it changed and sends the alerts of what
// changed, once each.
func (w *watcher) check(ctx context.Context) {
	w.mu.Lock()
	messages := w.state.alerts(w.started, time.Now())
	if w.dirty || len(messages) > 0 {
		if err := w.state.save(w.path); err != nil {
			slog.Error("save state", "error", err)
		}
		w.dirty = false
	}
	w.mu.Unlock()
	for _, m := range messages {
		slog.Info("alert", "server", m.Server, "app", m.App, "title", m.Title)
		for _, d := range w.config.Alerts {
			sending, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			if err := alert.Send(sending, d, m); err != nil {
				slog.Error("alert not sent", "kind", d.Kind, "title", m.Title, "error", err)
			}
			cancel()
		}
	}
}

// visitAll visits every domain at once and counts what each visit said.
func (w *watcher) visitAll(ctx context.Context) {
	w.mu.Lock()
	names := sortedKeys(w.state.Domains)
	w.mu.Unlock()
	type result struct {
		up     bool
		reason string
	}
	results := make([]result, len(names))
	var wait sync.WaitGroup
	for i, name := range names {
		wait.Go(func() {
			results[i].up, results[i].reason = w.visit(ctx, name)
		})
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
			d.visited(results[i].up, results[i].reason, now)
		}
	}
	w.dirty = true
}

// visit GETs a domain like a visitor: any answer below 500 is up; a 5xx,
// a timeout, a refused connection or a bad certificate is down.
func (w *watcher) visit(ctx context.Context, domain string) (bool, string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return false, "not a domain"
	}
	request.Header.Set("User-Agent", build.Name+"-watcher/"+build.Version)
	response, err := w.client.Do(request)
	if err != nil {
		return false, visitFailure(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode >= 500 {
		return false, "HTTP " + strconv.Itoa(response.StatusCode)
	}
	return true, ""
}

// visitFailure says in a few words why a visit failed.
func visitFailure(err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case errors.As(err, &dnsErr):
		return "DNS has no address for " + dnsErr.Name
	case errors.As(err, &netErr) && netErr.Timeout():
		return "no answer within " + visitTimeout.String()
	}
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return err.Error()
}
