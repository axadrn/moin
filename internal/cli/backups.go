package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"github.com/axadrn/ops/protocol"
)

// backups lists an app's backups, or manages the bucket and the snapshot
// before changes.
func backups(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "bucket" {
		return bucket(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("backups", "backups [app], or backups auto on|off [app]").withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.Arg(0) == "auto" {
		if flags.NArg() > 3 || (flags.Arg(1) != "on" && flags.Arg(1) != "off") {
			return fmt.Errorf("usage: `%s backups auto on|off [app]`", build.Name)
		}
		name, err := flags.app(flags.Arg(2))
		if err != nil {
			return err
		}
		api, err := apiClient()
		if err != nil {
			return err
		}
		err = api.SetSnapshotBeforeChanges(ctx, name, flags.Arg(1) == "on")
		if err != nil {
			return err
		}
		done(stdout, "Snapshot before every change of %s: %s.", name, flags.Arg(1))
		return nil
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
	printBackups(stdout, list)
	return nil
}

// backUp takes a snapshot now.
func backUp(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("backup", "backup [app]").withApp()
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
	list, err := api.BackUp(ctx, name)
	if err != nil {
		return err
	}
	for _, database := range list.Databases {
		if len(database.Snapshots) > 0 {
			done(stdout, "Snapshot %s of %s", database.Snapshots[0].ID, database.Database)
		}
	}
	return nil
}

// restore puts a database back from a snapshot or to a point in time.
func restore(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("restore", `restore [--app <app>] [--db <path>] <snapshot> [--bucket <bucket>] [--yes], or restore --at "2026-10-31 14:02" [--yes]`).withApp().withYes()
	database := flags.String("db", "", "database path in the container; needed when the app has several")
	at := flags.String("at", "", `point in time in UTC, like "2026-10-31 14:02:00", or "14:02" for today`)
	from := flags.String("bucket", "", "the bucket of the snapshot, when "+build.Name+" backups shows it in an older bucket the app left")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	snapshot := flags.Arg(0)
	if (snapshot == "") == (*at == "") {
		return fmt.Errorf("name a snapshot or a time: %s restore <snapshot> or %s restore --at \"2026-10-31 14:02:00\"", build.Name, build.Name)
	}
	if snapshot != "" && !protocol.ValidSnapshotID(snapshot) {
		return fmt.Errorf("%q is not a snapshot ID. Copy one from `%s backups`, like 20261031T140200Z-daily", snapshot, build.Name)
	}
	var when *time.Time
	if *at != "" {
		parsed, err := parseTime(*at)
		if err != nil {
			return err
		}
		when = &parsed
	}
	name, err := flags.app("")
	if err != nil {
		return err
	}
	target := "snapshot " + snapshot
	if when != nil {
		target = when.Format("2006-01-02 15:04:05") + " UTC"
	}
	err = confirmName(stdin, stdout, flags.yes(), fmt.Sprintf("Put the database of %s back to %s? It replaces what is there now; %s takes a snapshot of it first.", name, target, build.Name), name)
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	step(stdout, "restoring %s; %s takes a snapshot of the current state first", name, build.Name)
	err = api.Restore(ctx, name, *database, snapshot, *from, when)
	if err != nil {
		return err
	}
	done(stdout, "Restored. %s runs again.", name)
	return nil
}

// parseTime reads a time in UTC: RFC 3339, or a date and time.
func parseTime(text string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02T15:04", "15:04:05", "15:04"} {
		parsed, err := time.Parse(layout, strings.TrimSpace(text))
		if err != nil {
			continue
		}
		// A time without a day is today in UTC, as in the TUI.
		if parsed.Year() == 0 {
			now := time.Now().UTC()
			parsed = time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.UTC)
		}
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot read %q as a time: use \"2026-10-31 14:02:00\" or \"14:02\" for today, in UTC", text)
}

// bucket shows, sets or removes the bucket backups go to. It is the same
// for every app of the workspace.
func bucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	command := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	usage := map[string]string{
		"set": "backups bucket set --endpoint https://… --bucket <bucket> --access-key <key> [--region auto] [--path <folder>], then the secret key on stdin",
		"rm":  "backups bucket rm [--yes]",
	}[command]
	if usage == "" {
		usage = "backups bucket"
	}
	flags := newFlags("backups bucket "+command, usage)
	// Only set takes the bucket's flags, only rm --yes.
	var endpoint, region, name, folder, accessKey *string
	switch command {
	case "set":
		endpoint = flags.String("endpoint", "", "https:// URL of an S3-compatible store; empty for AWS S3")
		region = flags.String("region", "", "region; auto for Cloudflare R2")
		name = flags.String("bucket", "", "bucket name")
		folder = flags.String("path", "", "a folder inside the bucket, only if it holds other files too")
		accessKey = flags.String("access-key", "", "access key ID")
	case "rm":
		flags.withYes()
	}
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	switch command {
	case "show", "ls":
		storage, err := api.BackupStorage(ctx)
		if err != nil {
			return err
		}
		endpoint := storage.Endpoint
		if endpoint == "" {
			endpoint = "AWS S3"
		}
		table(stdout, []string{"BUCKET", "FOLDER", "ENDPOINT", "REGION", "ACCESS KEY"}, [][]string{{storage.Bucket, storage.Folder, endpoint, storage.Region, storage.AccessKey}})
		return nil
	case "set":
		if *name == "" || *accessKey == "" {
			return fmt.Errorf("give --bucket and --access-key; the secret key comes on stdin: %s backups bucket set --endpoint https://… --bucket <your-bucket> --access-key …", build.Name)
		}
		if isTerminal(stdin) {
			fmt.Fprint(stdout, "Secret key: ")
		}
		secret, err := readSecret(stdin)
		if isTerminal(stdin) {
			fmt.Fprintln(stdout)
		}
		if err != nil {
			return err
		}
		if secret == "" {
			return errors.New("the secret key is empty")
		}
		_, err = api.SetBackupStorage(ctx, client.BackupStorage{Endpoint: *endpoint, Region: *region, Bucket: *name, Path: *folder, AccessKey: *accessKey, SecretKey: secret})
		if err != nil {
			return err
		}
		done(stdout, "Backups go to %s now, for every app.", *name)
		return nil
	case "rm":
		storage, err := api.BackupStorage(ctx)
		if err != nil {
			return err
		}
		err = confirmName(stdin, stdout, flags.yes(), "Stop backups of every app? What is in the bucket stays.", storage.Bucket)
		if err != nil {
			return err
		}
		err = api.RemoveBackupStorage(ctx)
		if err != nil {
			return err
		}
		done(stdout, "Backups stopped. What is in the bucket stays.")
		return nil
	}
	return fmt.Errorf("unknown command %q: use %s backups bucket, bucket set or bucket rm", command, build.Name)
}

func printBackups(w io.Writer, list client.AppBackups) {
	now := time.Now()
	for index, database := range list.Databases {
		if index > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, style.Bold.Render(database.Database))
		switch {
		case database.Error != "":
			fmt.Fprintln(w, "  "+style.Danger.Render(style.SymbolFailed+" "+database.Error))
		case database.VerifiedAt != nil:
			fmt.Fprintln(w, "  "+style.Success.Render(style.SymbolDone+" verified by restore "+since(*database.VerifiedAt, now)))
		}
		if database.Since != nil && database.ReplicatedAt != nil {
			fmt.Fprintf(w, "  point in time %s to %s UTC\n", database.Since.UTC().Format("2006-01-02 15:04:05"), database.ReplicatedAt.UTC().Format("2006-01-02 15:04:05"))
		}
		if database.Note != "" {
			fmt.Fprintln(w, style.Muted.Render("  "+database.Note))
		}
		rows := [][]string{}
		for _, snapshot := range database.Snapshots {
			rows = append(rows, []string{snapshot.ID, snapshot.Reason, byteSize(snapshot.Size), since(snapshot.CreatedAt, now), dash(snapshot.Bucket)})
		}
		if len(rows) == 0 {
			fmt.Fprintln(w, style.Muted.Render("  no snapshots yet"))
			continue
		}
		table(w, []string{"SNAPSHOT", "REASON", "SIZE", "TAKEN", "BUCKET"}, rows)
	}
	auto := "on"
	if !list.SnapshotBeforeChanges {
		auto = "off"
	}
	fmt.Fprintln(w)
	if list.Bucket != "" {
		fmt.Fprintln(w, style.Muted.Render("bucket: "+list.Bucket))
	}
	if len(list.OlderBuckets) > 0 {
		fmt.Fprintln(w, style.Muted.Render("older backups in "+strings.Join(list.OlderBuckets, ", ")+": restore them with "+build.Name+" restore <snapshot> --bucket <bucket>"))
	}
	fmt.Fprintln(w, style.Muted.Render("snapshot before every change: "+auto))
}

func since(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

func byteSize(bytes int64) string {
	switch {
	case bytes < 1<<10:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
}
