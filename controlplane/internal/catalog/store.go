package catalog

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Store holds the entries and the releases of each, which it asks the
// registries for once an hour. They live in memory: a restart asks again.
type Store struct {
	entries  []Entry
	registry Registry
	every    time.Duration

	mu       sync.RWMutex
	versions map[string][]Version
	checked  map[string]time.Time
}

// NewStore keeps entries and reads their releases from registry.
func NewStore(entries []Entry, registry Registry) *Store {
	return &Store{entries: entries, registry: registry, every: time.Hour, versions: map[string][]Version{}, checked: map[string]time.Time{}}
}

// Entries are all entries, sorted by name.
func (s *Store) Entries() []Entry { return slices.Clone(s.entries) }

// Entry is the entry with slug.
func (s *Store) Entry(slug string) (Entry, bool) {
	index := slices.IndexFunc(s.entries, func(entry Entry) bool { return entry.Slug == slug })
	if index < 0 {
		return Entry{}, false
	}
	return s.entries[index], true
}

// Versions are the releases of slug, newest first; nil before the first
// answer of its registry.
func (s *Store) Versions(slug string) []Version {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.versions[slug])
}

// Latest is the newest release of slug.
func (s *Store) Latest(slug string) (Version, bool) {
	versions := s.Versions(slug)
	if len(versions) == 0 {
		return Version{}, false
	}
	return versions[0], true
}

// Checked is when the registry of slug last answered.
func (s *Store) Checked(slug string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checked[slug]
}

// Run reads every entry's releases now and then once an hour, until ctx
// ends.
func (s *Store) Run(ctx context.Context) {
	for {
		s.Refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.every):
		}
	}
}

// Refresh reads every entry's releases once. An entry whose registry does
// not answer keeps the releases it had.
func (s *Store) Refresh(ctx context.Context) {
	for _, entry := range s.entries {
		tags, err := s.registry.Tags(ctx, entry.Image)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("catalog: registry did not answer", "app", entry.Slug, "error", err)
			}
			continue
		}
		releases := Releases(tags, entry.Filter)
		s.mu.Lock()
		s.versions[entry.Slug] = releases
		s.checked[entry.Slug] = time.Now()
		s.mu.Unlock()
	}
}

// Set gives slug its releases, for tests.
func (s *Store) Set(slug string, versions []Version) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.versions[slug] = versions
	s.checked[slug] = time.Now()
}
