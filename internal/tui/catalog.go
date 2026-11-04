package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// catalogCommand runs :catalog, :catalog <app> and
// :catalog install <app> <server> [version].
func (m model) catalogCommand(args []string) (tea.Model, tea.Cmd) {
	switch {
	case len(args) == 0:
		return m, m.showCatalog()
	case args[0] == "install":
		if len(args) < 3 || len(args) > 4 {
			m.err = fmt.Errorf("install it on a server: :catalog install <app> <server> [version]")
			return m, nil
		}
		version := ""
		if len(args) == 4 {
			version = args[3]
		}
		return m, m.installCatalog(args[1], args[2], version)
	case len(args) == 1:
		return m, m.showCatalogApp(args[0])
	}
	m.err = fmt.Errorf("E488: :catalog, :catalog <app> or :catalog install <app> <server> [version]")
	return m, nil
}

// showCatalog lists the catalog in the output panel.
func (m model) showCatalog() tea.Cmd {
	return func() tea.Msg {
		entries, err := m.api.Catalog(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return catalogOutput(m.shared.outputs.Add(1), entries, time.Now())
	}
}

func catalogOutput(id int64, entries []client.CatalogEntry, now time.Time) outputMsg {
	rows := [][]any{}
	for _, entry := range entries {
		latest := "-"
		if entry.Latest != nil {
			latest = entry.Latest.Tag
			if entry.Latest.Date != nil {
				latest += ", " + duration(now.Sub(*entry.Latest.Date)) + " ago"
			}
		}
		rows = append(rows, []any{entry.Slug, entry.Category, latest, entry.Database, entry.Pitch})
	}
	lines := resultLines([]string{"app", "category", "newest", "database", "what it is"}, rows)
	return outputMsg{id: id, title: "catalog", note: style.Muted.Render(":catalog <app> for its versions"), lines: lines}
}

// showCatalogApp shows one catalog app and its releases.
func (m model) showCatalogApp(slug string) tea.Cmd {
	return func() tea.Msg {
		entry, err := m.api.CatalogApp(m.ctx, slug)
		if err != nil {
			return errMsg{err}
		}
		lines := []string{entry.Pitch, style.Muted.Render(entry.Website + " · " + entry.License + " · " + entry.Image)}
		if entry.Login != "" {
			lines = append(lines, entry.Login)
		}
		lines = append(lines, "")
		rows := [][]any{}
		for _, version := range entry.Versions {
			date := "-"
			if version.Date != nil {
				date = version.Date.Local().Format("2006-01-02")
			}
			rows = append(rows, []any{version.Tag, date})
		}
		lines = append(lines, resultLines([]string{"version", "released"}, rows)...)
		note := style.Muted.Render(":catalog install " + entry.Slug + " <server> [version]")
		return outputMsg{id: m.shared.outputs.Add(1), title: entry.Name, note: note, lines: lines}
	}
}

// installCatalog installs a catalog app and follows its deploy.
func (m model) installCatalog(slug, server, version string) tea.Cmd {
	return func() tea.Msg {
		installed, err := m.api.InstallCatalogApp(m.ctx, slug, client.CatalogInstall{Server: server, Version: version})
		if err != nil {
			return errMsg{err}
		}
		return m.follow(installed.App.Name, installed.Deploy, fmt.Sprintf("install of %s on %s", installed.App.Name, server))
	}
}

// updateCatalogApp updates the selected catalog app and follows the deploy.
func (m model) updateCatalogApp(name, version string) tea.Cmd {
	return func() tea.Msg {
		updated, err := m.api.UpdateApp(m.ctx, name, version)
		if err != nil {
			return errMsg{err}
		}
		return m.follow(name, updated.Deploy, fmt.Sprintf("update of %s, v%d", name, updated.Deploy.Number))
	}
}
