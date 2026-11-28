package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/protocol"
)

// mysqlEngine backs up a MySQL or MariaDB service with the tools of its
// own image, through docker exec: mysqldump or mariadb-dump writes a copy,
// gzipped here, and the mysql or mariadb client loads one. The daemon
// links no MySQL driver. It logs in as root over the socket in the
// container, with the root password the container's own environment
// names: the password never leaves the container.
//
// It backs up the one database MYSQL_DATABASE or MARIADB_DATABASE names.
//
// MySQL cannot rename a database, so nothing is built beside the live
// one. The check loads a copy into a throwaway container of the same
// image. A restore dumps the live database into a file in the app's
// folder on the server, the undo, and loads the copy over the live one.
// When that fails, the undo goes back in.
type mysqlEngine struct {
	b        *backups
	app      string
	database protocol.Database
}

// mysqlTarget is a running MySQL or MariaDB: its container, the exact
// image it runs and its database. host is "" for the socket, as in the
// app's own container.
type mysqlTarget struct {
	container string
	image     string
	database  string
	host      string
}

// The two names of each tool: MariaDB's, then MySQL's. Newer MariaDB
// images have only the first, MySQL images only the second.
var (
	mysqlClient = [2]string{"mariadb", "mysql"}
	mysqlDump   = [2]string{"mariadb-dump", "mysqldump"}
)

// mysqlTool runs in the container. It takes the root password from the
// container's environment, picks the name of the tool the image has and
// runs it with the rest of the arguments.
const mysqlTool = `password="${MARIADB_ROOT_PASSWORD:-$MYSQL_ROOT_PASSWORD}"
for file in "$MARIADB_ROOT_PASSWORD_FILE" "$MYSQL_ROOT_PASSWORD_FILE"; do
	if [ -f "$file" ]; then password=$(cat "$file"); fi
done
if [ -n "$password" ]; then export MYSQL_PWD="$password"; fi
tool=$1
if ! command -v "$tool" >/dev/null; then tool=$2; fi
shift 2
exec "$tool" "$@"`

// mysqlName is a database name moin puts into SQL as it is.
var mysqlName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}$`)

// target is the running container of the engine's service, with the
// database its environment names.
func (e *mysqlEngine) target(ctx context.Context) (mysqlTarget, error) {
	a, app, service := e.b.apps, e.app, e.database.Service
	if !protocol.ValidAppName(app) || !validService(service) {
		return mysqlTarget{}, errors.New("invalid app or service name")
	}
	out, err := a.output(ctx, "docker", "ps", "-q", "--filter", "status=running",
		"--filter", "label=com.docker.compose.project="+a.projectOf(app),
		"--filter", "label=com.docker.compose.service="+service)
	if err != nil {
		return mysqlTarget{}, err
	}
	container, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if container == "" {
		return mysqlTarget{}, fmt.Errorf("the MySQL service %s of %s is not running", service, app)
	}
	inspected, err := a.output(ctx, "docker", "inspect", "-f", "{{json .Config.Env}}\n{{.Image}}", container)
	if err != nil {
		return mysqlTarget{}, err
	}
	envJSON, image, _ := strings.Cut(strings.TrimSpace(inspected), "\n")
	var env []string
	_ = json.Unmarshal([]byte(envJSON), &env)
	name := mysqlDatabase(env)
	if name == "" {
		return mysqlTarget{}, fmt.Errorf("the MySQL service %s of %s names no database: set MYSQL_DATABASE or MARIADB_DATABASE", service, app)
	}
	if !mysqlName.MatchString(name) {
		return mysqlTarget{}, fmt.Errorf("the database name of the MySQL service %s has letters moin does not take", service)
	}
	return mysqlTarget{container: container, image: strings.TrimSpace(image), database: name}, nil
}

// mysqlDatabase reads the database the official images create:
// MARIADB_DATABASE, or MYSQL_DATABASE.
func mysqlDatabase(env []string) string {
	names := map[string]string{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		names[key] = value
	}
	if name := names["MARIADB_DATABASE"]; name != "" {
		return name
	}
	return names["MYSQL_DATABASE"]
}

// exec runs a tool in the container as root, with stdin, writing stdout
// to stdout. A failure carries the number of the error (mysqlError); all
// of stderr goes to the server's log only.
func (t mysqlTarget) exec(ctx context.Context, stdin io.Reader, stdout io.Writer, tool [2]string, args ...string) error {
	command := t.command(ctx, tool, args...)
	var stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, &stderr
	err := command.Run()
	if err != nil {
		slog.Error("mysql: a command failed", "command", tool[1], "stderr", stderr.String())
		if message := mysqlError(stderr.String()); message != "" {
			return errors.New(message)
		}
		return err
	}
	return nil
}

// command is docker exec of a tool in the container, as root.
func (t mysqlTarget) command(ctx context.Context, tool [2]string, args ...string) *exec.Cmd {
	run := []string{"exec", "-i", t.container, "sh", "-c", mysqlTool, "sh", tool[0], tool[1], "-uroot"}
	if t.host != "" {
		run = append(run, "-h", t.host)
	}
	return exec.CommandContext(ctx, "docker", append(run, args...)...)
}

// answers says the MySQL takes a query now. It logs nothing: a MySQL
// that starts does not answer for a while.
func (t mysqlTarget) answers(ctx context.Context) bool {
	return t.command(ctx, mysqlClient, "-e", "SELECT 1;", t.database).Run() == nil
}

var mysqlErrorStart = regexp.MustCompile(`^(ERROR \d+ \(\w+\)( at line \d+)?|[a-z-]+: (Got error|Error):? \d+)`)

// mysqlError turns the tools' stderr into one line: the first error, up
// to its number. What follows may quote the app's data, so none of it
// lands in an alert or a report.
func mysqlError(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		start := mysqlErrorStart.FindString(line)
		if start == "" {
			continue
		}
		if strings.HasPrefix(start, "ERROR 1045 ") || strings.HasSuffix(start, " 1045") {
			return "MySQL did not let root in: moin logs in with the MYSQL_ROOT_PASSWORD or MARIADB_ROOT_PASSWORD of the service"
		}
		return start
	}
	return ""
}

// sql runs a script on the database and returns its rows: no header,
// tabs between the columns. It stops at the first error.
func (t mysqlTarget) sql(ctx context.Context, script string) (string, error) {
	var stdout bytes.Buffer
	err := t.exec(ctx, strings.NewReader(script), &stdout, mysqlClient, "-N", "-B", t.database)
	return strings.TrimSpace(stdout.String()), err
}

// quoted is a name as MySQL takes it in a statement.
func quoted(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

func (e *mysqlEngine) Copy(ctx context.Context, w io.Writer) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	return t.dump(ctx, w)
}

// dump writes a gzipped dump of the database to w: one consistent state
// while the app writes, with its routines, triggers and events.
func (t mysqlTarget) dump(ctx context.Context, w io.Writer) error {
	zipped := gzip.NewWriter(w)
	err := t.exec(ctx, nil, zipped, mysqlDump, "--single-transaction", "--routines", "--triggers", "--events", t.database)
	return errors.Join(err, zipped.Close())
}

// load runs a gzipped dump on the database.
func (t mysqlTarget) load(ctx context.Context, dump io.Reader) error {
	unzipped, err := gzip.NewReader(dump)
	if err != nil {
		return fmt.Errorf("the copy is not gzipped: %w", err)
	}
	return t.exec(ctx, unzipped, io.Discard, mysqlClient, t.database)
}

// replace empties the database and loads a gzipped dump into it. The
// empty database is created as MySQL says the live one was, so it keeps
// its character set.
func (t mysqlTarget) replace(ctx context.Context, dump io.Reader) error {
	create := "CREATE DATABASE " + quoted(t.database)
	if out, err := t.sql(ctx, "SHOW CREATE DATABASE "+quoted(t.database)+";"); err == nil {
		if _, statement, ok := strings.Cut(out, "\t"); ok {
			create = statement
		}
	}
	// The script runs without a database: the one it names is gone for a
	// moment.
	script := "DROP DATABASE IF EXISTS " + quoted(t.database) + ";\n" + create + ";\n"
	if err := t.exec(ctx, strings.NewReader(script), io.Discard, mysqlClient); err != nil {
		return err
	}
	return t.load(ctx, dump)
}

// Size is how many bytes the tables of the database take, as MySQL
// counts them.
func (e *mysqlEngine) Size(ctx context.Context) (int64, error) {
	t, err := e.target(ctx)
	if err != nil {
		return 0, err
	}
	out, err := t.sql(ctx, `SELECT COALESCE(SUM(data_length + index_length), 0) FROM information_schema.tables WHERE table_schema = DATABASE();`)
	if err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	var size int64
	if _, err := fmt.Sscan(out, &size); err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	return size, nil
}

// Check loads a copy into a throwaway container of the same image and
// reads every row of every table, then removes the container. Nothing of
// the check touches the app's own MySQL.
func (e *mysqlEngine) Check(ctx context.Context, from source) error {
	t, err := e.target(ctx)
	if err != nil {
		return notChecked{err}
	}
	if folder, err := e.b.apps.output(ctx, "docker", "info", "-f", "{{.DockerRootDir}}"); err == nil {
		if err := room(strings.TrimSpace(folder), from.size); err != nil {
			return notChecked{err}
		}
	}
	check, remove, err := e.throwaway(ctx, t)
	if err != nil {
		return notChecked{fmt.Errorf("start a MySQL for the check: %w", err)}
	}
	defer remove()
	if err := check.load(ctx, from.copy); err != nil {
		return err
	}
	return check.read(ctx)
}

// throwaway starts a container of the target's image with an empty
// database of the same name, no network and a root password of its own,
// and waits up to three minutes until it answers. remove takes it and
// its data away.
func (e *mysqlEngine) throwaway(ctx context.Context, t mysqlTarget) (check mysqlTarget, remove func(), err error) {
	a := e.b.apps
	sum := sha256.Sum256([]byte(e.app + "/" + e.database.Service))
	name := tempName("check-" + hex.EncodeToString(sum[:4]))
	remove = func() {
		_, _ = a.output(context.WithoutCancel(ctx), "docker", "rm", "-f", "-v", name)
	}
	// One a check that was cut off left.
	remove()
	// The password goes through the environment: arguments show in ps.
	password := rand.Text()
	_, err = a.outputEnv(ctx, []string{"MYSQL_ROOT_PASSWORD=" + password, "MARIADB_ROOT_PASSWORD=" + password},
		"docker", "run", "-d", "--name", name, "--network", "none",
		"-e", "MYSQL_ROOT_PASSWORD", "-e", "MARIADB_ROOT_PASSWORD",
		"-e", "MYSQL_DATABASE="+t.database, "-e", "MARIADB_DATABASE="+t.database, t.image)
	if err != nil {
		remove()
		return mysqlTarget{}, nil, err
	}
	// Over TCP: the image first runs a MySQL that only sets things up,
	// and that one answers on the socket alone.
	check = mysqlTarget{container: name, image: t.image, database: t.database, host: "127.0.0.1"}
	for try := 0; try < 90; try++ {
		if check.answers(ctx) {
			return check, remove, nil
		}
		select {
		case <-ctx.Done():
			remove()
			return mysqlTarget{}, nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	remove()
	return mysqlTarget{}, nil, errors.New("it did not answer in three minutes")
}

// read reads every row of every table of the database: CHECKSUM TABLE
// goes through each row, and answers NULL for a table it cannot read.
func (t mysqlTarget) read(ctx context.Context) error {
	out, err := t.sql(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE';`)
	if err != nil || out == "" {
		return err
	}
	var tables []string
	for _, name := range strings.Split(out, "\n") {
		tables = append(tables, quoted(name))
	}
	sums, err := t.sql(ctx, "CHECKSUM TABLE "+strings.Join(tables, ", ")+";")
	if err != nil {
		return err
	}
	lines := strings.Split(sums, "\n")
	if len(lines) != len(tables) || slices.ContainsFunc(lines, func(line string) bool { return strings.HasSuffix(line, "\tNULL") }) {
		return errors.New("a table of the copy does not read")
	}
	return nil
}

// undoFile is where the state before the restore id is kept: a gzipped
// dump in the app's folder on the server, which no app mounts.
func (e *mysqlEngine) undoFile(id string) string {
	return filepath.Join(e.b.apps.appDir(e.app), "undos", e.database.Service, id+".sql.gz")
}

// Restore stops the app's other services, dumps the live database into
// the undo file and loads the copy, or an earlier undo, over the live
// database. A load that fails puts the undo back in, so the database is
// as it was. An undo it went back to is used up, like every engine's.
func (e *mysqlEngine) Restore(ctx context.Context, from source, keepAs string) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	if from.undo != "" {
		file, err := os.Open(e.undoFile(from.undo))
		if err != nil {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
		defer file.Close()
		from.copy = file
	}
	size, err := e.Size(ctx)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(e.undoFile(keepAs)), 0o700)
	}
	if err == nil {
		err = room(filepath.Dir(e.undoFile(keepAs)), size)
	}
	if err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	// An app that is only its database has no other service to stop.
	start := func() error { return nil }
	if others := e.b.otherServices(e.app, e.database.Service); len(others) > 0 {
		start, err = e.b.apps.stopServices(ctx, e.app, others...)
		if err != nil {
			return err
		}
	}
	ctx = context.WithoutCancel(ctx)
	if err := e.keep(ctx, t, keepAs); err != nil {
		return errors.Join(fmt.Errorf("keep the state before the restore: %w; nothing changed", err), start())
	}
	if err := t.replace(ctx, from.copy); err != nil {
		return errors.Join(e.back(ctx, t, keepAs, err), start())
	}
	if from.undo != "" {
		_ = os.Remove(e.undoFile(from.undo))
	}
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// keep dumps the live database into the undo file of id. The file has
// its name only once it is whole and on the disk: an undo that is there
// says the load over the live database began.
func (e *mysqlEngine) keep(ctx context.Context, t mysqlTarget, id string) error {
	name := e.undoFile(id)
	file, err := os.OpenFile(name+".new", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	err = errors.Join(t.dump(ctx, file), file.Sync(), file.Close())
	if err == nil {
		err = os.Rename(name+".new", name)
	}
	if err == nil {
		err = syncFile(filepath.Dir(name))
	}
	if err != nil {
		_ = os.Remove(name + ".new")
	}
	return err
}

// back puts the undo of id into the live database again, after a load
// that failed with why, and removes the undo. When that fails too, the
// undo stays to go back to.
func (e *mysqlEngine) back(ctx context.Context, t mysqlTarget, id string, why error) error {
	file, err := os.Open(e.undoFile(id))
	if err == nil {
		defer file.Close()
		err = t.replace(ctx, file)
	}
	if err != nil {
		return fmt.Errorf("%w, and going back failed too (%w): the state before is the undo %s", why, err, id)
	}
	_ = os.Remove(e.undoFile(id))
	return fmt.Errorf("%w; the data is as it was before", why)
}

// Recover ends a restore that was cut off, once the MySQL answers. With
// no undo of it, nothing had changed. With one, the load over the live
// database had begun and may have stopped halfway, so the undo goes back
// in: the restore never counts as switched.
func (e *mysqlEngine) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	t, err := e.waitForMySQL(ctx)
	if err != nil {
		return false, err
	}
	_ = os.Remove(e.undoFile(mark.Undo) + ".new")
	if !exists(e.undoFile(mark.Undo)) {
		return false, nil
	}
	file, err := os.Open(e.undoFile(mark.Undo))
	if err != nil {
		return false, err
	}
	defer file.Close()
	if err := t.replace(ctx, file); err != nil {
		return false, err
	}
	return false, os.Remove(e.undoFile(mark.Undo))
}

// waitForMySQL waits up to two minutes for the MySQL to answer.
func (e *mysqlEngine) waitForMySQL(ctx context.Context) (mysqlTarget, error) {
	var err error
	for try := 0; try <= 60; try++ {
		if try > 0 {
			time.Sleep(2 * time.Second)
		}
		var t mysqlTarget
		t, err = e.target(ctx)
		if err != nil {
			continue
		}
		if t.answers(ctx) {
			return t, nil
		}
		err = fmt.Errorf("the MySQL service %s of %s does not answer", e.database.Service, e.app)
	}
	return mysqlTarget{}, err
}

// Undos are the undo files of the database, newest first.
func (e *mysqlEngine) Undos(ctx context.Context) ([]protocol.Copy, error) {
	files, _ := filepath.Glob(e.undoFile("*"))
	undos := []protocol.Copy{}
	for _, file := range files {
		id := strings.TrimSuffix(filepath.Base(file), ".sql.gz")
		if at, ok := protocol.CopyTime(id); ok {
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: fileSize(file)})
		}
	}
	slices.SortFunc(undos, func(a, b protocol.Copy) int { return strings.Compare(b.ID, a.ID) })
	return undos, nil
}

func (e *mysqlEngine) Drop(ctx context.Context, id string) error {
	err := os.Remove(e.undoFile(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
