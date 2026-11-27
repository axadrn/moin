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
	"strings"
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
		databases, err := appBackups(ctx, server, app, os.Stderr)
		if err != nil {
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

// appBackups is how the databases of app are backed up: the copies, the
// undos and the last check of each.
func appBackups(ctx context.Context, server, app string, stderr io.Writer) ([]protocol.DatabaseBackup, error) {
	var databases []protocol.DatabaseBackup
	err := sshAnswer(ctx, server, []string{"backups", app}, nil, stderr, &databases)
	return databases, err
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

// restore is `moin restore [app]`: a database goes back to a copy, and
// what it was before stays as an undo. On a terminal it shows the copies
// and asks which one; a script names it with --to. What it asks goes to
// stderr, so stdout holds only the answer of --json.
func restore(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("restore", "restore [app] [--to <copy | 2026-11-27T14:07 | undo-…>] [--database <name>] [-s server] [-y] [--json]").withServer().withYes().withJSON()
	to := flags.String("to", "", "a copy, a time in UTC, or an undo: see `"+build.Name+" backups`")
	database := flags.String("database", "", "the database, when the app has several")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		if *to == "" {
			*database, *to, err = pickCopy(ctx, server, app, *database, stdin, os.Stderr)
			if err != nil {
				return err
			}
		}
		question := fmt.Sprintf("Put the data of %s back to %s? The app stops for a moment.", app, *to)
		if err := confirm(stdin, os.Stderr, flags.yes(), question); err != nil {
			return err
		}
		var restored daemon.Restored
		if err := sshJSON(ctx, server, []string{"restore", app, "--to", *to, "--database", *database}, nil, &restored); err != nil {
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

// pickCopy shows the copies and undos of the app's database and asks
// which one to go back to. Enter takes the newest copy. An app with
// several databases is asked which one first.
func pickCopy(ctx context.Context, server, app, name string, stdin io.Reader, stdout io.Writer) (database, to string, err error) {
	if !isTerminal(stdin) {
		return "", "", fmt.Errorf("say what to go back to with --to. See the copies with `%s backups %s`", build.Name, app)
	}
	databases, err := appBackups(ctx, server, app, os.Stderr)
	if err != nil {
		return "", "", err
	}
	chosen, err := pickDatabase(app, name, databases, stdin, stdout)
	if err != nil {
		return "", "", err
	}
	if len(chosen.Copies) == 0 {
		return "", "", fmt.Errorf("%s of %s has no copy to go back to yet", chosen.Database, app)
	}
	printBackup(stdout, app, chosen, time.Now())
	to, err = ask(stdin, stdout, "Go back to which copy?", chosen.Copies[0].ID)
	return chosen.Database, to, err
}

// pickDatabase is the database named name, the app's only one, or the
// one you pick.
func pickDatabase(app, name string, databases []protocol.DatabaseBackup, stdin io.Reader, stdout io.Writer) (protocol.DatabaseBackup, error) {
	if len(databases) == 0 {
		return protocol.DatabaseBackup{}, fmt.Errorf("%s has no database moin can copy", app)
	}
	if name == "" && len(databases) > 1 {
		for _, database := range databases {
			fmt.Fprintln(stdout, "  "+database.Database)
		}
		var err error
		name, err = ask(stdin, stdout, "Which database?", databases[0].Database)
		if err != nil {
			return protocol.DatabaseBackup{}, err
		}
	}
	for _, database := range databases {
		if database.Database == name || name == "" {
			return database, nil
		}
	}
	return protocol.DatabaseBackup{}, fmt.Errorf("%s has no database %s", app, name)
}

// ask asks one question on a terminal. Enter takes suggested, when there
// is one.
func ask(stdin io.Reader, stdout io.Writer, question, suggested string) (string, error) {
	fmt.Fprint(stdout, style.Warning.Render("?")+" "+question+" ")
	if suggested != "" {
		fmt.Fprint(stdout, style.Muted.Render("["+suggested+"]")+" ")
	}
	answer, err := readLine(stdin)
	if err != nil {
		return "", err
	}
	if answer == "" {
		return suggested, nil
	}
	return answer, nil
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
		hint(stdout, "Set one with", build.Name+" bucket set "+server)
		return nil
	}
	table(stdout, []string{"BUCKET", "ENDPOINT", "REGION", "PATH", "ACCESS KEY"}, [][]string{{bucket.Bucket, bucket.Endpoint, bucket.Region, bucket.Path, bucket.AccessKey}})
	return nil
}

// setBucket is `moin bucket set <server>`. On a terminal it asks for what
// no flag gave, and always for the secret key, which a script pipes in.
// The server keeps the bucket only when a test file went in and out.
func setBucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("bucket set", "bucket set <server> [--bucket <name>] [--access-key <key>] [--endpoint <url>] [--region <region>] [--path <folder>]")
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
	if isTerminal(stdin) {
		err = askBucket(&bucket, stdin, stdout)
	} else {
		bucket.SecretKey, err = readLine(stdin)
	}
	if err != nil {
		return err
	}
	if bucket.Bucket == "" || bucket.AccessKey == "" || bucket.SecretKey == "" {
		return errors.New("a bucket needs its name, an access key and a secret key: pass --bucket and --access-key, and pipe the secret key in")
	}
	if err := sendJSON(ctx, server, bucket, "bucket", "set"); err != nil {
		return err
	}
	done(stdout, "%s backs up to %s. The first copies come within a minute", server, bucket.Bucket)
	hint(stdout, "See them with", build.Name+" backups <app>")
	return nil
}

// askBucket asks on a terminal for what the flags left empty, and for the
// secret key without showing it.
func askBucket(bucket *daemon.Bucket, stdin io.Reader, stdout io.Writer) error {
	questions := []struct {
		value    *string
		question string
	}{
		{&bucket.Endpoint, "Endpoint, like https://s3.example.com (empty for AWS):"},
		{&bucket.Bucket, "Bucket name:"},
		{&bucket.AccessKey, "Access key:"},
	}
	for _, q := range questions {
		if *q.value != "" {
			continue
		}
		answer, err := ask(stdin, stdout, q.question, "")
		if err != nil {
			return err
		}
		*q.value = answer
	}
	fmt.Fprint(stdout, style.Warning.Render("?")+" Secret key: ")
	secret, err := readSecret(stdin)
	fmt.Fprintln(stdout)
	bucket.SecretKey = secret
	return err
}

// alerts is `moin alerts <server>` and `moin alerts add|rm|test <server> …`.
func alerts(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "add":
		return addAlert(ctx, args[1:], stdin, stdout)
	case "rm":
		return removeAlert(ctx, args[1:], stdin, stdout)
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
		hint(stdout, "Add one with", build.Name+" alerts add "+server+" telegram")
		return nil
	}
	rows := [][]string{}
	for index, name := range names {
		rows = append(rows, []string{strconv.Itoa(index + 1), name})
	}
	table(stdout, []string{"#", "ALERTS GO TO"}, rows)
	return nil
}

// addAlert is `moin alerts add <server> telegram --token … --chat-id …`,
// or slack, discord or webhook with --url. On a terminal it asks for what
// is missing. The server sends a test alert first and keeps the
// destination only when that went out.
func addAlert(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("alerts add", "alerts add <server> telegram [--token <bot-token>] [--chat-id <id>], or slack|discord|webhook [--url <url>]")
	var destination alert.Destination
	flags.StringVar(&destination.Token, "token", "", "the token of your Telegram bot")
	flags.StringVar(&destination.ChatID, "chat-id", "", "the Telegram chat the alerts go to")
	flags.StringVar(&destination.URL, "url", "", "the webhook URL of Slack, Discord or your own")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	server := flags.Arg(0)
	destination.Kind = flags.Arg(1)
	missing := map[string]*string{"--url": &destination.URL}
	if destination.Kind == "telegram" {
		missing = map[string]*string{"--token": &destination.Token, "--chat-id": &destination.ChatID}
	}
	for _, flag := range []string{"--token", "--chat-id", "--url"} {
		value := missing[flag]
		if value == nil || *value != "" {
			continue
		}
		if !isTerminal(stdin) {
			return fmt.Errorf("%s alerts need %s", destination.Kind, flag)
		}
		answer, err := ask(stdin, stdout, "The "+strings.TrimPrefix(flag, "--")+" for "+destination.Kind+":", "")
		if err != nil {
			return err
		}
		*value = answer
	}
	if err := sendJSON(ctx, server, destination, "alerts", "add"); err != nil {
		return err
	}
	done(stdout, "A test alert went out. %s alerts %s from now on", server, destination.Kind)
	return nil
}

// removeAlert is `moin alerts rm <server> <n>`, n as `moin alerts` lists it.
func removeAlert(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("alerts rm", "alerts rm <server> <n> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	server, number := flags.Arg(0), flags.Arg(1)
	if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove alert %s from %s?", number, server)); err != nil {
		return err
	}
	if err := sshRun(ctx, server, hereLine("alerts", "rm", number), nil, io.Discard, os.Stderr); err != nil {
		return err
	}
	done(stdout, "Removed alert %s from %s", number, server)
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

// watcherConnect is `moin watcher connect <server> <url> <token>`: the
// server reports to the watcher at url from now on.
func watcherConnect(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("watcher connect", "watcher connect <server> <url> <token>")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 3 {
		return flags.wrong()
	}
	server, watcher := flags.Arg(0), daemon.Watcher{URL: flags.Arg(1), Token: flags.Arg(2)}
	if err := sendJSON(ctx, server, watcher, "watchers", "connect"); err != nil {
		return err
	}
	done(stdout, "%s reports to %s every minute", server, watcher.URL)
	return nil
}

// watcherDisconnect is `moin watcher disconnect <server> <url>`: the
// server stops reporting there.
func watcherDisconnect(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("watcher disconnect", "watcher disconnect <server> <url> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	server, address := flags.Arg(0), flags.Arg(1)
	question := fmt.Sprintf("Stop the reports of %s to %s? That watcher will say the server is silent.", server, address)
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	if err := sshRun(ctx, server, hereLine("watchers", "disconnect", address), nil, io.Discard, os.Stderr); err != nil {
		return err
	}
	done(stdout, "%s no longer reports to %s", server, address)
	return nil
}
