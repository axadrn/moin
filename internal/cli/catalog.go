package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/style"
)

// catalogEntry is one app of the catalog, from its app.md.
type catalogEntry struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Pitch    string `json:"pitch"`
	Category string `json:"category"`
	Website  string `json:"website"`
	Login    string `json:"login,omitempty"`
}

// catalogList is `moin catalog`: the open source apps built into moin,
// each deployed with `moin deploy <server> --catalog <name>`.
func catalogList(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("catalog", "catalog [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	entries, err := catalogEntries()
	if err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, entries)
	}
	rows := [][]string{}
	for _, entry := range entries {
		rows = append(rows, []string{style.Bold.Render(entry.Name), entry.Category, style.Muted.Render(entry.Pitch)})
	}
	table(stdout, []string{"NAME", "WHAT", "ABOUT"}, rows)
	fmt.Fprintln(stdout)
	hint(stdout, "Deploy one with", build.Name+" deploy <server> --catalog umami")
	return nil
}

// catalogEntries reads every app.md of the catalog, sorted by name.
func catalogEntries() ([]catalogEntry, error) {
	folders, err := fs.ReadDir(catalog.Files, ".")
	if err != nil {
		return nil, err
	}
	entries := []catalogEntry{}
	for _, folder := range folders {
		body, err := fs.ReadFile(catalog.Files, folder.Name()+"/app.md")
		if err != nil {
			continue
		}
		fields := frontMatter(string(body))
		entries = append(entries, catalogEntry{
			Name:     folder.Name(),
			Title:    fields["name"],
			Pitch:    fields["pitch"],
			Category: fields["category"],
			Website:  fields["website"],
			Login:    fields["login"],
		})
	}
	return entries, nil
}

// frontMatter reads the "key: value" lines between the --- lines at the
// top of an app.md.
func frontMatter(text string) map[string]string {
	fields := map[string]string{}
	block, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return fields
	}
	block, _, _ = strings.Cut(block, "\n---")
	for line := range strings.Lines(block) {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return fields
}
