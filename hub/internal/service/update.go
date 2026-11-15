package service

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/markdown"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/build"
)

const (
	// ReleasesURL is where a self-hosted hub learns of the newest
	// release. GitHub answers it without a token.
	ReleasesURL = "https://api.github.com/repos/axadrn/moin/releases/latest"
	// updateCheckEvery is how often it asks.
	updateCheckEvery = 24 * time.Hour
	// updaterAlive is how old the updater's heartbeat may be for "Update
	// now" to show; it writes one every few seconds.
	updaterAlive = time.Minute
	// updateShown is how long the end of an update stays on the notice.
	updateShown = 24 * time.Hour
	// notesMax caps the release notes kept in memory.
	notesMax = 20_000
)

// The states of an update of the hub, as the updater writes them.
const (
	UpdateRequested  = "requested"
	UpdateRunning    = "updating"
	UpdateDone       = "done"
	UpdateRolledBack = "rolled_back"
	UpdateFailed     = "failed"
)

var (
	// ErrUpdateNotAllowed is an update asked by someone who does not run
	// this hub: on a self-hosted one the admins in ADMIN_EMAILS.
	ErrUpdateNotAllowed = errors.New("only the admins of this hub update it")
	// ErrUpdateHosted is an update asked of the hosted hub, which
	// updates with every release by itself.
	ErrUpdateHosted = errors.New("the hosted hub updates itself with every release")
	// ErrNoUpdate means this hub runs the newest release already,
	// or knows of none.
	ErrNoUpdate = errors.New("this hub runs the newest release")
	// ErrUpdaterMissing means the updater service next to the hub
	// does not run, so nothing would pick the update up.
	ErrUpdaterMissing = errors.New("the updater does not run next to this hub")
	// ErrUpdateRunning means an update is already under way.
	ErrUpdateRunning = errors.New("an update is already under way")
)

// UpdateService tells the admins of a self-hosted hub when a new
// release is out and has the updater next to it install one. It asks GitHub
// once a day. The hub never touches Docker: it writes a request
// file into its data directory, which the updater service of compose.yml
// picks up, and reads the status file the updater writes back.
type UpdateService struct {
	version    string
	url        string
	selfHosted bool
	check      bool
	dir        string
	admins     map[string]struct{}
	activity   *repository.Activity
	client     *http.Client
	now        func() time.Time

	mu        sync.Mutex
	release   *model.Release
	etag      string
	checkedAt time.Time
}

// NewUpdateService checks url for releases when check is on. dir is where
// the request, status and heartbeat files of the updater live. selfHosted
// is false on the hosted hub, which shows no update at all.
func NewUpdateService(version, url, dir string, selfHosted, check bool, adminEmails []string, activity *repository.Activity) *UpdateService {
	admins := make(map[string]struct{}, len(adminEmails))
	for _, address := range adminEmails {
		admins[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	return &UpdateService{
		version: version, url: url, dir: dir, selfHosted: selfHosted, check: check && selfHosted,
		admins: admins, activity: activity,
		client: &http.Client{Timeout: 10 * time.Second}, now: time.Now,
	}
}

// Run asks GitHub at the start and then once a day, and notes in the
// activity how an update ended, until ctx ends.
func (s *UpdateService) Run(ctx context.Context) {
	if !s.selfHosted {
		return
	}
	if s.check {
		slog.Info("update check on: once a day this hub asks GitHub for the newest release; UPDATE_CHECK=false turns it off", "url", s.url)
	}
	nextCheck := s.now()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		s.Settle()
		if s.check && !s.now().Before(nextCheck) {
			if err := s.Check(ctx); err != nil {
				slog.Warn("update check failed", "error", err)
			}
			nextCheck = s.now().Add(updateCheckEvery)
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
	if found.Draft || found.Prerelease || parseVersion(found.TagName) == nil {
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

// CanUpdate reports whether user runs this hub: on a self-hosted
// one, an admin of ADMIN_EMAILS with a verified address.
func (s *UpdateService) CanUpdate(user *model.User) bool {
	if !s.selfHosted || user == nil || user.EmailVerifiedAt == nil {
		return false
	}
	_, ok := s.admins[strings.ToLower(user.Email)]
	return ok
}

// Info is what user may know about the version: the running one for
// everyone, and on a self-hosted hub the newest release, how to
// update and how the last update went.
func (s *UpdateService) Info(user *model.User) model.VersionInfo {
	info := model.VersionInfo{Version: s.version, SelfHosted: s.selfHosted}
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
		info.UpdateAvailable = NewerVersion(release.Version, s.version)
	}
	info.Admin = s.CanUpdate(user)
	info.Updater = s.updaterAlive()
	if state := s.state(); state != nil && (state.Running() || s.now().Sub(state.At) < updateShown) {
		info.Update = state
	}
	info.CanUpdate = info.Admin && info.Updater && info.UpdateAvailable && (info.Update == nil || !info.Update.Running())
	return info
}

// Notes is what an admin reads before an update, the release notes among
// it, with the same answer Start would give when it cannot update now.
func (s *UpdateService) Notes(user *model.User) (model.VersionInfo, error) {
	if !s.selfHosted {
		return model.VersionInfo{}, ErrUpdateHosted
	}
	if !s.CanUpdate(user) {
		return model.VersionInfo{}, ErrUpdateNotAllowed
	}
	info := s.Info(user)
	switch {
	case info.Update != nil && info.Update.Running():
		return info, ErrUpdateRunning
	case !info.UpdateAvailable:
		return info, ErrNoUpdate
	case !info.Updater:
		return info, ErrUpdaterMissing
	}
	return info, nil
}

// Start asks the updater to install the newest release, for an admin.
func (s *UpdateService) Start(actor Actor, user *model.User) (model.VersionInfo, error) {
	info, err := s.Notes(user)
	if err != nil {
		return info, err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return info, err
	}
	at := s.now().UTC()
	lines := []string{
		"version=" + info.Latest,
		"from=" + s.version,
		"workspace=" + actor.WorkspaceID,
		"user=" + actor.UserID,
		"via=" + actor.Via,
		"token=" + oneLine(actor.Token),
		"at=" + at.Format(time.RFC3339),
	}
	if err := writeFileAtomic(filepath.Join(s.dir, "request"), []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		return info, err
	}
	recordActivity(s.activity, at, actor, "hub.update_started", "hub", info.Latest, map[string]string{"from": s.version, "to": info.Latest})
	return s.Info(user), nil
}

// Settle notes in the activity how a finished update ended, once: in the
// workspace of the admin who started it.
func (s *UpdateService) Settle() {
	finished := filepath.Join(s.dir, "finished")
	request, err := readKeyValues(finished)
	if err != nil {
		return
	}
	state := s.state()
	if state == nil || state.Running() {
		return
	}
	if err := os.Remove(finished); err != nil {
		slog.Warn("settle the update", "error", err)
		return
	}
	actor := Actor{WorkspaceID: request["workspace"], UserID: request["user"], Source: Source{Via: request["via"], Token: request["token"]}}
	details := map[string]string{"from": state.From, "to": state.Version}
	if state.At.IsZero() {
		state.At = s.now().UTC()
	}
	switch state.State {
	case UpdateDone:
		recordActivity(s.activity, state.At, actor, "hub.updated", "hub", state.Version, details)
	case UpdateRolledBack:
		details["reason"] = clipText(state.Reason, 300)
		recordActivity(s.activity, state.At, actor, "hub.rolled_back", "hub", state.Version, details)
	default:
		details["reason"] = clipText(state.Reason, 300)
		recordActivity(s.activity, state.At, actor, "hub.update_failed", "hub", state.Version, details)
	}
}

// state is the update under way or the last one: a request the updater has
// not picked up yet, or the status it wrote.
func (s *UpdateService) state() *model.UpdateState {
	if request, err := readKeyValues(filepath.Join(s.dir, "request")); err == nil {
		at, _ := time.Parse(time.RFC3339, request["at"])
		return &model.UpdateState{State: UpdateRequested, Version: request["version"], From: request["from"], At: at}
	}
	status, err := readKeyValues(filepath.Join(s.dir, "status"))
	if err != nil {
		return nil
	}
	at, _ := time.Parse(time.RFC3339, status["at"])
	return &model.UpdateState{State: status["state"], Version: status["version"], From: status["from"], Reason: status["reason"], At: at}
}

// updaterAlive reports whether the updater wrote its heartbeat lately.
func (s *UpdateService) updaterAlive() bool {
	info, err := os.Stat(filepath.Join(s.dir, "updater"))
	return err == nil && s.now().Sub(info.ModTime()) < updaterAlive
}

// readKeyValues reads a file of key=value lines.
func readKeyValues(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(io.LimitReader(file, 64<<10))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values, scanner.Err()
}

// writeFileAtomic writes a file the updater never sees half written.
func writeFileAtomic(path string, body []byte) error {
	temporary := path + ".new"
	if err := os.WriteFile(temporary, body, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func oneLine(value string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(value)
}

// NewerVersion reports whether candidate is a newer release than current,
// both like v1.2.3 or v1.0.0-beta.3, by the rules of semantic versioning.
// Anything else, such as "dev", is never newer and never older.
func NewerVersion(candidate, current string) bool {
	a, b := parseVersion(candidate), parseVersion(current)
	if a == nil || b == nil {
		return false
	}
	return compareVersions(a, b) > 0
}

type semver struct {
	core       [3]int
	prerelease []string
}

func parseVersion(value string) *semver {
	value, ok := strings.CutPrefix(strings.TrimSpace(value), "v")
	if !ok {
		return nil
	}
	value, _, _ = strings.Cut(value, "+")
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nil
	}
	var version semver
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || part == "" || (len(part) > 1 && part[0] == '0') {
			return nil
		}
		version.core[index] = number
	}
	if hasPrerelease {
		version.prerelease = strings.Split(prerelease, ".")
		for _, identifier := range version.prerelease {
			if identifier == "" {
				return nil
			}
		}
	}
	return &version
}

func compareVersions(a, b *semver) int {
	for index := range a.core {
		if a.core[index] != b.core[index] {
			return cmp.Compare(a.core[index], b.core[index])
		}
	}
	// A release is newer than its prereleases.
	switch {
	case len(a.prerelease) == 0 && len(b.prerelease) == 0:
		return 0
	case len(a.prerelease) == 0:
		return 1
	case len(b.prerelease) == 0:
		return -1
	}
	for index := 0; index < len(a.prerelease) && index < len(b.prerelease); index++ {
		x, y := a.prerelease[index], b.prerelease[index]
		xNumber, xErr := strconv.Atoi(x)
		yNumber, yErr := strconv.Atoi(y)
		switch {
		case xErr == nil && yErr == nil:
			if xNumber != yNumber {
				return cmp.Compare(xNumber, yNumber)
			}
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return cmp.Compare(len(a.prerelease), len(b.prerelease))
}
