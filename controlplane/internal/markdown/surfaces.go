package markdown

import (
	"bufio"
	"bytes"
	"context"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// A surfaces block says how to do one task on each surface of ops:
//
//	```surfaces
//	Web: **Server** → **Security** → **Keys**
//	CLI: ops integrations cloudflare
//	TUI: settings tab, **Cloudflare token**, `enter`
//	API: PUT /api/v1/integrations
//	MCP: set_integrations
//	```
//
// The docs show it as tabs, one per surface the block names, in this order.
// Web and TUI lines are inline markdown, the others a line of code. A
// surface named twice gets both lines. A command that ends in a backslash
// goes on in the next line, as in a shell. Any other line is ignored.

type surface struct {
	value string
	label string
	code  bool
}

var surfaces = []surface{
	{"web", "Web", false},
	{"cli", "CLI", true},
	{"tui", "TUI", false},
	{"api", "API", true},
	{"mcp", "MCP", true},
}

// surfaceEntry is one surface of a block with its lines.
type surfaceEntry struct {
	surface
	lines []string
}

// parseSurfaces reads the lines of a block, in the order of surfaces.
func parseSurfaces(body string) []surfaceEntry {
	lines := map[string][]string{}
	// continued is the surface whose last line ends in a backslash: the next
	// line goes on with the same command.
	continued := ""
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if continued != "" {
			last := len(lines[continued]) - 1
			lines[continued][last] += "\n" + line
			if !strings.HasSuffix(line, "\\") {
				continued = ""
			}
			continue
		}
		name, rest, ok := strings.Cut(line, ":")
		rest = strings.TrimSpace(rest)
		if !ok || rest == "" {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		lines[name] = append(lines[name], rest)
		if strings.HasSuffix(rest, "\\") {
			continued = name
		}
	}
	var entries []surfaceEntry
	for _, s := range surfaces {
		if len(lines[s.value]) > 0 {
			entries = append(entries, surfaceEntry{s, lines[s.value]})
		}
	}
	return entries
}

var kindSurfaces = ast.NewNodeKind("Surfaces")

// surfacesNode takes the place of a fenced code block with the info string
// surfaces.
type surfacesNode struct {
	ast.BaseBlock
	entries []surfaceEntry
}

func (n *surfacesNode) Kind() ast.NodeKind { return kindSurfaces }

func (n *surfacesNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type surfacesTransformer struct{}

func (surfacesTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var blocks []*ast.FencedCodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if block, ok := n.(*ast.FencedCodeBlock); ok && entering && string(block.Language(source)) == "surfaces" {
			blocks = append(blocks, block)
		}
		return ast.WalkContinue, nil
	})
	for _, block := range blocks {
		var body bytes.Buffer
		for i := 0; i < block.Lines().Len(); i++ {
			line := block.Lines().At(i)
			body.Write(line.Value(source))
		}
		node := &surfacesNode{entries: parseSurfaces(body.String())}
		block.Parent().ReplaceChild(block.Parent(), block, node)
	}
}

type surfacesRenderer struct {
	// inline renders the markdown of a Web or TUI line.
	inline goldmark.Markdown
}

func (r surfacesRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindSurfaces, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		node := n.(*surfacesNode)
		if !entering || len(node.entries) == 0 {
			return ast.WalkContinue, nil
		}
		items := make([]surfaceItem, 0, len(node.entries))
		for _, e := range node.entries {
			item := surfaceItem{Value: e.value, Label: e.label, Code: e.code}
			if e.code {
				item.Text = strings.Join(e.lines, "\n")
			} else {
				for _, line := range e.lines {
					item.HTML = append(item.HTML, r.inlineHTML(line))
				}
			}
			items = append(items, item)
		}
		return ast.WalkSkipChildren, surfacesView(items).Render(context.Background(), w)
	})
}

// inlineHTML is the markdown of one line without its paragraph.
func (r surfacesRenderer) inlineHTML(line string) string {
	var buf bytes.Buffer
	if err := r.inline.Convert([]byte(line), &buf); err != nil {
		return ""
	}
	html := strings.TrimSpace(buf.String())
	html = strings.TrimPrefix(html, "<p>")
	return strings.TrimSuffix(html, "</p>")
}

// surfacesExtension renders surfaces blocks as tabs.
type surfacesExtension struct{}

func (surfacesExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(surfacesTransformer{}, 100)))
	inline := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Typographer))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(surfacesRenderer{inline: inline}, 100)))
}

// PlainSurfaces turns every surfaces block of a markdown source into a list,
// for the markdown the docs hand to people and AI agents:
//
//   - Web: **Server** → **Security** → **Keys**
//   - CLI: `ops integrations cloudflare`
func PlainSurfaces(source []byte) []byte {
	var out bytes.Buffer
	var block []string
	inBlock := false
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case !inBlock && strings.TrimSpace(line) == "```surfaces":
			inBlock = true
			block = block[:0]
		case inBlock && strings.TrimSpace(line) == "```":
			inBlock = false
			for _, e := range parseSurfaces(strings.Join(block, "\n")) {
				for _, l := range e.lines {
					if e.code && strings.Contains(l, "\n") {
						out.WriteString("- " + e.label + ":\n\n  ```\n  " + strings.ReplaceAll(l, "\n", "\n  ") + "\n  ```\n")
						continue
					}
					if e.code {
						l = codeSpan(l)
					}
					out.WriteString("- " + e.label + ": " + l + "\n")
				}
			}
		case inBlock:
			block = append(block, line)
		default:
			out.WriteString(line + "\n")
		}
	}
	return out.Bytes()
}

// codeSpan wraps s in backticks, more of them when s holds one.
func codeSpan(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}
