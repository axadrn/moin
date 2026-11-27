package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/protocol"
)

// Backups, and the parts of a server's config the CLI changes: the bucket,
// the alerts and the watchers. Each command runs `moin here …` on the
// server over SSH. What it sends, like a secret key, goes as JSON on
// stdin, never on the command line; what comes back is JSON on stdout.

// sendJSON runs `moin here args…` on server with value as JSON on stdin,
// for a command that answers nothing.
func sendJSON(ctx context.Context, server string, value any, args ...string) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = sshOutput(ctx, server, hereLine(args...), bytes.NewReader(body))
	return err
}

// shownCopies is how many copies of a database `moin backups` lists.
const shownCopies = 10

// backups is `moin backups [app]`: the copies of the app's databases,
// the last daily check and the copy it proved.
func backups(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("backups", "backups [app] [-s server] [--json]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		var databases []protocol.DatabaseBackup
		if err := sshJSON(ctx, server, []string{"backups", app}, nil, &databases); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, databases)
		}
		if len(databases) == 0 {
			fmt.Fprintln(stdout, app+" has no database moin can copy: no SQLite file on a volume, no Postgres service.")
			return nil
		}
		for _, database := range databases {
			printBackup(stdout, app, database, time.Now())
		}
		return nil
	})
}

// printBackup shows how one database is backed up.
func printBackup(stdout io.Writer, app string, database protocol.DatabaseBackup, now time.Time) {
	fmt.Fprintln(stdout, style.Bold.Render(database.Database))
	if database.Error != "" {
		fmt.Fprintln(stdout, "  "+style.Danger.Render("✗")+" "+database.Error)
	}
	fmt.Fprintln(stdout, "  "+checkLine(database, now))
	rows := [][]string{}
	for index, copy := range database.Copies {
		if index == shownCopies {
			rows = append(rows, []string{style.Muted.Render(fmt.Sprintf("and %d older, see --json", len(database.Copies)-shownCopies)), "", ""})
			break
		}
		rows = append(rows, []string{copy.ID, human.Ago(copy.At, now), human.Size(float64(copy.Size))})
	}
	for _, undo := range database.Undos {
		rows = append(rows, []string{"undo-" + undo.ID, human.Ago(undo.At, now), "before a restore"})
	}
	if len(rows) > 0 {
		table(stdout, []string{"COPY", "TAKEN", "SIZE"}, rows)
		hint(stdout, "Go back to one with", build.Name+" restore "+app+" --to <copy>")
	}
	fmt.Fprintln(stdout)
}

// checkLine says how the last daily check of a database went.
func checkLine(database protocol.DatabaseBackup, now time.Time) string {
	switch {
	case database.CheckedAt == nil:
		return style.Muted.Render("No restore check yet. The newest copy is restored and read once a day.")
	case database.CheckError != "":
		return style.Danger.Render("✗") + " Restore check " + human.Ago(*database.CheckedAt, now) + ": " + database.CheckError
	}
	return style.Success.Render("✓") + " Restore check " + human.Ago(*database.CheckedAt, now) + ": the copy " + database.CheckedCopy + " restored and read fine"
}

// backUp is `moin backup [app]`: a copy of the app's databases now.
func backUp(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("backup", "backup [app] [-s server] [--json]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		var copied []daemon.Copied
		if err := sshJSON(ctx, server, []string{"backup", app}, nil, &copied); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, copied)
		}
		for _, copy := range copied {
			done(stdout, "Copied %s of %s: %s", copy.Database, app, copy.Copy)
		}
		return nil
	})
}

// restore is `moin restore [app] --to <copy|time|undo-…>`: a database goes
// back, and what it was before stays as an undo.
func restore(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("restore", "restore [app] --to <copy | 2026-11-27T14:07 | undo-…> [--database <name>] [-s server] [--yes] [--json]").withServer().withYes().withJSON()
	to := flags.String("to", "", "a copy, a time in UTC, or an undo: see `"+build.Name+" backups`")
	database := flags.String("database", "", "the database, when the app has several")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	if *to == "" {
		return fmt.Errorf("say what to go back to with --to. See the copies with `%s backups %s`", build.Name, app)
	}
	question := fmt.Sprintf("Put the data of %s back to %s? The app stops for a moment.", app, *to)
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		var restored daemon.Restored
		if err := sshJSON(ctx, server, []string{"restore", app, *to, *database}, nil, &restored); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, restored)
		}
		done(stdout, "%s of %s is back at %s", restored.Database, app, restored.To)
		hint(stdout, "Undo it with", build.Name+" restore "+app+" --to "+restored.Undo)
		return nil
	})
}

// bucket is `moin bucket <server>` and `moin bucket set <server> …`.
func bucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "set" {
		return setBucket(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("bucket", "bucket <server> [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	var bucket *daemon.Bucket
	if err := sshJSON(ctx, server, []string{"bucket"}, nil, &bucket); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, bucket)
	}
	if bucket == nil {
		fmt.Fprintln(stdout, server+" has no bucket yet, so nothing is backed up.")
		hint(stdout, "Set one with", build.Name+" bucket set "+server+" --endpoint <url> --bucket <name> --access-key <key>")
		return nil
	}
	table(stdout, []string{"BUCKET", "ENDPOINT", "REGION", "PATH", "ACCESS KEY"}, [][]string{{bucket.Bucket, bucket.Endpoint, bucket.Region, bucket.Path, bucket.AccessKey}})
	return nil
}

// setBucket is `moin bucket set <server> …`. It asks for the secret key,
// and the server keeps the bucket only when a test file went in and out.
func setBucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("bucket set", "bucket set <server> --bucket <name> --access-key <key> [--endpoint <url>] [--region <region>] [--path <folder>]")
	var bucket daemon.Bucket
	flags.StringVar(&bucket.Endpoint, "endpoint", "", "the S3 endpoint, like https://s3.example.com; none for AWS")
	flags.StringVar(&bucket.Region, "region", "", "the region; default us-east-1")
	flags.StringVar(&bucket.Bucket, "bucket", "", "the bucket's name")
	flags.StringVar(&bucket.Path, "path", "", "a folder in the bucket for moin")
	flags.StringVar(&bucket.AccessKey, "access-key", "", "the access key ID")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	if bucket.Bucket == "" || bucket.AccessKey == "" {
		return flags.wrong()
	}
	if isTerminal(stdin) {
		fmt.Fprint(stdout, "Secret key: ")
	}
	bucket.SecretKey, err = readSecret(stdin)
	if isTerminal(stdin) {
		fmt.Fprintln(stdout)
	}
	if err != nil {
		return err
	}
	if bucket.SecretKey == "" {
		return errors.New("the secret key is empty: type it, or pipe it in")
	}
	if err := sendJSON(ctx, server, bucket, "bucket", "set"); err != nil {
		return err
	}
	done(stdout, "%s backs up to %s. The first copies come within a minute", server, bucket.Bucket)
	hint(stdout, "See them with", build.Name+" backups "+server)
	return nil
}

// alerts is `moin alerts <server>` and `moin alerts add|rm|test <server> …`.
func alerts(ctx context.Context, args []string, stdout io.Writer) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "add":
		return addAlert(ctx, args[1:], stdout)
	case "rm":
		return removeAlert(ctx, args[1:], stdout)
	case "test":
		return testAlerts(ctx, args[1:], stdout)
	}
	flags := newFlags("alerts", "alerts <server> [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	var names []string
	if err := sshJSON(ctx, server, []string{"alerts"}, nil, &names); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, names)
	}
	if len(names) == 0 {
		fmt.Fprintln(stdout, server+" alerts nobody yet: what goes wrong only shows in its log.")
		hint(stdout, "Add one with", build.Name+" alerts add "+server+" telegram <bot-token> <chat-id>")
		return nil
	}
	rows := [][]string{}
	for index, name := range names {
		rows = append(rows, []string{strconv.Itoa(index + 1), name})
	}
	table(stdout, []string{"#", "ALERTS GO TO"}, rows)
	return nil
}

// addAlert is `moin alerts add <server> telegram <token> <chat-id>`, or
// slack, discord or webhook with a URL. The server sends a test alert
// first and keeps the destination only when that went out.
func addAlert(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("alerts add", "alerts add <server> telegram <bot-token> <chat-id> | slack|discord|webhook <url>")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() < 3 {
		return flags.wrong()
	}
	server := flags.Arg(0)
	destination := alert.Destination{Kind: flags.Arg(1)}
	switch {
	case destination.Kind == "telegram" && flags.NArg() == 4:
		destination.Token, destination.ChatID = flags.Arg(2), flags.Arg(3)
	case destination.Kind != "telegram" && flags.NArg() == 3:
		destination.URL = flags.Arg(2)
	default:
		return flags.wrong()
	}
	if err := sendJSON(ctx, server, destination, "alerts", "add"); err != nil {
		return err
	}
	done(stdout, "A test alert went out. %s alerts %s from now on", server, destination.Kind)
	return nil
}

// removeAlert is `moin alerts rm <server> <n>`, n as `moin alerts` lists it.
func removeAlert(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("alerts rm", "alerts rm <server> <n>")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	if err := sshRun(ctx, flags.Arg(0), hereLine("alerts", "rm", flags.Arg(1)), nil, io.Discard, os.Stderr); err != nil {
		return err
	}
	done(stdout, "Removed alert %s from %s", flags.Arg(1), flags.Arg(0))
	return nil
}

// testAlerts is `moin alerts test <server>`: a test alert to everyone.
func testAlerts(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("alerts test", "alerts test <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	if err := sshRun(ctx, server, hereLine("alerts", "test"), nil, io.Discard, os.Stderr); err != nil {
		return err
	}
	done(stdout, "A test alert went out from %s", server)
	return nil
}

// watchers is `moin watchers <server>` and `moin watchers add|rm <server> …`.
func watchers(ctx context.Context, args []string, stdout io.Writer) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "add":
		return addWatcher(ctx, args[1:], stdout)
	case "rm":
		return removeWatcher(ctx, args[1:], stdout)
	}
	flags := newFlags("watchers", "watchers <server> [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	var addresses []string
	if err := sshJSON(ctx, server, []string{"watchers"}, nil, &addresses); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, addresses)
	}
	if len(addresses) == 0 {
		fmt.Fprintln(stdout, server+" reports to no watcher: nobody notices when it goes away.")
		hint(stdout, "Run one on another machine with", build.Name+" watcher")
		return nil
	}
	rows := [][]string{}
	for _, address := range addresses {
		rows = append(rows, []string{address})
	}
	table(stdout, []string{"REPORTS TO"}, rows)
	return nil
}

// addWatcher is `moin watchers add <server> <url> <token>`.
func addWatcher(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("watchers add", "watchers add <server> <url> <token>")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 3 {
		return flags.wrong()
	}
	watcher := daemon.Watcher{URL: flags.Arg(1), Token: flags.Arg(2)}
	if err := sendJSON(ctx, flags.Arg(0), watcher, "watchers", "add"); err != nil {
		return err
	}
	done(stdout, "%s reports to %s every minute", flags.Arg(0), watcher.URL)
	return nil
}

// removeWatcher is `moin watchers rm <server> <url>`.
func removeWatcher(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("watchers rm", "watchers rm <server> <url>")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	if err := sshRun(ctx, flags.Arg(0), hereLine("watchers", "rm", flags.Arg(1)), nil, io.Discard, os.Stderr); err != nil {
		return err
	}
	done(stdout, "%s no longer reports to %s", flags.Arg(0), flags.Arg(1))
	return nil
}

// hereBackups is the server's side of the commands above: `moin here
// backups|backup|restore|bucket|alerts|watchers …`. It reads what the CLI
// sent as JSON from stdin, writes its answer as JSON to stdout and what it
// says to people to stderr.
func hereBackups(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	dir := daemon.StateDir()
	// rest are the words after the first, always three, so a missing one
	// is "" and not a crash.
	rest := append(append([]string{}, args[1:]...), "", "", "")
	switch args[0] {
	case "backups":
		databases, err := daemon.BackupsHere(ctx, dir, rest[0])
		return respond(stdout, databases, err)
	case "backup":
		copied, err := daemon.BackupHere(ctx, dir, rest[0], os.Stderr)
		return respond(stdout, copied, err)
	case "restore":
		restored, err := daemon.RestoreHere(ctx, dir, rest[0], rest[2], rest[1], os.Stderr)
		return respond(stdout, restored, err)
	case "bucket":
		return hereBucket(ctx, rest[0], stdin, stdout)
	case "alerts":
		return hereAlerts(ctx, rest[0], rest[1], stdin, stdout)
	case "watchers":
		return hereWatchers(rest[0], rest[1], stdin, stdout)
	}
	return fmt.Errorf("unknown: %s here %s", build.Name, args[0])
}

// hereBucket is `moin here bucket` and `moin here bucket set`.
func hereBucket(ctx context.Context, verb string, stdin io.Reader, stdout io.Writer) error {
	if verb == "" {
		bucket, err := daemon.BucketHere()
		return respond(stdout, bucket, err)
	}
	var bucket daemon.Bucket
	if err := json.NewDecoder(stdin).Decode(&bucket); err != nil {
		return fmt.Errorf("read the bucket: %w", err)
	}
	return daemon.SetBucketHere(ctx, bucket)
}

// hereAlerts is `moin here alerts`, and add, rm <n> and test.
func hereAlerts(ctx context.Context, verb, number string, stdin io.Reader, stdout io.Writer) error {
	switch verb {
	case "add":
		var destination alert.Destination
		if err := json.NewDecoder(stdin).Decode(&destination); err != nil {
			return fmt.Errorf("read the alert: %w", err)
		}
		return daemon.AddAlertHere(ctx, destination)
	case "rm":
		n, _ := strconv.Atoi(number)
		return daemon.RemoveAlertHere(n)
	case "test":
		return daemon.TestAlertsHere(ctx)
	}
	names, err := daemon.AlertsHere()
	return respond(stdout, names, err)
}

// hereWatchers is `moin here watchers`, and add and rm <url>.
func hereWatchers(verb, address string, stdin io.Reader, stdout io.Writer) error {
	switch verb {
	case "add":
		var watcher daemon.Watcher
		if err := json.NewDecoder(stdin).Decode(&watcher); err != nil {
			return fmt.Errorf("read the watcher: %w", err)
		}
		return daemon.AddWatcherHere(watcher)
	case "rm":
		return daemon.RemoveWatcherHere(address)
	}
	addresses, err := daemon.WatchersHere()
	return respond(stdout, addresses, err)
}
