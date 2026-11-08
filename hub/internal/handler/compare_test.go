package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

func serveCompare(path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /compare/{name}", Compare)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{AppURL: "https://example.com", Billing: true})
	ctx = ctxkeys.WithURLPath(ctx, request.URL.Path)
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

// TestComparePagesAnswer checks every competitor has its page and answers
// every question, each with a source.
func TestComparePagesAnswer(t *testing.T) {
	if len(pages.Competitors) != 7 {
		t.Fatalf("competitors = %d, want 7", len(pages.Competitors))
	}
	for _, competitor := range pages.Competitors {
		if len(competitor.Cells) != len(pages.CompareRows) {
			t.Errorf("%s answers %d of %d questions", competitor.Name, len(competitor.Cells), len(pages.CompareRows))
		}
		for i, cell := range competitor.Cells {
			if cell.Text == "" || !strings.HasPrefix(cell.Source, "https://") {
				t.Errorf("%s, %s: answer %q, source %q", competitor.Name, pages.CompareRows[i].Label, cell.Text, cell.Source)
			}
		}
		response := serveCompare("/compare/" + competitor.Slug)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), competitor.Name) {
			t.Errorf("/compare/%s: status %d", competitor.Slug, response.Code)
		}
		if strings.Contains(response.Body.String(), "—") {
			t.Errorf("/compare/%s has an em dash", competitor.Slug)
		}
	}
}

func TestCompareUnknownIsNotFound(t *testing.T) {
	if response := serveCompare("/compare/nothing"); response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}
