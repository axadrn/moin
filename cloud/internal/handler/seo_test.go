package handler

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRobotsNameTheSitemap(t *testing.T) {
	handler := NewSEOHandler(nil, nil, "https://example.com/", os.DirFS(t.TempDir()))
	recorder := httptest.NewRecorder()
	handler.Robots(recorder, httptest.NewRequest("GET", "/robots.txt", nil))
	if !strings.Contains(recorder.Body.String(), "Sitemap: https://example.com/sitemap.xml") {
		t.Fatalf("robots response = %q", recorder.Body.String())
	}
}
