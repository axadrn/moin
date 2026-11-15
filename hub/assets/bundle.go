package assets

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
)

// The hub's pages load one stylesheet, app.css, and one script, app.js,
// joined here from moin UI and the hub's own files. moin UI keeps its
// @imports for people who copy it; a page that followed them would load
// some fifty files.

// importPattern is an @import as moin UI writes it: @import "x.css";
var importPattern = regexp.MustCompile(`@import\s+"([^"]+)"\s*;`)

// urlPattern is a url() with its target in double, single or no quotes.
var urlPattern = regexp.MustCompile(`url\(\s*(?:"([^"]*)"|'([^']*)'|([^"'()\s]*))\s*\)`)

// CSS is ui/moin.css with each @import replaced by its file, then
// css/hub.css. It is served from the folder above ui/ and css/, so a
// relative url() of either gets the path from there.
func CSS(ui, hub fs.FS) ([]byte, error) {
	var out bytes.Buffer
	if err := inline(&out, ui, "moin.css"); err != nil {
		return nil, err
	}
	own, err := fs.ReadFile(hub, "css/hub.css")
	if err != nil {
		return nil, err
	}
	out.Write(rebase(own, "css"))
	return out.Bytes(), nil
}

// JS is ui/moin.js, then js/hub.js, in the order the pages ran them.
func JS(ui, hub fs.FS) ([]byte, error) {
	kit, err := fs.ReadFile(ui, "moin.js")
	if err != nil {
		return nil, err
	}
	own, err := fs.ReadFile(hub, "js/hub.js")
	if err != nil {
		return nil, err
	}
	// The semicolon ends the first file's last statement, written with or
	// without one.
	return bytes.Join([][]byte{kit, own}, []byte("\n;\n")), nil
}

// inline writes the ui file name, its imports replaced by their files.
func inline(out *bytes.Buffer, ui fs.FS, name string) error {
	source, err := fs.ReadFile(ui, name)
	if err != nil {
		return fmt.Errorf("css bundle: %w", err)
	}
	dir := path.Dir(name)
	source = rebase(source, path.Join("ui", dir))
	last := 0
	for _, match := range importPattern.FindAllSubmatchIndex(source, -1) {
		out.Write(source[last:match[0]])
		if err := inline(out, ui, path.Join(dir, string(source[match[2]:match[3]]))); err != nil {
			return err
		}
		last = match[1]
	}
	out.Write(source[last:])
	return nil
}

// rebase points each relative url() of a file in dir at the bundle's
// folder: ui/components/x.css's "../icons/a.svg" is "ui/icons/a.svg",
// css/hub.css's "../fonts/a.woff2" is "fonts/a.woff2".
func rebase(source []byte, dir string) []byte {
	return urlPattern.ReplaceAllFunc(source, func(match []byte) []byte {
		parts := urlPattern.FindSubmatch(match)
		target := string(parts[1]) + string(parts[2]) + string(parts[3])
		if target == "" || strings.HasPrefix(target, "/") || strings.HasPrefix(target, "#") || strings.Contains(target, ":") {
			return match
		}
		return []byte(`url("` + path.Join(dir, target) + `")`)
	})
}
