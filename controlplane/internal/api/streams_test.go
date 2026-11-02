package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
)

// TestStreamLimitPerUser: a user gets 429 past the cap, another user is not
// affected, and a closed stream frees its slot.
func TestStreamLimitPerUser(t *testing.T) {
	limit := newStreamLimit(2)
	started := make(chan struct{})
	stream := limit.wrap(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-r.Context().Done()
	})
	open := func(userID string) (context.CancelFunc, chan int) {
		ctx, cancel := context.WithCancel(ctxkeys.WithUser(context.Background(), &model.User{ID: userID}))
		request := httptest.NewRequest(http.MethodGet, "/api/v1/apps/events", nil).WithContext(ctx)
		status := make(chan int, 1)
		go func() {
			recorder := httptest.NewRecorder()
			stream(recorder, request)
			status <- recorder.Code
		}()
		return cancel, status
	}

	firstCancel, firstStatus := open("alice")
	<-started
	secondCancel, _ := open("alice")
	<-started
	defer secondCancel()

	_, third := open("alice")
	if code := <-third; code != http.StatusTooManyRequests {
		t.Fatalf("third stream: status %d, want 429", code)
	}
	bobCancel, _ := open("bob")
	<-started
	defer bobCancel()

	// Disconnecting frees the slot for the next stream.
	firstCancel()
	<-firstStatus
	againCancel, _ := open("alice")
	<-started
	defer againCancel()
}
