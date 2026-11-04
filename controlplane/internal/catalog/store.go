package catalog

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Cached is what the store keeps of an entry: its releases and when the
// registry gave them.
type Cached struct {
	Versions []Version
	Checked  time.Time
}

// Cache keeps the releases across restarts, in history.db. Load reads all
// of them, Save one entry. Losing them costs one read of each registry.
type Cache struct {
	Load func() (map[string]Cached, error)
	Save func(slug string, cached Cached) error
}

// Store holds the entries and the releases of each. It reads each entry's
// registry once an hour, spread over the hour, keeps them in memory and in
// its cache, and calls OnRefresh after each entry.
type Store struct {
	entries  []Entry
	registry Registry
	every    time.Duration
	cache    Cache
	// OnRefresh runs after an entry got new releases, such as to tell of
	// an update.
	OnRefresh func(slug string)

	mu       sync.RWMutex
	versions map[string][]Version
	checked  map[string]time.Time
}

// NewStore keeps entries and reads their releases from registry.
func NewStore(entries []Entry, registry Registry) *Store {
	return &Store{entries: entries, registry: registry, every: time.Hour, versions: map[string][]Version{}, checked: map[string]time.Time{}}
}

// UseCache keeps the releases in cache, and loads what it has now, so the
// pages show releases before the first registry answers.
func (s *Store) UseCache(cache Cache) {
	s.cache = cache
	if cache.Load == nil {
		return
	}
	cached, err := cache.Load()
	if err != nil {
		slog.Warn("catalog: read the cached releases failed", "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for slug, entry := range cached {
		s.versions[slug] = entry.Versions
		s.checked[slug] = entry.Checked
	}
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

// Run reads the releases until ctx ends. An entry the store never read,
// or read more than an hour ago, is read right away, a second apart; then
// each entry is read once an hour, spread over the hour, so the registries
// never get every request at once.
func (s *Store) Run(ctx context.Context) {
	if len(s.entries) == 0 {
		return
	}
	gap := s.every / time.Duration(len(s.entries))
	for first := true; ; first = false {
		for _, entry := range s.entries {
			wait := gap
			// Due a little early, so a pass that took a bit less than an
			// hour never skips an entry for a second hour.
			if time.Since(s.Checked(entry.Slug)) >= s.every-gap {
				s.refresh(ctx, entry)
				if first {
					// Catching up after a start: the others soon.
					wait = time.Second
				}
			} else if first {
				wait = 0
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}
}

// Refresh reads every entry's releases once. An entry whose registry does
// not answer keeps the releases it had.
func (s *Store) Refresh(ctx context.Context) {
	for _, entry := range s.entries {
		s.refresh(ctx, entry)
	}
}

func (s *Store) refresh(ctx context.Context, entry Entry) {
	tags, err := s.registry.Tags(ctx, entry.Image)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("catalog: registry did not answer", "app", entry.Slug, "error", err)
		}
		return
	}
	s.Set(entry.Slug, UpToChannel(Releases(tags, entry.Filter), tags, entry.Stable))
	if s.OnRefresh != nil {
		s.OnRefresh(entry.Slug)
	}
}

// Set gives slug its releases and keeps them in the cache.
func (s *Store) Set(slug string, versions []Version) {
	now := time.Now()
	s.mu.Lock()
	s.versions[slug] = versions
	s.checked[slug] = now
	s.mu.Unlock()
	if s.cache.Save != nil {
		if err := s.cache.Save(slug, Cached{Versions: versions, Checked: now}); err != nil {
			slog.Warn("catalog: keep the releases failed", "app", slug, "error", err)
		}
	}
}
