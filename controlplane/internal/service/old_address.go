package service

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
)

const (
	// oldAddressDays is how far the admin view looks back.
	oldAddressDays = 14
	// oldAddressQuietDays without a request means the old address can go.
	oldAddressQuietDays = 14
	oldAddressFlush     = time.Minute
)

// OldAddressService counts the requests that still arrive on an old
// address of the control plane (OLD_URLS), so admins know when it can go.
// Counts gather in memory and go to history.db once a minute.
type OldAddressService struct {
	days  *repository.OldAddressDays
	hosts []string

	mu      sync.Mutex
	pending map[oldAddressKey]oldAddressCount
}

type oldAddressKey struct{ host, kind string }

type oldAddressCount struct {
	requests int
	last     time.Time
}

func NewOldAddressService(days *repository.OldAddressDays, oldURLs []string) *OldAddressService {
	s := &OldAddressService{days: days, pending: map[oldAddressKey]oldAddressCount{}}
	for _, raw := range oldURLs {
		if parsed, err := url.Parse(raw); err == nil && parsed.Hostname() != "" {
			s.hosts = append(s.hosts, strings.ToLower(parsed.Hostname()))
		}
	}
	return s
}

// Hosts are the old hosts, lowercase.
func (s *OldAddressService) Hosts() []string {
	if s == nil {
		return nil
	}
	return s.hosts
}

// Count counts one request of kind (web, api, agent, download, webhook)
// on host.
func (s *OldAddressService) Count(host, kind string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := oldAddressKey{host, kind}
	count := s.pending[key]
	count.requests++
	count.last = time.Now()
	s.pending[key] = count
}

// Run writes the counts once a minute until ctx ends, then once more.
func (s *OldAddressService) Run(ctx context.Context) {
	if s == nil || len(s.hosts) == 0 {
		return
	}
	ticker := time.NewTicker(oldAddressFlush)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.flush()
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

func (s *OldAddressService) flush() {
	s.mu.Lock()
	pending := s.pending
	s.pending = map[oldAddressKey]oldAddressCount{}
	s.mu.Unlock()
	for key, count := range pending {
		if err := s.days.Add(key.host, key.kind, count.requests, count.last); err != nil {
			slog.Warn("old address: count lost", "host", key.host, "error", err)
		}
	}
}

// Report is the traffic of the last days, counts not yet written included.
func (s *OldAddressService) Report() (*model.OldAddress, error) {
	report := &model.OldAddress{Hosts: s.Hosts(), Days: oldAddressDays, QuietDays: oldAddressQuietDays, Rows: []model.OldAddressDay{}, Quiet: true}
	if s == nil || len(s.hosts) == 0 {
		return report, nil
	}
	s.flush()
	since := time.Now().UTC().AddDate(0, 0, -(oldAddressDays - 1))
	rows, err := s.days.Since(since)
	if err != nil {
		return nil, err
	}
	report.Rows = rows
	for _, row := range rows {
		report.Total += row.Requests
		if report.LastAt == nil || row.LastAt.After(*report.LastAt) {
			last := row.LastAt
			report.LastAt = &last
		}
	}
	report.Quiet = report.LastAt == nil || time.Since(*report.LastAt) > oldAddressQuietDays*24*time.Hour
	return report, nil
}
