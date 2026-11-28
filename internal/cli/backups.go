package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"

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

// backups is `moin backups [app]`: the copies of the app's databases, in
// the bucket and at the watchers that take copies (restore.go), the last
// daily check and the copy it proved.
func backups(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("backups", "backups [app] [--from <server>] [-s server] [--json]").withServer().withJSON()
	from := flags.String("from", "", "see the copies another server made, like one that is gone")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		databases, err := listBackups(ctx, server, app, *from, os.Stderr)
		if err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, databases)
		}
		if len(databases) == 0 {
			fmt.Fprintln(stdout, app+" has no database "+build.Name+" can copy: no SQLite file on a volume, no Postgres service.")
			return nil
		}
		copies := false
		for _, database := range databases {
			printBackup(stdout, database, time.Now())
			copies = copies || len(database.Copies) > 0
		}
		if copies {
			hint(stdout, "Go back to one with", build.Name+" restore "+app+" --to <copy>")
		}
		return nil
	})
}

// appBackups is how the databases of app are backed up: the copies, the
// undos and the last check of each.
func appBackups(ctx context.Context, server, app string, stderr io.Writer) ([]protocol.DatabaseBackup, error) {
	return backupsFrom(ctx, server, app, "", stderr)
}

// backupsFrom is appBackups with the copies another server left in the
// bucket, like one that is gone; "" is this server.
func backupsFrom(ctx context.Context, server, app, from string, stderr io.Writer) ([]protocol.DatabaseBackup, error) {
	var databases []protocol.DatabaseBackup
	err := sshAnswer(ctx, server, []string{"backups", app, "--from", from}, nil, stderr, &databases)
	return databases, err
}

// printBackup shows how one database is backed up: the last check and
// the copies, where each is, the one the check restored marked.
func printBackup(stdout io.Writer, database databaseBackups, now time.Time) {
	fmt.Fprintln(stdout, style.Bold.Render(database.Database))
	if database.Error != "" {
		fmt.Fprintln(stdout, "  "+style.Danger.Render(style.SymbolFailed)+" "+commands(stdout, sentence(database.Error)))
	}
	fmt.Fprintln(stdout, "  "+checkLine(database.DatabaseBackup, now))
	if database.CloudError != "" {
		fmt.Fprintln(stdout, "  "+style.Danger.Render(style.SymbolFailed)+" No watcher took the last copy: "+database.CloudError)
	}
	rows := [][]string{}
	for index, copy := range database.Copies {
		if index == shownCopies {
			rows = append(rows, []string{style.Muted.Render(fmt.Sprintf("and %d older, see --json", len(database.Copies)-shownCopies)), "", "", "", ""})
			break
		}
		size := human.Size(float64(copy.Size))
		if copy.Plain {
			size += ", not encrypted"
		}
		note := ""
		if copy.ID == database.CheckedCopy && database.CheckError == "" {
			note = badge(stdout, style.Success.Render(style.SymbolDone+" checked"), "checked")
		}
		rows = append(rows, []string{copy.ID, copy.Where, human.Ago(copy.At, now), size, note})
	}
	for _, undo := range database.Undos {
		rows = append(rows, []string{"undo-" + undo.ID, "server", human.Ago(undo.At, now), "", style.Muted.Render("before a restore")})
	}
	if len(rows) > 0 {
		fmt.Fprintln(stdout)
		table(stdout, []string{"COPY", "WHERE", "TAKEN", "SIZE", ""}, rows)
	}
	fmt.Fprintln(stdout)
}

// checkLine says how the last daily check of a database went.
func checkLine(database protocol.DatabaseBackup, now time.Time) string {
	switch {
	case database.CheckedAt == nil:
		return style.Muted.Render("No check yet. Once a day the newest copy is restored and read.")
	case database.CheckError != "" && database.CheckedCopy != "":
		return style.Danger.Render(style.SymbolFailed) + " Checked " + human.Ago(*database.CheckedAt, now) + ": " + database.CheckError + ". The last copy that restored fine: " + database.CheckedCopy
	case database.CheckError != "":
		return style.Danger.Render(style.SymbolFailed) + " Checked " + human.Ago(*database.CheckedAt, now) + ": " + database.CheckError + ". No copy restored fine yet"
	}
	return style.Success.Render(style.SymbolDone) + " Checked " + human.Ago(*database.CheckedAt, now) + ": the copy marked checked restored and read fine"
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
		copied, err := backupNow(ctx, server, app, os.Stderr)
		if err != nil {
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

// backupNow copies every database of app into the bucket now.
func backupNow(ctx context.Context, server, app string, said io.Writer) ([]daemon.Copied, error) {
	var copied []daemon.Copied
	err := sshAnswer(ctx, server, []string{"backup", app}, nil, said, &copied)
	return copied, err
}

// askBackupKey reads your backup key: typed or pasted on a terminal,
// without showing it, or one line from a pipe.
func askBackupKey(stdin io.Reader, stdout io.Writer, question string) (*age.X25519Identity, error) {
	if isTerminal(stdin) {
		fmt.Fprint(stdout, style.Warning.Render("?")+" "+question+" ")
	}
	secret, err := readSecret(stdin)
	if isTerminal(stdin) {
		fmt.Fprintln(stdout)
	}
	if err != nil {
		return nil, err
	}
	key, err := age.ParseX25519Identity(strings.TrimSpace(secret))
	if err != nil {
		return nil, errors.New("that is not a backup key: one starts with AGE-SECRET-KEY-1")
	}
	return key, nil
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

// bucket is `moin bucket <server>`, and `moin bucket set|key|confirm
// <server>`.
func bucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "set":
		return setBucket(ctx, args[1:], stdin, stdout)
	case "key":
		return bucketKey(ctx, args[1:], stdin, stdout)
	case "confirm":
		return bucketConfirm(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("bucket", "bucket <server> [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	var check daemon.BucketCheck
	if err := sshJSON(ctx, server, []string{"bucket", "check"}, nil, &check); err != nil {
		return err
	}
	key, err := backupKey(ctx, server)
	if err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, struct {
			daemon.BucketCheck
			BackupKey *daemon.BackupKey `json:"backup_key"`
		}{check, key})
	}
	bucket := check.Bucket
	if bucket == nil {
		fmt.Fprintln(stdout, server+" has no bucket yet, so nothing is backed up.")
		hint(stdout, "Set one with", build.Name+" bucket set "+server)
		return nil
	}
	table(stdout, []string{"BUCKET", "ENDPOINT", "REGION", "PATH", "ACCESS KEY"}, [][]string{{bucket.Bucket, bucket.Endpoint, bucket.Region, bucket.Path, bucket.AccessKey}})
	if check.Error != "" {
		fmt.Fprintln(stdout, style.Danger.Render(style.SymbolFailed)+" "+server+" could not write to the bucket just now: "+check.Error)
	} else {
		printLock(stdout, bucket.Locked)
	}
	switch {
	case key == nil:
		fmt.Fprintln(stdout, style.Danger.Render("✗")+" No backup key of yours: when "+server+" is gone, nobody can read its copies.")
		hint(stdout, "Make one with", build.Name+" bucket key "+server)
	case !key.Confirmed:
		fmt.Fprintln(stdout, style.Danger.Render("✗")+" You did not confirm your backup key yet: "+key.Public)
		hint(stdout, "Confirm it with", build.Name+" bucket confirm "+server)
	default:
		fmt.Fprintln(stdout, style.Success.Render("✓")+" Every copy is encrypted for your backup key, which you confirmed: "+key.Public)
	}
	return nil
}

// backupKey asks server for the public half of your backup key, nil when
// it has none.
func backupKey(ctx context.Context, server string) (*daemon.BackupKey, error) {
	var key *daemon.BackupKey
	err := sshJSON(ctx, server, []string{"bucket", "key"}, nil, &key)
	return key, err
}

// bucketKey is `moin bucket key <server>`: a new backup key of yours for
// the server, made here on your laptop and shown once. The server gets
// only its public half. --public takes a key you already have, like the
// one of another server, instead of making one.
func bucketKey(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("bucket key", "bucket key <server> [--public <age1…>] [-y]").withYes()
	public := flags.String("public", "", "the public half of a backup key you already have, instead of a new one")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	has, err := backupKey(ctx, server)
	if err != nil {
		return err
	}
	if has != nil {
		question := fmt.Sprintf("%s has a backup key of yours. Take another from now on? The copies made so far only open with the old one.", server)
		if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
			return err
		}
	}
	return newBackupKey(ctx, server, *public, stdin, stdout)
}

// newBackupKey gives server a backup key of yours: public, or a new one
// that is shown once. On a terminal you confirm it right away.
func newBackupKey(ctx context.Context, server, public string, stdin io.Reader, stdout io.Writer) error {
	if public == "" {
		key, err := age.GenerateX25519Identity()
		if err != nil {
			return err
		}
		public = key.Recipient().String()
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, style.Bold.Render("Your backup key for "+server+". It is shown once."))
		fmt.Fprintln(stdout, "Every copy is encrypted before it leaves the server. When the server is gone,")
		fmt.Fprintln(stdout, "only this key reads the copies. Keep it in your password manager.")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "  "+key.String())
		fmt.Fprintln(stdout)
	}
	if err := sendJSON(ctx, server, daemon.BackupKey{Public: public}, "bucket", "key", "set"); err != nil {
		return err
	}
	if !isTerminal(stdin) {
		hint(stdout, "Confirm that you have it with", build.Name+" bucket confirm "+server)
		return nil
	}
	return confirmBackupKey(ctx, server, stdin, stdout)
}

// bucketConfirm is `moin bucket confirm <server>`: you paste your backup
// key, and moin opens a fresh copy with it, here on your laptop. Until that worked, `moin` shows the server in red.
func bucketConfirm(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("bucket confirm", "bucket confirm <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	return confirmBackupKey(ctx, server, stdin, stdout)
}

// confirmBackupKey asks for your backup key and proves it: the server
// takes a copy of its smallest database now, moin fetches it as it lies
// in the bucket, and opens and reads it here with your key. A server
// without a bucket sends that copy straight from the database. Then the
// server writes down that the key is confirmed.
func confirmBackupKey(ctx context.Context, server string, stdin io.Reader, stdout io.Writer) error {
	has, err := backupKey(ctx, server)
	if err != nil {
		return err
	}
	if has == nil {
		return fmt.Errorf("%s has no backup key of yours yet: make one with `%s bucket key %s`", server, build.Name, server)
	}
	key, err := askBackupKey(stdin, stdout, "Paste your backup key, to confirm that you have it:")
	if err != nil {
		return err
	}
	if key.Recipient().String() != has.Public {
		return fmt.Errorf("this is not the backup key of %s. Its public half is %s", server, has.Public)
	}
	var copy *daemon.KeyCopy
	if err := sshJSON(ctx, server, []string{"bucket", "key", "copy"}, nil, &copy); err != nil {
		return err
	}
	if copy != nil {
		if err := fetchAndOpen(ctx, server, *copy, key); err != nil {
			return fmt.Errorf("your key did not open the copy of %s: %w", copy.Database, err)
		}
	}
	if err := sendJSON(ctx, server, daemon.BackupKey{Public: has.Public}, "bucket", "key", "confirm"); err != nil {
		return err
	}
	if copy == nil {
		done(stdout, "This is the backup key of %s. It has no database to copy yet", server)
		return nil
	}
	if copy.Copy == "" {
		done(stdout, "Your key opens the copies of %s: it read a fresh copy of %s to the end", server, copy.Database)
		return nil
	}
	done(stdout, "Your key opens the copies of %s: it read the copy %s of %s from the bucket to the end", server, copy.Copy, copy.Database)
	return nil
}

// fetchAndOpen fetches a copy from the server as it lies in the bucket,
// or a fresh one from a server without a bucket, and opens it here with
// key.
func fetchAndOpen(ctx context.Context, server string, copy daemon.KeyCopy, key age.Identity) error {
	reader, writer := io.Pipe()
	defer reader.Close()
	go func() {
		line := hereLine("bucket", "key", "send", copy.App, "--database", copy.Database, "--copy", copy.Copy)
		writer.CloseWithError(sshRun(ctx, server, line, nil, writer, os.Stderr))
	}()
	return openCopy(reader, copy.Postgres, key)
}

// The first bytes of what a copy holds: a SQLite file, or a pg_dump in
// its custom format.
const (
	sqliteStart   = "SQLite format 3\x00"
	postgresStart = "PGDMP"
)

// openCopy decrypts a copy with key and reads it to its end: a gzipped
// SQLite file, or a dump of Postgres. Every part of it is checked on the
// way, so a copy that is cut short or changed does not pass.
func openCopy(encrypted io.Reader, postgres bool, key age.Identity) error {
	plain, err := daemon.DecryptCopy(encrypted, key)
	if err != nil {
		return err
	}
	start := postgresStart
	if !postgres {
		start = sqliteStart
		if plain, err = gzip.NewReader(plain); err != nil {
			return fmt.Errorf("what it holds is not a database: %w", err)
		}
	}
	first := make([]byte, len(start))
	if _, err := io.ReadFull(plain, first); err != nil {
		return fmt.Errorf("it ends before a database starts: %w", err)
	}
	if string(first) != start {
		return errors.New("what it holds is not a database")
	}
	_, err = io.Copy(io.Discard, plain)
	return err
}

// hereBucketKey is `moin here bucket key …`, the server's side of your
// backup key: `key` answers its public half, `key set` and `key confirm`
// read one from stdin, `key copy` takes the copy that proves it, and
// `key send` writes that copy to stdout as it lies in the bucket, or a
// fresh one when --copy names none.
func hereBucketKey(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	switch verb {
	case "":
		key, err := daemon.BackupKeyHere()
		return respond(stdout, key, err)
	case "set", "confirm":
		var key daemon.BackupKey
		if err := json.NewDecoder(stdin).Decode(&key); err != nil {
			return fmt.Errorf("read the key: %w", err)
		}
		if verb == "confirm" {
			return daemon.ConfirmBackupKeyHere(key.Public)
		}
		return daemon.SetBackupKeyHere(key.Public)
	case "copy":
		copy, err := daemon.KeyCopyHere(ctx, daemon.StateDir(), os.Stderr)
		return respond(stdout, copy, err)
	case "send":
		flags := newFlags("here bucket key send", "here bucket key send <app> --database <name> [--copy <id>]")
		database := flags.String("database", "", "the database")
		copy := flags.String("copy", "", "the copy")
		app, err := hereApp(flags, args[1:])
		if err != nil {
			return err
		}
		// stdout is raw here (see here): the copy leaves byte for byte.
		return daemon.SendCopyHere(ctx, daemon.StateDir(), daemon.KeyCopy{App: app, Database: *database, Copy: *copy}, stdout)
	}
	return errors.New("usage: here bucket key [set|confirm|copy|send]")
}

// setBucket is `moin bucket set <server>`. On a terminal it asks for what
// no flag gave, and always for the secret key, which a script pipes in.
// The server keeps the bucket only when a test file went in and out. A
// server without a backup key of yours gets one, shown once (newBackupKey).
func setBucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("bucket set", "bucket set <server> [--bucket <name>] [--access-key <key>] [--endpoint <url>] [--region <region>] [--path <folder>]")
	var bucket daemon.Bucket
	flags.StringVar(&bucket.Endpoint, "endpoint", "", "the S3 endpoint, like https://s3.example.com; none for AWS")
	flags.StringVar(&bucket.Region, "region", "", "the region; default us-east-1")
	flags.StringVar(&bucket.Bucket, "bucket", "", "the bucket's name")
	flags.StringVar(&bucket.Path, "path", "", "a folder in the bucket for "+build.Name)
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
	body, err := json.Marshal(bucket)
	if err != nil {
		return err
	}
	var saved daemon.Bucket
	if err := sshJSON(ctx, server, []string{"bucket", "set"}, bytes.NewReader(body), &saved); err != nil {
		return err
	}
	done(stdout, "%s backs up to %s. The first copies come within a minute", server, bucket.Bucket)
	hint(stdout, "See them with", build.Name+" backups <app>")
	printLock(stdout, saved.Locked)
	// Every copy is encrypted: a server that has no backup key of yours
	// gets one now.
	has, err := backupKey(ctx, server)
	if err != nil || has != nil {
		return err
	}
	return newBackupKey(ctx, server, "", stdin, stdout)
}

// printLock says whether the bucket refused to delete a fresh object: a
// locked bucket keeps the copies, also from a hacked server. An unlocked
// one is no failure, only less safe.
func printLock(w io.Writer, locked bool) {
	if locked {
		fmt.Fprintln(w, style.Success.Render(style.SymbolDone)+" Locked: a hacked server can't delete your copies.")
		return
	}
	fmt.Fprintln(w, style.Warning.Render("!")+" Not locked: a hacked server could delete your copies.")
	fmt.Fprintln(w, "  "+style.Muted.Render("Turn on a lock: https://moin.run/docs/backups#lock-your-bucket"))
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
