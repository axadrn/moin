package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/compare"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
)

func serveCompare(path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /compare/{name}", Compare)
	request := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{AppURL: "https://example.com"})
	ctx = ctxkeys.WithURLPath(ctx, request.URL.Path)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request.WithContext(ctx))
	return recorder
}

// TestComparePagesAnswer checks every competitor has its page and answers
// every question, each with a source.
func TestComparePagesAnswer(t *testing.T) {
	if len(compare.Competitors) == 0 {
		t.Fatal("no competitors")
	}
	for _, competitor := range compare.Competitors {
		if _, err := time.Parse("January 2, 2006", competitor.Read); err != nil {
			t.Errorf("%s: read %q, want a date", competitor.Name, competitor.Read)
		}
		if len(competitor.Cells) != len(compare.Rows) {
			t.Errorf("%s answers %d of %d questions", competitor.Name, len(competitor.Cells), len(compare.Rows))
		}
		for i, cell := range competitor.Cells {
			if cell.Short == "" || !strings.HasPrefix(cell.Source, "https://") {
				t.Errorf("%s, %s: answer %q, source %q", competitor.Name, compare.Rows[i].Label, cell.Short, cell.Source)
			}
		}
		response := serveCompare("/compare/" + competitor.Slug)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), competitor.Name) {
			t.Errorf("/compare/%s: status %d", competitor.Slug, response.Code)
		}
		body := response.Body.String()
		if strings.Contains(body, "—") {
			t.Errorf("/compare/%s has an em dash", competitor.Slug)
		}
		if !strings.Contains(body, competitor.ScoreLine()) {
			t.Errorf("/compare/%s does not say %q", competitor.Slug, competitor.ScoreLine())
		}
		for _, gone := range []string{"Pick " + competitor.Name, "not the only one", "Where others are ahead"} {
			if strings.Contains(body, gone) {
				t.Errorf("/compare/%s still says %q", competitor.Slug, gone)
			}
		}
		if score := competitor.Score(); score.Facts == 0 || score.Wins > score.Facts || score.Ours != score.Facts {
			t.Errorf("/compare/%s: score %+v", competitor.Slug, score)
		}
	}
}

func TestCompareUnknownIsNotFound(t *testing.T) {
	if response := serveCompare("/compare/nothing"); response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}
