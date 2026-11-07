package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/google/uuid"
)

const (
	// telemetryEvery is how often a self-hosted control plane reports.
	telemetryEvery = 5 * time.Minute
	// telemetryActive is how long a report counts: a control plane that
	// stops reporting drops off within it.
	telemetryActive = 15 * time.Minute
	// networkFresh is how long the numbers on the landing page stay cached.
	networkFresh = time.Minute
	// telemetryMax caps a count, so one report cannot claim the world.
	telemetryMax = 100_000
)

// TelemetryService counts what moin runs. A self-hosted control plane sends a
// few counts every five minutes to the hosted one, which adds them to its own for
// the landing page. Nothing else ever goes out: no name, domain or address.
type TelemetryService struct {
	repo    *repository.Telemetry
	url     string
	version string
	send    bool
	client  *http.Client
	now     func() time.Time
	// firstAfter is the wait before the first report after a start.
	firstAfter time.Duration

	mu       sync.Mutex
	network  model.Network
	cachedAt time.Time
}

// NewTelemetryService reports to url when send is true.
func NewTelemetryService(repo *repository.Telemetry, url, version string, send bool) *TelemetryService {
	return &TelemetryService{
		repo: repo, url: url, version: version, send: send,
		client: &http.Client{Timeout: 10 * time.Second}, now: time.Now, firstAfter: 10 * time.Second,
	}
}

// Run reports soon after the start, then every five minutes, until ctx ends.
func (s *TelemetryService) Run(ctx context.Context) {
	if !s.send {
		return
	}
	slog.Info("telemetry on: every five minutes this control plane sends its instance ID, version and the number of servers, apps and deploys; TELEMETRY=false turns it off", "to", s.url)
	wait := s.firstAfter
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err := s.Report(ctx); err != nil {
			slog.Warn("telemetry report failed", "error", err)
		}
		wait = telemetryEvery
	}
}

// Report sends the counts of this control plane once.
func (s *TelemetryService) Report(ctx context.Context) error {
	id, err := s.repo.InstanceID()
	if err != nil {
		return err
	}
	counts, err := s.repo.Counts()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(model.TelemetryReport{InstanceID: id, Version: s.version, Counts: counts})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("telemetry: %s", response.Status)
	}
	return nil
}

// Receive stores the report of a self-hosted control plane.
func (s *TelemetryService) Receive(report model.TelemetryReport) error {
	if _, err := uuid.Parse(report.InstanceID); err != nil {
		return ErrTelemetry
	}
	limit := func(count int) int { return min(max(count, 0), telemetryMax) }
	report.Version = clipText(report.Version, 32)
	report.Servers, report.Apps, report.Deploys = limit(report.Servers), limit(report.Apps), limit(report.Deploys)
	report.SeenAt = s.now().UTC()
	return s.repo.Save(report)
}

// ErrTelemetry is a report that is not one.
var ErrTelemetry = fmt.Errorf("not a telemetry report")

// Network is what moin runs right now, hosted and self-hosted, cached for a
// few minutes.
func (s *TelemetryService) Network() model.Network {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cachedAt.IsZero() && s.now().Sub(s.cachedAt) < networkFresh {
		return s.network
	}
	hosted, err := s.repo.Counts()
	if err != nil {
		slog.Error("count hosted", "error", err)
		return s.network
	}
	workspaces, err := s.repo.HostedWorkspaces()
	if err != nil {
		slog.Error("count hosted workspaces", "error", err)
		return s.network
	}
	instances, selfHosted, err := s.repo.SelfHosted(s.now().Add(-telemetryActive).UTC())
	if err != nil {
		slog.Error("count self-hosted", "error", err)
		return s.network
	}
	s.network = model.Network{
		SelfHosted:       instances,
		HostedWorkspaces: workspaces,
		Counts: model.Counts{
			Servers: hosted.Servers + selfHosted.Servers,
			Apps:    hosted.Apps + selfHosted.Apps,
			Deploys: hosted.Deploys + selfHosted.Deploys,
		},
	}
	s.cachedAt = s.now()
	return s.network
}
