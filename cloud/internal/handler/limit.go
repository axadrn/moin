package handler

import (
	"sync"
	"time"
)

// limit allows at most n of something per key in a window of time, such
// as three sign-in mails per address in 15 minutes.
type limit struct {
	n      int
	window time.Duration

	mu   sync.Mutex
	seen map[string][]time.Time
}

func newLimit(n int, window time.Duration) *limit {
	return &limit{n: n, window: window, seen: map[string][]time.Time{}}
}

// allow counts one for key at now and says whether it is within the limit.
func (l *limit) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.seen[key][:0]
	for _, at := range l.seen[key] {
		if now.Sub(at) < l.window {
			recent = append(recent, at)
		}
	}
	if len(recent) >= l.n {
		l.seen[key] = recent
		return false
	}
	l.seen[key] = append(recent, now)
	// Forget keys that went quiet, so the map does not grow forever.
	if len(l.seen) > 10000 {
		for key, times := range l.seen {
			if len(times) == 0 || now.Sub(times[len(times)-1]) >= l.window {
				delete(l.seen, key)
			}
		}
	}
	return true
}
