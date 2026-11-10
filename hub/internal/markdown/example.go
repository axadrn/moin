package markdown

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/axadrn/moin/hub/internal/ui/components"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// An example block shows one file of examples/ in the docs of moin UI, what it
// looks like and its code:
//
//	```example
//	badge
//	```
//
// The word resize on the next line lets the reader pull the preview narrower.
// Every other code block of these docs gets a copy button.

var kindExample = ast.NewNodeKind("Example")

// exampleNode takes the place of a fenced code block.
type exampleNode struct {
	ast.BaseBlock
	// name is the file of an example block, empty for any other code.
	name   string
	resize bool
	code   string
	// n counts the blocks of a page from 1, so each has its own id.
	n int
}

func (n *exampleNode) Kind() ast.NodeKind { return kindExample }

func (n *exampleNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type exampleTransformer struct{}

func (exampleTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var blocks []*ast.FencedCodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if block, ok := n.(*ast.FencedCodeBlock); ok && entering && string(block.Language(source)) != "surfaces" {
			blocks = append(blocks, block)
		}
		return ast.WalkContinue, nil
	})
	for n, block := range blocks {
		var body bytes.Buffer
		for i := 0; i < block.Lines().Len(); i++ {
			line := block.Lines().At(i)
			body.Write(line.Value(source))
		}
		node := &exampleNode{code: strings.TrimRight(body.String(), "\n"), n: n + 1}
		if string(block.Language(source)) == "example" {
			words := strings.Fields(node.code)
			if len(words) > 0 {
				node.name = words[0]
			}
			node.resize = len(words) > 1 && words[1] == "resize"
		}
		block.Parent().ReplaceChild(block.Parent(), block, node)
	}
}

type exampleRenderer struct {
	files fs.FS
}

func (r exampleRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindExample, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		node := n.(*exampleNode)
		id := fmt.Sprintf("code-%d", node.n)
		if node.name == "" {
			return ast.WalkSkipChildren, components.Code(components.CodeProps{ID: id, Text: node.code}).Render(context.Background(), w)
		}
		example, err := fs.ReadFile(r.files, examplePath(node.name))
		if err != nil {
			return ast.WalkStop, fmt.Errorf("example %s: %w", node.name, err)
		}
		code := strings.TrimSpace(string(example))
		preview := components.Preview(components.PreviewProps{ID: id, Code: code, Resize: node.resize})
		ctx := templ.WithChildren(context.Background(), templ.Raw(code))
		return ast.WalkSkipChildren, preview.Render(ctx, w)
	})
}

func examplePath(name string) string {
	return path.Join("examples", name+".html")
}

type examplesExtension struct {
	files fs.FS
}

func (e examplesExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(exampleTransformer{}, 100)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(exampleRenderer{files: e.files}, 100)))
}

// Examples makes example blocks show the files of examples/ in files.
func Examples(files fs.FS) goldmark.Extender {
	return examplesExtension{files: files}
}

var exampleBlock = regexp.MustCompile("(?m)^```example\\n(\\S+)[^`]*?\\n```$")

// PlainExamples is the markdown with each example block as its HTML, for the
// .md address of a page.
func PlainExamples(source []byte, files fs.FS) []byte {
	return exampleBlock.ReplaceAllFunc(source, func(block []byte) []byte {
		name := exampleBlock.FindSubmatch(block)[1]
		example, err := fs.ReadFile(files, examplePath(string(name)))
		if err != nil {
			return block
		}
		return append(append([]byte("```html\n"), bytes.TrimSpace(example)...), []byte("\n```")...)
	})
}
