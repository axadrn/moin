package assets

import (
	"bytes"
	"io/fs"
	"net/url"
	"strings"
	"testing"

	"github.com/axadrn/moin"
)

// The bundle has every imported file in it, no @import left, and each url()
// in it points at a file the hub serves.
func TestCSS(t *testing.T) {
	ui := moin.UI(false)
	css, err := CSS(ui, AssetsFS)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(css, []byte("@import")) {
		t.Error("app.css still has an @import")
	}

	entry, err := fs.ReadFile(ui, "moin.css")
	if err != nil {
		t.Fatal(err)
	}
	imports := importPattern.FindAllSubmatch(entry, -1)
	if len(imports) < 40 {
		t.Fatalf("moin.css has %d imports, want all of them found", len(imports))
	}
	for _, match := range imports {
		name := string(match[1])
		source, err := fs.ReadFile(ui, name)
		if err != nil {
			t.Fatal(err)
		}
		if rule := firstRule(source); !bytes.Contains(css, rule) {
			t.Errorf("app.css misses %q from %s", rule, name)
		}
	}
	own, err := fs.ReadFile(AssetsFS, "css/hub.css")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(css, own) {
		t.Error("app.css does not end with css/hub.css")
	}

	// The bundle is served at /assets/<version>/app.css.
	base, _ := url.Parse("/assets/v1.2.3/app.css")
	urls := 0
	for _, match := range urlPattern.FindAllSubmatch(css, -1) {
		target := string(match[1]) + string(match[2]) + string(match[3])
		if strings.HasPrefix(target, "data:") {
			continue
		}
		urls++
		ref, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		file := base.ResolveReference(ref).Path
		files, name := fs.FS(AssetsFS), strings.TrimPrefix(file, "/assets/")
		if rest, ok := strings.CutPrefix(file, "/assets/v1.2.3/ui/"); ok {
			files, name = ui, rest
		} else if rest, ok := strings.CutPrefix(file, "/assets/v1.2.3/"); ok {
			name = rest
		}
		if _, err := fs.Stat(files, name); err != nil {
			t.Errorf("url(%q) in app.css points at %s, which does not exist", target, file)
		}
	}
	if urls < 40 {
		t.Errorf("app.css has %d url()s, want the icons among them", urls)
	}
}

// firstRule is the first line of a CSS file that opens a block.
func firstRule(source []byte) []byte {
	for line := range bytes.SplitSeq(source, []byte("\n")) {
		if bytes.Contains(line, []byte("{")) {
			return line
		}
	}
	return nil
}

func TestRebase(t *testing.T) {
	for _, test := range []struct{ dir, in, want string }{
		{"components", `mask: url("../icons/check.svg") center;`, `mask: url("ui/icons/check.svg") center;`},
		{".", `--i: url("icons/x.svg");`, `--i: url("ui/icons/x.svg");`},
		{".", `--i: url('icons/x.svg');`, `--i: url("ui/icons/x.svg");`},
		{"components", `src: url(../fonts/a.woff2);`, `src: url("ui/fonts/a.woff2");`},
		{"components", `src: url("/assets/fonts/a.woff2");`, `src: url("/assets/fonts/a.woff2");`},
		{"components", `mask: url("data:image/svg+xml,%3Csvg/%3E");`, `mask: url("data:image/svg+xml,%3Csvg/%3E");`},
		{"components", `background: url("https://example.com/a.png");`, `background: url("https://example.com/a.png");`},
	} {
		if got := string(rebase([]byte(test.in), test.dir)); got != test.want {
			t.Errorf("rebase(%q, %q) = %q, want %q", test.in, test.dir, got, test.want)
		}
	}
}

// app.js is moin.js, then hub.js, as the pages loaded them before.
func TestJS(t *testing.T) {
	ui := moin.UI(false)
	js, err := JS(ui, AssetsFS)
	if err != nil {
		t.Fatal(err)
	}
	kit, _ := fs.ReadFile(ui, "moin.js")
	own, _ := fs.ReadFile(AssetsFS, "js/hub.js")
	if len(kit) == 0 || len(own) == 0 {
		t.Fatal("moin.js or hub.js is empty")
	}
	if !bytes.HasPrefix(js, kit) || !bytes.HasSuffix(js, own) {
		t.Error("app.js is not moin.js followed by hub.js")
	}
}
