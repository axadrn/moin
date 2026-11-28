package watcher

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

// This file is `moin watcher run`: one Watcher, its config file, its
// state file, and a small site with two routes, reports in, the page out.

const (
	// checkEvery is how often the watcher looks for silent servers.
	checkEvery = 5 * time.Second
	// visitEvery is how often it visits every domain.
	visitEvery = time.Minute
	// visitsAtOnce is how many visits run at the same time.
	visitsAtOnce = 64
	// reportLimit is the largest report it reads.
	reportLimit = 1 << 20
)

// DefaultDir is where the watcher keeps what it knows.
const DefaultDir = "/var/lib/" + build.Name + "/watcher"

// site is a watcher with its config, served on its own.
type site struct {
	config  Config
	watcher *Watcher
	visitor *Visitor
	// statePath is the file the watcher's state is saved in.
	statePath string
}

// newSite loads what the watcher knew from dir.
func newSite(config Config, dir string, now time.Time) (*site, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "state.json")
	saved, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	servers := slices.Sorted(maps.Keys(config.Servers))
	w, err := newAt(servers, config.Alerts, saved, now)
	if err != nil {
		return nil, fmt.Errorf("%s: %w. Delete it to start fresh", path, err)
	}
	return &site{config: config, watcher: w, visitor: NewVisitor(visitsAtOnce), statePath: path}, nil
}

// Run serves reports and the page on listen, and alerts, until ctx ends.
func Run(ctx context.Context, listen, configPath, dir string) error {
	config, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	s, err := newSite(config, dir, time.Now())
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go s.loop(ctx)
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
func (s *site) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+report.Path, s.receive)
	mux.HandleFunc("GET /{$}", s.page)
	return mux
}

// receive takes a report. The token names the server; an unknown token
// gets 401. The answer is a status code and nothing a daemon could act on.
func (s *site) receive(rw http.ResponseWriter, r *http.Request) {
	name, ok := s.serverOf(r.Header.Get("Authorization"))
	if !ok {
		http.Error(rw, "unknown token", http.StatusUnauthorized)
		return
	}
	var got report.Report
	if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, reportLimit)).Decode(&got); err != nil {
		http.Error(rw, "not a report", http.StatusBadRequest)
		return
	}
	if err := s.watcher.Receive(name, got); err != nil {
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

// serverOf is the server whose token the Authorization header carries.
// Every token is compared, in constant time.
func (s *site) serverOf(header string) (string, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		return "", false
	}
	found := ""
	for name, want := range s.config.Servers {
		if subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1 {
			found = name
		}
	}
	return found, found != ""
}

// loop looks for silent servers every few seconds and visits every
// domain every minute, until ctx ends. After each round it saves.
func (s *site) loop(ctx context.Context) {
	checks := time.NewTicker(checkEvery)
	visits := time.NewTicker(visitEvery)
	defer checks.Stop()
	defer visits.Stop()
	for {
		select {
		case <-ctx.Done():
			s.save()
			return
		case <-checks.C:
			s.watcher.Tick(ctx)
		case <-visits.C:
			s.watcher.Visit(ctx, s.visitor)
			s.watcher.Tick(ctx)
		}
		s.save()
	}
}

// save writes the state file whole, or not at all, when it changed.
func (s *site) save() {
	err := s.watcher.Save(func(data []byte) error {
		temp := s.statePath + ".tmp"
		if err := os.WriteFile(temp, data, 0o600); err != nil {
			return err
		}
		return os.Rename(temp, s.statePath)
	})
	if err != nil {
		slog.Error("save state", "error", err)
	}
}
