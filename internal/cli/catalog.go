package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// catalog lists the catalog, shows one app with its releases, or installs
// one: catalog, catalog <app>, catalog install <app> --server <server>.
func catalog(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "install" {
		return catalogInstall(ctx, args[1:], stdout)
	}
	flags := newFlags("catalog", "catalog [app]")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if flags.NArg() == 1 {
		entry, err := api.CatalogApp(ctx, flags.Arg(0))
		if err != nil {
			return err
		}
		printCatalogApp(stdout, entry)
		return nil
	}
	entries, err := api.Catalog(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		latest := style.Muted.Render("-")
		if entry.Latest != nil {
			latest = entry.Latest.Tag
			if entry.Latest.Date != nil {
				latest += style.Muted.Render(", " + since(*entry.Latest.Date, now))
			}
		}
		rows = append(rows, []string{entry.Slug, entry.Category, latest, dash(entry.Database), entry.Pitch})
	}
	table(stdout, []string{"APP", "CATEGORY", "NEWEST", "DATABASE", "WHAT IT IS"}, rows)
	hint(stdout, "Install one with", build.Name+" catalog install <app> --server <server>")
	return nil
}

// printCatalogApp shows a catalog app and its releases, newest first.
func printCatalogApp(stdout io.Writer, entry client.CatalogEntry) {
	fmt.Fprintln(stdout, style.Bold.Render(entry.Name)+" "+entry.Pitch)
	fmt.Fprintln(stdout, style.Muted.Render(entry.Website+" · "+entry.License+" · "+entry.Image))
	switch entry.Database {
	case "postgres":
		fmt.Fprintln(stdout, "Its Postgres: every change backed up, checked every day.")
	case "sqlite":
		fmt.Fprintln(stdout, "Its SQLite: every change backed up, checked every day.")
	default:
		if entry.DatabaseNote != "" {
			fmt.Fprintln(stdout, entry.DatabaseNote)
		}
	}
	if entry.Login != "" {
		fmt.Fprintln(stdout, entry.Login)
	}
	fmt.Fprintln(stdout)
	now := time.Now()
	shown := entry.Versions
	if len(shown) > 15 {
		shown = shown[:15]
	}
	rows := make([][]string, 0, len(shown))
	for _, version := range shown {
		date := style.Muted.Render("-")
		if version.Date != nil {
			date = version.Date.Local().Format("2006-01-02") + style.Muted.Render(", "+since(*version.Date, now))
		}
		rows = append(rows, []string{version.Tag, date})
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, style.Muted.Render("No releases read from the registry yet. Try again in a minute."))
	} else {
		table(stdout, []string{"VERSION", "RELEASED"}, rows)
		if more := len(entry.Versions) - len(shown); more > 0 {
			fmt.Fprintln(stdout, style.Muted.Render(fmt.Sprintf("and %d older", more)))
		}
	}
	if entry.NeedsDomain {
		hint(stdout, "It needs a domain. Install it with", build.Name+" catalog install "+entry.Slug+" --server <server> --domain <domain>")
		return
	}
	hint(stdout, "Install it with", build.Name+" catalog install "+entry.Slug+" --server <server>")
}

// catalogInstall installs a catalog app and follows its first deploy.
func catalogInstall(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("catalog install", "catalog install <app> --server <server> [--version <tag>] [--name <name>] [--domain <domain>]")
	server := flags.String("server", "", "the server to install it on")
	version := flags.String("version", "", "a release from moin catalog <app>; default the newest")
	name := flags.String("name", "", "the app name; default the catalog app's")
	domain := flags.String("domain", "", "a domain for the app")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || strings.TrimSpace(*server) == "" {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	installed, err := api.InstallCatalogApp(ctx, flags.Arg(0), client.CatalogInstall{Server: *server, Version: *version, Name: *name, Domain: *domain})
	if err != nil {
		return err
	}
	err = follow(ctx, api, installed.App.Name, installed.Deploy, "Deploy", false, stdout)
	if err != nil {
		return err
	}
	entry, err := api.CatalogApp(ctx, flags.Arg(0))
	if err == nil && entry.DatabaseNote != "" {
		fmt.Fprintln(stdout, "  "+entry.DatabaseNote)
	}
	if err == nil && entry.Login != "" {
		fmt.Fprintln(stdout, "  "+entry.Login)
	}
	if *domain == "" {
		hint(stdout, "Give it a domain with", build.Name+" domains add --app "+installed.App.Name+" <domain>")
	}
	return nil
}

// updateApp sets a catalog app to a version, the newest by default, and
// follows the deploy.
func updateApp(ctx context.Context, app, version string, stdout io.Writer) error {
	api, err := apiClient()
	if err != nil {
		return err
	}
	if version != "" {
		fmt.Fprintln(stdout, style.Muted.Render("An older version may not undo the migrations of a newer one; moin takes a snapshot first."))
	}
	updated, err := api.UpdateApp(ctx, app, version)
	if err != nil {
		return err
	}
	return follow(ctx, api, updated.App.Name, updated.Deploy, "Update", false, stdout)
}
