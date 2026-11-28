package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// clickhouseEngine backs up a ClickHouse service with ClickHouse's own
// BACKUP and RESTORE, through docker exec of clickhouse-client. The
// daemon links no ClickHouse driver. The client logs in with the
// CLICKHOUSE_USER and CLICKHOUSE_PASSWORD of the container's own
// environment, which it reads by itself: the password never leaves the
// container.
//
// A copy is one zip with every database of the service but ClickHouse's
// own. BACKUP writes only to a file on the ClickHouse's disk, so the zip
// lies in the backups folder of its data for a moment, and moin reads it
// out and removes it. The stock config allows that folder; no disk has
// to be set up.
//
// ClickHouse cannot swap a database for another in one step, so the
// restore is the one of overLive: the live databases are kept as a zip on
// the server, the undo, and the copy is restored over them.
type clickhouseEngine struct {
	overLive
}

func newClickhouseEngine(b *backups, app string, database protocol.Database) *clickhouseEngine {
	e := &clickhouseEngine{}
	e.overLive = overLive{b: b, app: app, database: database, db: e, ending: ".zip"}
	return e
}

// clickhouseTarget is a running ClickHouse: its container and the exact
// image it runs.
type clickhouseTarget struct {
	container string
	image     string
}

// clickhouseOwn are the databases ClickHouse keeps for itself, as a list
// in SQL.
const clickhouseOwn = `'system', 'INFORMATION_SCHEMA', 'information_schema'`

func (e *clickhouseEngine) target(ctx context.Context) (clickhouseTarget, error) {
	container, image, err := e.b.apps.serviceContainer(ctx, e.app, e.database.Service, "ClickHouse")
	return clickhouseTarget{container: container, image: image}, err
}

// sql runs statements in the container and returns their rows, tabs
// between the columns. A failure carries the number and the name of the
// error (clickhouseError); all of stderr goes to the server's log only.
func (t clickhouseTarget) sql(ctx context.Context, statements string) (string, error) {
	// The database system is always there, also while the app's are gone.
	// No table is too big to drop for a restore.
	command := exec.CommandContext(ctx, "docker", "exec", "-i", t.container, "clickhouse-client", "--database", "system", "--max_table_size_to_drop", "0", "--multiquery")
	var stdout, stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = strings.NewReader(statements), &stdout, &stderr
	if err := command.Run(); err != nil {
		slog.Error("clickhouse: a statement failed", "stderr", stderr.String())
		if message := clickhouseError(stderr.String()); message != "" {
			return "", errors.New(message)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// answers says the ClickHouse takes a statement now. It logs nothing: a
// ClickHouse that starts does not answer for a while.
func (t clickhouseTarget) answers(ctx context.Context) bool {
	return exec.CommandContext(ctx, "docker", "exec", t.container, "clickhouse-client", "--database", "system", "-q", "SELECT 1").Run() == nil
}

var clickhouseErrorLine = regexp.MustCompile(`Code: (\d+)\..*\(([A-Z_]+)\)`)

// clickhouseError turns the client's stderr into one line: the number and
// the name of the first error. The text between them may quote the app's
// data, so none of it lands in an alert or a report.
func clickhouseError(stderr string) string {
	found := clickhouseErrorLine.FindStringSubmatch(stderr)
	switch {
	case found == nil:
		return ""
	case found[2] == "AUTHENTICATION_FAILED":
		return "ClickHouse did not let moin in: moin logs in with the CLICKHOUSE_USER and CLICKHOUSE_PASSWORD of the service"
	}
	return "ClickHouse error " + found[1] + " (" + found[2] + ")"
}

// clickhouseQuoted is a name as ClickHouse takes it in a statement.
func clickhouseQuoted(name string) string {
	return "`" + strings.NewReplacer(`\`, `\\`, "`", "\\`").Replace(name) + "`"
}

// backupFile is a new zip in the folder ClickHouse may back up to: its
// name for BACKUP and RESTORE, and where it lies in the container.
func (t clickhouseTarget) backupFile(ctx context.Context) (name, file string, err error) {
	disk, err := t.sql(ctx, `SELECT path FROM system.disks WHERE name = 'default';`)
	if err != nil {
		return "", "", err
	}
	name = tempName(strings.ToLower(rand.Text()) + ".zip")
	return name, strings.TrimSuffix(disk, "/") + "/backups/" + name, nil
}

// run runs a command in the container with stdin, writing stdout to
// stdout.
func (t clickhouseTarget) run(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	command := exec.CommandContext(ctx, "docker", append([]string{"exec", "-i", t.container}, args...)...)
	var stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s in the ClickHouse container: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (e *clickhouseEngine) Copy(ctx context.Context, w io.Writer) error { return e.dump(ctx, w) }

// dump has ClickHouse back up every database but its own into a zip on
// its disk, writes the zip to w and removes it.
func (e *clickhouseEngine) dump(ctx context.Context, w io.Writer) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	name, file, err := t.backupFile(ctx)
	if err != nil {
		return err
	}
	defer t.run(context.WithoutCancel(ctx), nil, io.Discard, "rm", "-f", file)
	if _, err := t.sql(ctx, `BACKUP ALL EXCEPT DATABASES system, INFORMATION_SCHEMA, information_schema TO File('`+name+`');`); err != nil {
		return err
	}
	return t.run(ctx, nil, w, "cat", file)
}

// load puts a zip on the ClickHouse's disk, has ClickHouse restore
// everything in it and removes the zip.
func (t clickhouseTarget) load(ctx context.Context, zip io.Reader) error {
	name, file, err := t.backupFile(ctx)
	if err != nil {
		return err
	}
	defer t.run(context.WithoutCancel(ctx), nil, io.Discard, "rm", "-f", file)
	if err := t.run(ctx, zip, io.Discard, "sh", "-c", `mkdir -p "$(dirname "$1")" && cat > "$1" && chmod a+r "$1"`, "sh", file); err != nil {
		return err
	}
	_, err = t.sql(ctx, `RESTORE ALL FROM File('`+name+`');`)
	return err
}

// databases are the databases of the ClickHouse but its own.
func (t clickhouseTarget) databases(ctx context.Context) ([]string, error) {
	out, err := t.sql(ctx, `SELECT name FROM system.databases WHERE name NOT IN (`+clickhouseOwn+`) FORMAT TSVRaw;`)
	if err != nil || out == "" {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// replace drops the app's databases and restores the ones of a zip.
func (e *clickhouseEngine) replace(ctx context.Context, zip io.Reader) error {
	t, err := e.target(ctx)
	if err == nil {
		err = waitFor(ctx, t.answers)
	}
	if err != nil {
		return err
	}
	databases, err := t.databases(ctx)
	if err != nil {
		return err
	}
	drop := ""
	for _, name := range databases {
		drop += "DROP DATABASE IF EXISTS " + clickhouseQuoted(name) + " SYNC;\n"
	}
	if _, err := t.sql(ctx, drop); err != nil {
		return err
	}
	return t.load(ctx, zip)
}

// Size is how many bytes the tables of the app's databases take on the
// disk, as ClickHouse counts them.
func (e *clickhouseEngine) Size(ctx context.Context) (int64, error) {
	t, err := e.target(ctx)
	if err != nil {
		return 0, err
	}
	out, err := t.sql(ctx, `SELECT ifNull(sum(total_bytes), 0) FROM system.tables WHERE database NOT IN (`+clickhouseOwn+`);`)
	if err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	var size int64
	if _, err := fmt.Sscan(out, &size); err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	return size, nil
}

// Check restores a copy in a throwaway container of the same image,
// which has no network, and reads every row of every table. Then it
// removes the container. Nothing of the check touches the app's own
// ClickHouse.
func (e *clickhouseEngine) Check(ctx context.Context, from source) error {
	t, err := e.target(ctx)
	if err == nil {
		// The zip lies next to what is restored from it.
		err = e.b.apps.roomForCheck(ctx, 2*from.size)
	}
	if err != nil {
		return notChecked{err}
	}
	name, remove := e.b.apps.throwawayContainer(ctx, "check", e.app, e.database.Service)
	// One a check that was cut off left.
	remove()
	defer remove()
	check := clickhouseTarget{container: name, image: t.image}
	_, err = e.b.apps.output(ctx, "docker", "run", "-d", "--name", name, "--network", "none", t.image)
	if err == nil {
		err = waitFor(ctx, check.answers)
	}
	if err != nil {
		return notChecked{fmt.Errorf("start a ClickHouse for the check: %w", err)}
	}
	if err := check.load(ctx, from.copy); err != nil {
		return err
	}
	return check.read(ctx)
}

// read reads every column of every row of the tables that hold data: the
// MergeTree and the Log families, which are what BACKUP copies.
func (t clickhouseTarget) read(ctx context.Context) error {
	out, err := t.sql(ctx, `SELECT database, name FROM system.tables WHERE database NOT IN (`+clickhouseOwn+`) AND (engine LIKE '%MergeTree' OR engine LIKE '%Log') FORMAT TSVRaw;`)
	if err != nil || out == "" {
		return err
	}
	read := ""
	for _, line := range strings.Split(out, "\n") {
		database, table, _ := strings.Cut(line, "\t")
		read += "SELECT count() FROM " + clickhouseQuoted(database) + "." + clickhouseQuoted(table) + " WHERE NOT ignore(*) FORMAT Null;\n"
	}
	_, err = t.sql(ctx, read)
	return err
}
