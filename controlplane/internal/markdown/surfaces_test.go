package markdown

import (
	"strings"
	"testing"
)

func TestSurfacesRenderAsTabs(t *testing.T) {
	source := "Intro.\n\n```surfaces\nWeb: **Server** → [Security](/docs/guides/security)\nCLI: ops integrations cloudflare\nnot a surface line\nDesktop: ignored\nMCP: set_integrations\nAPI: PUT /api/v1/integrations\n```\n"
	html, err := NewParser().Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	out := string(html)

	for _, want := range []string{
		`data-slot="tabs"`,
		`data-surfaces`,
		`role="tablist"`,
		`data-slot="tabs-trigger"`,
		`<strong>Server</strong>`,
		`<a href="/docs/guides/security">Security</a>`,
		`<pre class="m-0 rounded-none bg-transparent px-6 py-4 pr-14"><code>ops integrations cloudflare</code></pre>`,
		`<code>PUT /api/v1/integrations</code>`,
		`<code>set_integrations</code>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"not a surface line", "Desktop", "ignored", "language-surfaces", ">TUI<"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q in\n%s", unwanted, out)
		}
	}

	// Tabs follow the surface order, not the order of the lines, and the
	// first is open.
	order := []string{">Web<", ">CLI<", ">API<", ">MCP<"}
	last := -1
	for _, label := range order {
		i := strings.Index(out, label)
		if i < last {
			t.Errorf("%s out of order", label)
		}
		last = i
	}
	if !strings.Contains(out, `aria-selected="true"`) || strings.Count(out, `data-hidden`) != 3 {
		t.Errorf("want the first tab open and three hidden panels:\n%s", out)
	}
}

func TestOtherCodeBlocksStay(t *testing.T) {
	html, err := NewParser().Parse([]byte("```bash\nops deploy\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `<pre><code class="language-bash">ops deploy`) {
		t.Errorf("got %s", html)
	}
}

func TestPlainSurfaces(t *testing.T) {
	source := "# Keys\n\n```surfaces\nWeb: **Keys**\nCLI: ops integrations\nCLI: ops integrations rm `x`\nnoise\n```\n\n```bash\nops deploy\n```\n"
	want := "# Keys\n\n- Web: **Keys**\n- CLI: `ops integrations`\n- CLI: `` ops integrations rm `x` ``\n\n```bash\nops deploy\n```\n"
	if got := string(PlainSurfaces([]byte(source))); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSurfacesLongCommand(t *testing.T) {
	source := "```surfaces\nCLI: ops backups bucket set \\\n  --bucket b \\\n  --access-key k\nMCP: set_backup_storage\n```\n"
	html, err := NewParser().Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "<code>ops backups bucket set \\\n  --bucket b \\\n  --access-key k</code>") {
		t.Errorf("got %s", html)
	}
	want := "- CLI:\n\n  ```\n  ops backups bucket set \\\n    --bucket b \\\n    --access-key k\n  ```\n- MCP: `set_backup_storage`\n"
	if got := string(PlainSurfaces([]byte(source))); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
