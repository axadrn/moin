package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
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
// the stream ends, which is when the client disconnects. The stream of a
// connection or CI token ends when its access token does, at the latest in
// an hour, with the event reconnect: the client comes back with a fresh
// token, so a removal or a change of rights counts within the hour.
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
		bearer := ctxkeys.Bearer(r.Context())
		if bearer == nil {
			next(w, r)
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), StreamDeadline(*bearer, time.Now()))
		defer cancel()
		next(w, r.WithContext(ctx))
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && r.Context().Err() == nil && w.Header().Get("Content-Type") == "text/event-stream" {
			_, _ = io.WriteString(w, "event: reconnect\ndata: {}\n\n")
			_ = http.NewResponseController(w).Flush()
		}
	}
}

// StreamDeadline is when the stream of a connection or CI token ends: when
// its access token does, and at the latest an hour from now.
func StreamDeadline(bearer model.Bearer, now time.Time) time.Time {
	deadline := now.Add(model.AccessTokenLifetime)
	if bearer.ExpiresAt != nil && bearer.ExpiresAt.Before(deadline) {
		deadline = *bearer.ExpiresAt
	}
	return deadline
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
