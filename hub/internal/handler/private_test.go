package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/testutil"
)

func TestHomeIsTheLandingPageOnlyWithBilling(t *testing.T) {
	telemetry := service.NewTelemetryService(repository.NewTelemetry(testutil.NewDatabase(t)), "", "test", false)
	for _, test := range []struct {
		name     string
		billing  bool
		signedIn bool
		location string
	}{
		{name: "hosted", billing: true},
		{name: "self-hosted guest", location: "/auth"},
		{name: "self-hosted signed in", signedIn: true, location: "/app"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{Billing: test.billing})
			if test.signedIn {
				ctx = ctxkeys.WithUser(ctx, &model.User{ID: "user", Email: "user@example.com"})
			}
			recorder := httptest.NewRecorder()

			NewHomeHandler(telemetry, test.billing, os.DirFS(t.TempDir())).HomePage(recorder, request.WithContext(ctx))

			if test.location == "" {
				if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<html") {
					t.Fatalf("status = %d, want the landing page", recorder.Code)
				}
				return
			}
			if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != test.location {
				t.Fatalf("status = %d, location = %q, want %s", recorder.Code, recorder.Header().Get("Location"), test.location)
			}
		})
	}
}
