package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/dbbackup"
	"github.com/axadrn/moin/hub/internal/markdown"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/internal/build"
)

const (
	// ReleasesURL is where a self-hosted hub learns of the newest
	// release. GitHub answers it without a token.
	ReleasesURL = "https://api.github.com/repos/axadrn/moin/releases/latest"
	// updateCheckEvery is how often it asks.
	updateCheckEvery = 24 * time.Hour
	// notesMax caps the release notes kept in memory.
	notesMax = 20_000
)

// UpdateService tells the admins of a self-hosted hub when a new
// release is out. It asks GitHub once a day. The hub never updates
// itself: its admins run update.sh on the server.
type UpdateService struct {
	version    string
	url        string
	selfHosted bool
	check      bool
	admins     HubAdmins
	client     *http.Client
	now        func() time.Time

	// hubBackup is the backup of the hub's own database, nil when off.
	hubBackup *dbbackup.Backup

	mu        sync.Mutex
	release   *model.Release
	etag      string
	checkedAt time.Time
}

// NewUpdateService checks url for releases when check is on. selfHosted
// is false on the hosted hub, which shows no update at all.
func NewUpdateService(version, url string, selfHosted, check bool, adminEmails []string) *UpdateService {
	return &UpdateService{
		version: version, url: url, selfHosted: selfHosted, check: check && selfHosted,
		admins: NewHubAdmins(adminEmails),
		client: &http.Client{Timeout: 10 * time.Second}, now: time.Now,
	}
}

// Run asks GitHub at the start and then once a day, until ctx ends.
func (s *UpdateService) Run(ctx context.Context) {
	if !s.check {
		return
	}
	slog.Info("update check on: once a day this hub asks GitHub for the newest release; UPDATE_CHECK=false turns it off", "url", s.url)
	ticker := time.NewTicker(updateCheckEvery)
	defer ticker.Stop()
	for {
		if err := s.Check(ctx); err != nil {
			slog.Warn("update check failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// githubRelease is the part of GitHub's release that moin reads.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

// Check asks GitHub for the newest release once and keeps it.
func (s *UpdateService) Check(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", build.Name+"-hub/"+s.version)
	s.mu.Lock()
	if s.etag != "" {
		request.Header.Set("If-None-Match", s.etag)
	}
	s.mu.Unlock()
	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		s.mu.Lock()
		s.checkedAt = s.now().UTC()
		s.mu.Unlock()
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("releases: %s", response.Status)
	}
	var found githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&found); err != nil {
		return fmt.Errorf("releases: %w", err)
	}
	if found.Draft || found.Prerelease || !build.IsRelease(found.TagName) {
		return fmt.Errorf("releases: %q is no release", found.TagName)
	}
	notes := strings.TrimSpace(found.Body)
	if len(notes) > notesMax {
		notes = notes[:notesMax]
	}
	html, err := markdown.NewParser().Parse([]byte(notes))
	if err != nil {
		return fmt.Errorf("releases: %w", err)
	}
	release := &model.Release{
		Version: found.TagName, URL: found.HTMLURL, PublishedAt: found.PublishedAt.UTC(),
		Summary: releaseSummary(notes), Notes: notes, NotesHTML: string(html),
	}
	s.mu.Lock()
	s.release, s.etag, s.checkedAt = release, response.Header.Get("ETag"), s.now().UTC()
	s.mu.Unlock()
	return nil
}

// releaseSummary is the "In short" line of release notes, without its label.
func releaseSummary(notes string) string {
	for line := range strings.SplitSeq(notes, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "**In short:**"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// UseHubBackup shows how the backup of the hub's own database stands. It
// is set before the hub serves; without it the backup is off.
func (s *UpdateService) UseHubBackup(backup *dbbackup.Backup) {
	s.hubBackup = backup
}

// HubBackup is how the backup of the hub's own database stands: off, on,
// or late when the bucket took no copy for model.HubBackupLateAfter.
func (s *UpdateService) HubBackup() model.HubBackup {
	if s.hubBackup == nil {
		return model.HubBackup{State: model.HubBackupOff}
	}
	backup := model.HubBackup{State: model.HubBackupOn, Bucket: s.hubBackup.Bucket}
	since := s.hubBackup.Started
	if last := s.hubBackup.LastCopy(); !last.IsZero() {
		last = last.UTC()
		backup.LastCopy, since = &last, last
	}
	if s.now().Sub(since) > model.HubBackupLateAfter {
		backup.State = model.HubBackupLate
	}
	return backup
}

// Info is what user may know about the version: the running one for
// everyone, and on a self-hosted hub the newest release.
func (s *UpdateService) Info(user *model.User) model.VersionInfo {
	info := model.VersionInfo{Version: s.version, SelfHosted: s.selfHosted}
	if s.admins.Has(user) {
		backup := s.HubBackup()
		info.HubBackup = &backup
	}
	if !s.selfHosted {
		return info
	}
	s.mu.Lock()
	release, checkedAt := s.release, s.checkedAt
	s.mu.Unlock()
	if !checkedAt.IsZero() {
		info.CheckedAt = &checkedAt
	}
	if release != nil {
		published := release.PublishedAt
		info.Latest, info.NotesURL, info.Summary, info.Notes, info.NotesHTML = release.Version, release.URL, release.Summary, release.Notes, release.NotesHTML
		if !published.IsZero() {
			info.PublishedAt = &published
		}
		info.UpdateAvailable = build.NewerVersion(release.Version, s.version)
	}
	info.Admin = s.admins.Has(user)
	return info
}
