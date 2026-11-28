package handler

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/cloud/assets"
	"github.com/axadrn/moin/cloud/internal/brand"
)

// TestBrandPage: /brand answers with every section, the mark it draws is
// the one in assets/img/mark.svg, and every file it offers is there.
func TestBrandPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	Brand(recorder, httptest.NewRequest(http.MethodGet, "/brand", nil))
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	for _, want := range []string{`id="logo"`, `id="color"`, `id="type"`, `id="kit"`, `id="voice"`, brand.SupportEmail, `class="moin-compare"`, `class="moin-terminal`} {
		if !strings.Contains(body, want) {
			t.Errorf("/brand misses %s", want)
		}
	}

	mark, err := assets.Files.ReadFile("img/mark.svg")
	if err != nil {
		t.Fatal(err)
	}
	path := regexp.MustCompile(` d="([^"]+)"`).FindSubmatch(mark)
	if path == nil || !strings.Contains(body, string(path[1])) {
		t.Error("/brand draws another mark than assets/img/mark.svg")
	}

	files := regexp.MustCompile(`href="/assets/[^/"]+/(brand/[^"]+)"[^>]*download=`).FindAllStringSubmatch(body, -1)
	if len(files) != 12 {
		t.Errorf("/brand offers %d downloads, want 12", len(files))
	}
	for _, file := range files {
		data, err := assets.Files.ReadFile(file[1])
		if err != nil || len(data) == 0 {
			t.Errorf("/brand offers %s, which is missing", file[1])
		}
		if strings.HasSuffix(file[1], ".svg") && !strings.Contains(string(data), string(path[1])) && !strings.Contains(file[1], "wordmark") {
			t.Errorf("%s draws another mark than assets/img/mark.svg", file[1])
		}
	}
}
