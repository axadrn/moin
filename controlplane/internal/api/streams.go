package api

import (
	"net/http"
	"sync"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
)

// maxStreamsPerUser caps the live-update streams one user holds open. A
// browser tab holds one or two, so 20 leaves room for many tabs while a
// runaway script cannot pin the server's connections.
const maxStreamsPerUser = 20

// streamLimit counts the open live-update streams per user.
type streamLimit struct {
	max  int
	mu   sync.Mutex
	open map[string]int
}

func newStreamLimit(limit int) *streamLimit {
	return &streamLimit{max: limit, open: map[string]int{}}
}

// streams is shared by every event route, so the cap counts them together.
var streams = newStreamLimit(maxStreamsPerUser)

// wrap answers 429 once the user holds max streams, and frees the slot when
// the stream ends, which is when the client disconnects.
func (l *streamLimit) wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := ctxkeys.User(r.Context())
		if user == nil {
			next(w, r)
			return
		}
		if !l.acquire(user.ID) {
			Error(w, http.StatusTooManyRequests, "rate_limited", "Too many live-update streams are open. Close some tabs or clients and try again.")
			return
		}
		defer l.release(user.ID)
		next(w, r)
	}
}

func (l *streamLimit) acquire(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[userID] >= l.max {
		return false
	}
	l.open[userID]++
	return true
}

func (l *streamLimit) release(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.open[userID]--
	if l.open[userID] <= 0 {
		delete(l.open, userID)
	}
}
