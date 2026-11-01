package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSEOHandlerServesAbsoluteSitemapReference(t *testing.T) {
	handler := NewSEOHandler(nil, "https://example.com/", t.TempDir(), false)
	recorder := httptest.NewRecorder()
	handler.Robots(recorder, httptest.NewRequest("GET", "/robots.txt", nil))
	if !strings.Contains(recorder.Body.String(), "Sitemap: https://example.com/sitemap.xml") {
		t.Fatalf("robots response = %q", recorder.Body.String())
	}
}

func TestSEOHandlerDisallowsRobotsWhenNoIndexIsEnabled(t *testing.T) {
	handler := NewSEOHandler(nil, "https://example.com/", t.TempDir(), true)
	recorder := httptest.NewRecorder()
	handler.Robots(recorder, httptest.NewRequest("GET", "/robots.txt", nil))
	if recorder.Body.String() != "User-agent: *\nDisallow: /\n" {
		t.Fatalf("robots response = %q", recorder.Body.String())
	}
}
