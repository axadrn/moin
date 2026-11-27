package markdown

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/axadrn/moin/hub/internal/ui/components"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Every code block of the site gets a copy button (components.Code).

var kindCode = ast.NewNodeKind("Code")

// codeNode takes the place of a fenced code block.
type codeNode struct {
	ast.BaseBlock
	code string
	// n counts the blocks of a page from 1, so each has its own id.
	n int
}

func (n *codeNode) Kind() ast.NodeKind { return kindCode }

func (n *codeNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type codeTransformer struct{}

func (codeTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var blocks []*ast.FencedCodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if block, ok := n.(*ast.FencedCodeBlock); ok && entering {
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
		node := &codeNode{code: strings.TrimRight(body.String(), "\n"), n: n + 1}
		block.Parent().ReplaceChild(block.Parent(), block, node)
	}
}

type codeRenderer struct{}

func (codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindCode, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		node := n.(*codeNode)
		code := components.Code(components.CodeProps{ID: fmt.Sprintf("code-%d", node.n), Text: node.code})
		return ast.WalkSkipChildren, code.Render(context.Background(), w)
	})
}

type codeExtension struct{}

func (codeExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(util.Prioritized(codeTransformer{}, 100)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(codeRenderer{}, 100)))
}
