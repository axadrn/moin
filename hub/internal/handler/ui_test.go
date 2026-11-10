package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axadrn/moin"
	"github.com/axadrn/moin/hub/internal/service"
)

// TestUIPages walks the website of moin UI on the files of the binary: a doc
// that names a missing example fails here, when the docs are read.
func TestUIPages(t *testing.T) {
	files := moin.UI(false)
	docs := service.NewUIDocsService(files, "", false)
	if err := docs.BuildDocsTree(); err != nil {
		t.Fatalf("read the docs: %v", err)
	}
	ui := NewUIHandler(docs, files)
	tests := []struct {
		path   string
		status int
		want   string
	}{
		{"/ui", http.StatusOK, "Get started"},
		{"/ui.md", http.StatusOK, "## Get started"},
		{"/ui/components", http.StatusOK, `href="/ui/badge"`},
		{"/ui/layout", http.StatusOK, `href="/ui/grid-that-fits"`},
		{"/ui/badge", http.StatusOK, `<span class="moin-badge">Beta</span>`},
		{"/ui/badge.md", http.StatusOK, "```html\n"},
		{"/ui/grid-that-fits", http.StatusOK, "data-resize"},
		{"/ui/theme", http.StatusOK, "<moin-theme-editor"},
		{"/ui/blocks", http.StatusOK, `href="/ui/blocks/saas-billing"`},
		{"/ui/blocks/saas-billing", http.StatusOK, `src="/assets/ui/blocks/saas-billing.html"`},
		{"/ui/blocks/nothing", http.StatusNotFound, ""},
		{"/ui/nothing", http.StatusNotFound, ""},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		ui.Show(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.status {
			t.Errorf("%s: status %d, want %d", test.path, recorder.Code, test.status)
		}
		if !strings.Contains(recorder.Body.String(), test.want) {
			t.Errorf("%s: the page misses %q", test.path, test.want)
		}
	}
}
