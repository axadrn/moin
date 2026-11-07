package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

func TestLogoutPreservesCookieWhenRevocationFails(t *testing.T) {
	// A closed database fails every query, like a database that is down.
	database := testutil.NewDatabase(t)
	sessionRepository := repository.NewSessions(database)
	_ = database.Close()
	authService := service.NewAuthService(
		nil, nil, sessionRepository, nil, nil,
		false, time.Hour, time.Hour, time.Hour, time.Hour, time.Hour,
	)
	handler := &authHandler{authService: authService}
	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: "session_token", Value: "valid-token"})
	recorder := httptest.NewRecorder()

	handler.Logout(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("logout cleared the cookie after revocation failed")
	}
}
