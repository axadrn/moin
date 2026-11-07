package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// backups lists the backups of an app with the moments to go back to, or
// sets how often and how long with --every, --keep and --every-change.
func backups(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("backups", "backups [app] [--every 15m|1h|1d] [--keep 1d|3d|7d|30d] [--every-change on|off]").withApp().withYes()
	every := flags.String("every", "", "how often moin copies the databases: 15m, 1h or 1d")
	keep := flags.String("keep", "", "how long each copy stays: 1d, 3d, 7d or 30d")
	everyChange := flags.String("every-change", "", "on or off: also record every change of a SQLite file, to go back to any minute")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	list, err := api.Backups(ctx, name)
	if err != nil {
		return err
	}
	if *every == "" && *keep == "" && *everyChange == "" {
		printBackups(stdout, name, list, time.Now())
		return nil
	}
	schedule := list.BackupSchedule
	if *every != "" {
		if schedule.EveryMinutes = map[string]int{"15m": 15, "1h": 60, "60m": 60, "1d": 1440, "24h": 1440}[*every]; schedule.EveryMinutes == 0 {
			return fmt.Errorf("--every is 15m, 1h or 1d")
		}
	}
	if *keep != "" {
		days, err := strconv.Atoi(strings.TrimSuffix(*keep, "d"))
		if err != nil || !slices.Contains([]int{1, 3, 7, 30}, days) {
			return fmt.Errorf("--keep is 1d, 3d, 7d or 30d")
		}
		schedule.KeepDays = days
	}
	switch *everyChange {
	case "":
	case "on":
		schedule.EveryChange = true
	case "off":
		if schedule.EveryChange {
			if err := confirm(stdin, stdout, flags.yes(), "Turn Every change off for "+name+"? What it wrote down goes; the copies stay."); err != nil {
				return err
			}
		}
		schedule.EveryChange = false
	default:
		return fmt.Errorf("--every-change is on or off")
	}
	schedule, err = api.SetBackupSchedule(ctx, name, schedule)
	if err != nil {
		return err
	}
	done(stdout, "%s is copied %s, each copy kept %s, every change %s.", name, everyText(schedule.EveryMinutes), keepText(schedule.KeepDays), onOff(schedule.EveryChange))
	return nil
}

func everyText(minutes int) string {
	switch minutes {
	case 60:
		return "hourly"
	case 1440:
		return "daily"
	}
	return fmt.Sprintf("every %d minutes", minutes)
}

func keepText(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// backUp is moin backup: download a copy.
func backUp(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "download" {
		return fmt.Errorf("usage: `%s backup download [app] [--db <path>] [--to <moment>] [--link]`. moin copies every database on its schedule: %s backups lists the copies", build.Name, build.Name)
	}
	flags := newFlags("backup download", "backup download [app] [--db <path>] [--to <moment>] [--link]").withApp()
	database := flags.String("db", "", "database path in the container; needed when the app has several")
	to := flags.String("to", "", "a copy from "+build.Name+" backups, like before-v12; the newest without it")
	link := flags.Bool("link", false, "print the link to the copy in your bucket instead of saving it here")
	if err := flags.parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	download, err := api.Download(ctx, name, *database, strings.TrimSpace(*to))
	if err != nil {
		return err
	}
	if *link {
		fmt.Fprintln(stdout, download.Link)
		hint(stdout, "The link works for 15 minutes:", build.Name+" backup download "+name+" --link")
		return nil
	}
	file := copyName(name, download.Link)
	out, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("save the copy: %w", err)
	}
	err = api.FetchCopy(ctx, download.Link, out)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file)
		return err
	}
	done(stdout, "Saved %s (%s), the copy of %s from %s.", file, human.Size(float64(download.Size)), download.Database, download.At.UTC().Format("Jan 2 15:04 UTC"))
	return nil
}

// copyName is the file a copy saves to: the app, the database and the
// copy's name in the bucket, like shop-data-app.db-20261107T140000Z.db.gz.
func copyName(app, link string) string {
	parsed, err := url.Parse(link)
	if err != nil {
		return app + "-copy"
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 3 {
		return app + "-copy"
	}
	return app + "-" + parts[len(parts)-3] + "-" + parts[len(parts)-1]
}

// restore puts a database back to a moment.
func restore(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("restore", `restore [app] --to <moment> [--db <path>] [--confirm <app>]`).withApp().withYes()
	database := flags.String("db", "", "database path in the container; needed when the app has several")
	typed := flags.String("confirm", "", "the app's name, typed out, for scripts; without it the CLI asks for it")
	to := flags.String("to", "", `a moment from `+build.Name+` backups, like before-v12 or 1h, or a time in UTC, like 14:01 or "2026-11-07 14:01"`)
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	if strings.TrimSpace(*to) == "" {
		return fmt.Errorf("name the moment to go back to: %s restore <app> --to 1h. %s backups lists them", build.Name, build.Name)
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	moment, at := restoreTarget(*to, time.Now())
	what := "the moment " + moment
	if at != nil {
		what = at.Format("2006-01-02 15:04") + " UTC"
	}
	// The control plane checks the name too: --yes is not enough.
	if *typed == "" {
		if !isTerminal(stdin) {
			return fmt.Errorf("a restore replaces the data of %s: confirm it with --confirm %s", name, name)
		}
		err = confirmName(stdin, stdout, false, fmt.Sprintf("Put the database of %s back to %s? It replaces what is there now; %s keeps the data of now beside it, so you can undo this.", name, what, build.Name), name)
		if err != nil {
			return err
		}
		*typed = name
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	step(stdout, "restoring %s: building it beside the live database, then one switch", name)
	err = api.Restore(ctx, name, *database, moment, at, *typed)
	if err != nil {
		return err
	}
	done(stdout, "Restored. %s runs again. Undo it with %s restore %s --to the newest undo- moment of %s backups.", name, build.Name, name, build.Name)
	return nil
}

// restoreTarget reads where a restore goes: a time, to the minute, or
// else a moment's ID or label.
func restoreTarget(to string, now time.Time) (moment string, at *time.Time) {
	parsed, err := parseTime(to, now)
	if err != nil {
		return strings.TrimSpace(to), nil
	}
	parsed = parsed.Truncate(time.Minute)
	return "", &parsed
}

// parseTime reads a time in UTC: RFC 3339, or a date and time, or a time
// of today.
func parseTime(text string, now time.Time) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04", "15:04:05", "15:04"} {
		parsed, err := time.Parse(layout, strings.TrimSpace(text))
		if err != nil {
			continue
		}
		// A time without a day is today in UTC, as in the TUI.
		if parsed.Year() == 0 {
			now = now.UTC()
			parsed = time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.UTC)
		}
		return parsed.UTC(), nil
	}
	return time.Time{}, errors.New("not a time")
}

func printBackups(w io.Writer, app string, list client.AppBackups, now time.Time) {
	for index, database := range list.Databases {
		if index > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, style.Bold.Render(database.Database))
		checked := "not checked yet"
		if database.CheckedAt != nil {
			checked = "checked " + human.Ago(*database.CheckedAt, now)
		}
		switch {
		case database.Problem != "":
			fmt.Fprintln(w, "  "+style.Danger.Render(style.SymbolFailed+" "+database.Problem))
		case database.CopiedAt == nil:
			fmt.Fprintln(w, "  "+style.Muted.Render("starting: no copy yet"))
		default:
			fmt.Fprintln(w, "  "+style.Success.Render(style.SymbolDone+" last copy "+human.Ago(*database.CopiedAt, now)+" · "+checked))
		}
		if len(database.Moments) == 0 {
			fmt.Fprintln(w, style.Muted.Render("  nothing to go back to yet"))
			continue
		}
		rows := [][]string{}
		for _, moment := range database.Moments {
			rows = append(rows, []string{moment.ID, moment.Label, moment.At.UTC().Format("Jan 2 15:04") + " UTC"})
		}
		table(w, []string{"MOMENT", "GO BACK TO", "AT"}, rows)
	}
	fmt.Fprintln(w)
	every := "every change of SQLite off"
	if list.EveryChange {
		every = "every change of SQLite on"
	}
	if list.Bucket != "" {
		fmt.Fprintln(w, style.Muted.Render("bucket: "+list.Bucket+", copied "+everyText(list.EveryMinutes)+", kept "+keepText(list.KeepDays)+", "+every))
	}
	hint(w, "Go back:", build.Name+" restore "+app+" --to <moment>, or a time like 14:01 UTC")
	hint(w, "How often:", build.Name+" backups "+app+" --every 15m|1h|1d --keep 1d|3d|7d|30d")
	hint(w, "A copy:", build.Name+" backup download "+app)
}
