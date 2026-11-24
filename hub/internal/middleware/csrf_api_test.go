package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFProtectionSkipsRequestsWithAuthorization(t *testing.T) {
	called := false
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.Header.Set("Authorization", "Bearer token")
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: generateCSRFToken()})
	recorder := httptest.NewRecorder()

	CSRFProtection(false, NewClientIPResolver(nil))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)

	if !called || recorder.Code != http.StatusNoContent {
		t.Fatalf("called = %t, status = %d", called, recorder.Code)
	}
}

func TestCSRFProtectionChecksOtherAuthorizationSchemes(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/logout", "/auth/logout"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		request.AddCookie(&http.Cookie{Name: "session_token", Value: "session"})
		request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: generateCSRFToken()})
		recorder := httptest.NewRecorder()

		CSRFProtection(false, NewClientIPResolver(nil))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("%s: handler was called without a CSRF token", path)
		})).ServeHTTP(recorder, request)

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403", path, recorder.Code)
		}
	}
}

func TestCSRFProtectionRejectsAPICookieRequestWithJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: "session_token", Value: "session"})
	request.AddCookie(&http.Cookie{Name: csrfCookieName, Value: generateCSRFToken()})
	recorder := httptest.NewRecorder()

	CSRFProtection(false, NewClientIPResolver(nil))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	detail := decodeAPIError(t, recorder)
	if detail.Code != "forbidden" {
		t.Fatalf("code = %q, want forbidden", detail.Code)
	}
	if detail.Message != "Send the X-CSRF-Token header with cookie requests." {
		t.Fatalf("message = %q", detail.Message)
	}
}

func TestCSRFProtectionSkipsAPIRequestsWithoutSessionCookie(t *testing.T) {
	called := false
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", nil)
	recorder := httptest.NewRecorder()

	CSRFProtection(false, NewClientIPResolver(nil))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)

	if !called || recorder.Code != http.StatusNoContent {
		t.Fatalf("called = %t, status = %d", called, recorder.Code)
	}
}

func TestCSRFProtectionKeepsCheckingHTMLRequestsWithoutSessionCookie(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/auth/key", nil)
	recorder := httptest.NewRecorder()

	CSRFProtection(false, NewClientIPResolver(nil))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("protected handler was called")
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestRequestEmailReadsJSONAndRestoresBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/account/email", strings.NewReader(`{"email":" Ada@Example.com "}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")

	email := requestEmail(request)
	if email != "ada@example.com" {
		t.Fatalf("requestEmail() = %q", email)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != `{"email":" Ada@Example.com "}` {
		t.Fatalf("restored body = %q", body)
	}
}

func TestRequestEmailReadsForms(t *testing.T) {
	request := httptest.NewRequest(http.MethodPatch, "/app/workspace/account/email", strings.NewReader("email=Ada%40Example.com"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	email := requestEmail(request)
	if email != "ada@example.com" {
		t.Fatalf("requestEmail() = %q", email)
	}
}
