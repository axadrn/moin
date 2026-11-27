package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// TestHomeBlog shows the three newest posts of the blog, newest first,
// and a link to all of them.
func TestHomeBlog(t *testing.T) {
	post := func(title, date string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("---\ntitle: " + title + "\ndescription: About " + title + ".\ndate: \"" + date + "\"\n---\n\nText.\n")}
	}
	files := fstest.MapFS{
		"blog/first.md":  post("First", "2026-11-01"),
		"blog/fourth.md": post("Fourth", "2026-11-04"),
		"blog/second.md": post("Second", "2026-11-02"),
		"blog/third.md":  post("Third", "2026-11-03"),
	}
	w := httptest.NewRecorder()
	knocks := func() *pages.HomeKnocks { return nil }
	numbers := func() *pages.HomeNumbers { return nil }
	Home(files, knocks, numbers)(w, httptest.NewRequest("GET", "/", nil))
	data, _ := io.ReadAll(w.Result().Body)
	body := string(data)

	if strings.Contains(body, `href="/blog/first"`) {
		t.Error("the oldest of four posts shows on the home page")
	}
	last := -1
	for _, slug := range []string{"fourth", "third", "second"} {
		at := strings.Index(body, `href="/blog/`+slug+`"`)
		if at < 0 {
			t.Fatalf("no link to /blog/%s", slug)
		}
		if at < last {
			t.Errorf("/blog/%s is out of order", slug)
		}
		last = at
	}
	if !strings.Contains(body, `<a href="/blog" class="moin-link moin-foreground">See all</a>`) {
		t.Error("no link to /blog")
	}
}
