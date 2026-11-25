package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// bearerLimit is the bearer limit on a clock the test moves by hand.
type bearerLimit struct {
	limiter *RateLimiter
	handler http.Handler
	now     time.Time
}

func newBearerLimit(t *testing.T, next http.Handler) *bearerLimit {
	t.Helper()
	limit := &bearerLimit{
		limiter: NewRateLimiter(APIRequestsPerMinute, time.Minute),
		now:     time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
	}
	t.Cleanup(limit.limiter.Close)
	limit.limiter.now = func() time.Time { return limit.now }
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	limit.handler = rateLimitBearers(limit.limiter)(next)
	return limit
}

// send sends one request as the bearer, or as a browser session when
// bearer is nil.
func (limit *bearerLimit) send(bearer *model.Bearer) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/apps", nil)
	ctx := ctxkeys.WithUser(request.Context(), &model.User{ID: "user-1"})
	if bearer != nil {
		ctx = ctxkeys.WithBearer(ctx, bearer)
	}
	recorder := httptest.NewRecorder()
	limit.handler.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

// sendAll sends n requests and fails the test when one is refused.
func (limit *bearerLimit) sendAll(t *testing.T, bearer *model.Bearer, n int) {
	t.Helper()
	for i := range n {
		if recorder := limit.send(bearer); recorder.Code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d, want 204", i+1, recorder.Code)
		}
	}
}

func connection(id string) *model.Bearer {
	return &model.Bearer{UserID: "user-1", ConnectionID: id}
}

func ciToken(id string) *model.Bearer {
	return &model.Bearer{UserID: "user-1", CITokenID: id}
}

func TestBearerLimitRefusesTheRequestAfter121(t *testing.T) {
	limit := newBearerLimit(t, nil)
	limit.sendAll(t, connection("connection-1"), APIRequestsPerMinute)

	recorder := limit.send(connection("connection-1"))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("request 122: status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After = %q, want 60", got)
	}
	detail := decodeAPIError(t, recorder)
	want := "Too many requests: 121 a minute per connection or CI token. Try again in 60 seconds."
	if detail.Code != "rate_limited" || detail.Message != want {
		t.Fatalf("error = %q %q, want rate_limited %q", detail.Code, detail.Message, want)
	}
}

func TestBearerLimitSaysHowLongToWait(t *testing.T) {
	limit := newBearerLimit(t, nil)
	limit.sendAll(t, ciToken("ci-token-1"), APIRequestsPerMinute)

	limit.now = limit.now.Add(20*time.Second + 500*time.Millisecond)
	recorder := limit.send(ciToken("ci-token-1"))
	if got := recorder.Header().Get("Retry-After"); got != "40" {
		t.Fatalf("Retry-After = %q, want 40", got)
	}
	limit.now = limit.now.Add(39 * time.Second)
	recorder = limit.send(ciToken("ci-token-1"))
	detail := decodeAPIError(t, recorder)
	if got := recorder.Header().Get("Retry-After"); got != "1" || detail.Message != "Too many requests: 121 a minute per connection or CI token. Try again in 1 second." {
		t.Fatalf("Retry-After = %q, message = %q", got, detail.Message)
	}
}

func TestBearerLimitKeepsEachKeyApart(t *testing.T) {
	limit := newBearerLimit(t, nil)
	limit.sendAll(t, connection("same-id"), APIRequestsPerMinute)

	if recorder := limit.send(connection("same-id")); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("full connection: status = %d, want 429", recorder.Code)
	}
	limit.sendAll(t, connection("connection-2"), APIRequestsPerMinute)
	// A CI token with the same ID as a connection keeps its own count.
	limit.sendAll(t, ciToken("same-id"), APIRequestsPerMinute)
}

func TestBearerLimitStartsAgainAfterAMinute(t *testing.T) {
	limit := newBearerLimit(t, nil)
	limit.sendAll(t, connection("connection-1"), APIRequestsPerMinute)

	limit.now = limit.now.Add(time.Minute)
	limit.sendAll(t, connection("connection-1"), APIRequestsPerMinute)
	if recorder := limit.send(connection("connection-1")); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
}

func TestBearerLimitCountsAStreamOnce(t *testing.T) {
	stream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := range 500 {
			fmt.Fprintf(w, "event: status\ndata: %d\n\n", i)
			_ = http.NewResponseController(w).Flush()
		}
	})
	limit := newBearerLimit(t, stream)

	if recorder := limit.send(connection("connection-1")); recorder.Code != http.StatusOK {
		t.Fatalf("stream: status = %d, want 200", recorder.Code)
	}
	for i := range APIRequestsPerMinute - 1 {
		if recorder := limit.send(connection("connection-1")); recorder.Code != http.StatusOK {
			t.Fatalf("request %d after the stream: status = %d, want 200", i+1, recorder.Code)
		}
	}
	if recorder := limit.send(connection("connection-1")); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
}

func TestBearerLimitLeavesBrowserSessionsAlone(t *testing.T) {
	limit := newBearerLimit(t, nil)
	limit.sendAll(t, nil, 3*APIRequestsPerMinute)
}

func TestBearerLimitForgetsQuietKeys(t *testing.T) {
	limit := newBearerLimit(t, nil)
	limit.sendAll(t, connection("connection-1"), 3)
	limit.sendAll(t, ciToken("ci-token-1"), 3)

	limit.now = limit.now.Add(time.Minute)
	limit.sendAll(t, ciToken("ci-token-1"), 1)
	limit.now = limit.now.Add(90 * time.Second)
	limit.limiter.cleanup()

	limit.limiter.mu.RLock()
	defer limit.limiter.mu.RUnlock()
	if _, kept := limit.limiter.requests["connection:connection-1"]; kept {
		t.Fatal("cleanup kept a connection quiet for two minutes")
	}
	if _, kept := limit.limiter.requests["ci-token:ci-token-1"]; !kept {
		t.Fatal("cleanup dropped a CI token used within two minutes")
	}
}

// Through the real sign-in middleware: a CI token on /mcp is limited, a
// browser session on the API is not.
func TestBearerLimitBehindBearerAuth(t *testing.T) {
	environment := newBearerTestEnvironment(t)
	fixture := testutil.SeedUser(t, environment.database, testutil.UserOptions{ID: "limited-user", Name: "Limited User", Verified: true})
	_, token, err := environment.ciTokens.Create(service.Actor{UserID: fixture.User.ID, WorkspaceID: fixture.Workspace.ID}, service.NewCIToken{Name: "Script"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cookie := startCookieSession(t, environment.auth, fixture.User.ID)
	limiter := NewRateLimiter(APIRequestsPerMinute, time.Minute)
	t.Cleanup(limiter.Close)
	handler := environment.authenticate(environment.tokenAuth(rateLimitBearers(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ctxkeys.User(r.Context()) == nil {
			t.Fatal("handler reached without a user")
		}
		w.WriteHeader(http.StatusNoContent)
	}))))
	send := func(path string, sign func(*http.Request)) int {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		sign(request)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	withToken := func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+token) }
	withCookie := func(request *http.Request) { request.AddCookie(cookie) }

	for i := range APIRequestsPerMinute {
		if code := send("/mcp", withToken); code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d, want 204", i+1, code)
		}
	}
	if code := send("/api/v1/apps", withToken); code != http.StatusTooManyRequests {
		t.Fatalf("request 122: status = %d, want 429", code)
	}
	for i := range APIRequestsPerMinute + 1 {
		if code := send("/api/v1/apps", withCookie); code != http.StatusNoContent {
			t.Fatalf("session request %d: status = %d, want 204", i+1, code)
		}
	}
}
