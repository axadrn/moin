package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// inline renders one line of markdown, with nothing else switched on.
var inline = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Typographer))

// Inline is the markdown of one line as HTML, without its paragraph.
func Inline(line string) string {
	var buf bytes.Buffer
	if err := inline.Convert([]byte(line), &buf); err != nil {
		return ""
	}
	html := strings.TrimSpace(buf.String())
	html = strings.TrimPrefix(html, "<p>")
	return strings.TrimSuffix(html, "</p>")
}
