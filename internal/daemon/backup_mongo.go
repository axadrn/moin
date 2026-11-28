package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// mongoEngine backs up a MongoDB service with the tools of its own image,
// through docker exec: mongodump writes a copy, mongorestore loads one,
// mongosh asks and drops. The daemon links no MongoDB driver. It logs in
// as the root user the container's own environment names, if it names
// one: the password never leaves the container.
//
// A copy is everything mongodump takes: every database, the users too. A
// restore loads the app's databases and leaves admin and config, where
// MongoDB keeps its users and its own notes, as they are.
//
// MongoDB cannot swap a database for another in one step, so the restore
// is the one of overLive: the live databases are kept as a dump on the
// server, the undo, and the copy is loaded over them.
type mongoEngine struct {
	overLive
}

func newMongoEngine(b *backups, app string, database protocol.Database) *mongoEngine {
	e := &mongoEngine{}
	e.overLive = overLive{b: b, app: app, database: database, db: e, ending: ".archive.gz"}
	return e
}

// mongoTarget is a running MongoDB: its container and the exact image it
// runs.
type mongoTarget struct {
	container string
	image     string
}

// mongoTool runs in the container: a tool of MongoDB with the rest of
// the arguments. When the container's environment names a root user, the
// tool logs in as it. Its password goes into a file only root reads, for
// a moment: arguments show in ps.
const mongoTool = `user="$MONGO_INITDB_ROOT_USERNAME"
password="$MONGO_INITDB_ROOT_PASSWORD"
if [ -f "$MONGO_INITDB_ROOT_USERNAME_FILE" ]; then user=$(cat "$MONGO_INITDB_ROOT_USERNAME_FILE"); fi
if [ -f "$MONGO_INITDB_ROOT_PASSWORD_FILE" ]; then password=$(cat "$MONGO_INITDB_ROOT_PASSWORD_FILE"); fi
tool=$1
shift
if [ -z "$user" ]; then exec "$tool" "$@"; fi
umask 077
file=$(mktemp)
trap 'rm -f "$file"' EXIT
printf "password: '%s'\n" "$(printf %s "$password" | sed "s/'/''/g")" > "$file"
"$tool" --config "$file" --username "$user" --authenticationDatabase admin "$@"`

// mongoLogin starts every script for mongosh: it logs in as the same root
// user, with the password from the environment of the container.
const mongoLogin = `const read = (name) => process.env[name + "_FILE"] ? require("fs").readFileSync(process.env[name + "_FILE"], "utf8").trim() : process.env[name];
if (read("MONGO_INITDB_ROOT_USERNAME")) db.getSiblingDB("admin").auth(read("MONGO_INITDB_ROOT_USERNAME"), read("MONGO_INITDB_ROOT_PASSWORD"));
const own = ["admin", "config", "local"];
const databases = () => db.adminCommand({listDatabases: 1}).databases.filter((d) => !own.includes(d.name));
`

// mongoOwn keeps mongorestore away from the databases MongoDB keeps for
// itself.
var mongoOwn = []string{"--nsExclude", "admin.*", "--nsExclude", "config.*"}

func (e *mongoEngine) target(ctx context.Context) (mongoTarget, error) {
	container, image, err := e.b.apps.serviceContainer(ctx, e.app, e.database.Service, "MongoDB")
	return mongoTarget{container: container, image: image}, err
}

// tool runs mongodump or mongorestore in the container, with stdin,
// writing stdout to stdout. A failure carries one short line (mongoError);
// all of stderr goes to the server's log only.
func (t mongoTarget) tool(ctx context.Context, stdin io.Reader, stdout io.Writer, tool string, args ...string) error {
	run := append([]string{"exec", "-i", t.container, "sh", "-c", mongoTool, "sh", tool, "--quiet"}, args...)
	command := exec.CommandContext(ctx, "docker", run...)
	var stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, &stderr
	if err := command.Run(); err != nil {
		slog.Error("mongo: a command failed", "command", tool, "stderr", stderr.String())
		return mongoError(tool, stderr.String(), err)
	}
	return nil
}

// js runs a script in mongosh in the container and returns what it
// prints.
func (t mongoTarget) js(ctx context.Context, script string) (string, error) {
	command := exec.CommandContext(ctx, "docker", "exec", t.container, "mongosh", "--quiet", "--norc", "--eval", mongoLogin+script)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		slog.Error("mongo: a script failed", "stderr", stderr.String())
		return "", mongoError("mongosh", stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// answers says the MongoDB takes a command now. It logs nothing: a
// MongoDB that starts does not answer for a while.
func (t mongoTarget) answers(ctx context.Context) bool {
	return exec.CommandContext(ctx, "docker", "exec", t.container, "mongosh", "--quiet", "--norc", "--eval", mongoLogin+`db.runCommand({ping: 1})`).Run() == nil
}

// mongoError turns the tools' stderr into one line: the kind of the first
// error. What follows it may quote the app's data, so none of it lands in
// an alert or a report.
func mongoError(tool, stderr string, err error) error {
	if strings.Contains(stderr, "AuthenticationFailed") || strings.Contains(stderr, "Authentication failed") {
		return errors.New("MongoDB did not let moin in: moin logs in with the MONGO_INITDB_ROOT_USERNAME and MONGO_INITDB_ROOT_PASSWORD of the service")
	}
	for _, line := range strings.Split(stderr, "\n") {
		for _, start := range []string{"Failed: ", "MongoServerError: "} {
			if _, message, ok := strings.Cut(line, start); ok {
				if end := strings.IndexAny(message, ":;({["); end >= 0 {
					message = message[:end]
				}
				return fmt.Errorf("%s: %s", tool, strings.TrimSpace(message))
			}
		}
	}
	return fmt.Errorf("%s: %w", tool, err)
}

func (e *mongoEngine) Copy(ctx context.Context, w io.Writer) error { return e.dump(ctx, w) }

// dump writes a gzipped archive of every database to w, while the app
// keeps writing.
func (e *mongoEngine) dump(ctx context.Context, w io.Writer) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	return t.tool(ctx, nil, w, "mongodump", "--archive", "--gzip")
}

// load loads the app's databases of a gzipped archive.
func (t mongoTarget) load(ctx context.Context, archive io.Reader) error {
	return t.tool(ctx, archive, io.Discard, "mongorestore", append([]string{"--archive", "--gzip"}, mongoOwn...)...)
}

// replace drops the app's databases and loads the ones of a gzipped
// archive.
func (e *mongoEngine) replace(ctx context.Context, archive io.Reader) error {
	t, err := e.target(ctx)
	if err == nil {
		err = waitFor(ctx, t.answers)
	}
	if err != nil {
		return err
	}
	if _, err := t.js(ctx, `for (const d of databases()) db.getSiblingDB(d.name).dropDatabase();`); err != nil {
		return err
	}
	return t.load(ctx, archive)
}

// Size is how many bytes the app's databases take on the disk, as
// MongoDB counts them.
func (e *mongoEngine) Size(ctx context.Context) (int64, error) {
	t, err := e.target(ctx)
	if err != nil {
		return 0, err
	}
	out, err := t.js(ctx, `print(databases().reduce((sum, d) => sum + Number(d.sizeOnDisk), 0));`)
	if err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	var size int64
	if _, err := fmt.Sscan(out, &size); err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	return size, nil
}

// Check loads a copy into a throwaway container of the same image, which
// has no network, and reads every document of every collection. Then it
// removes the container. Nothing of the check touches the app's own
// MongoDB.
func (e *mongoEngine) Check(ctx context.Context, from source) error {
	t, err := e.target(ctx)
	if err == nil {
		err = e.b.apps.roomForCheck(ctx, from.size)
	}
	if err != nil {
		return notChecked{err}
	}
	name, remove := e.b.apps.throwawayContainer(ctx, "check", e.app, e.database.Service)
	// One a check that was cut off left.
	remove()
	defer remove()
	check := mongoTarget{container: name, image: t.image}
	_, err = e.b.apps.output(ctx, "docker", "run", "-d", "--name", name, "--network", "none", t.image)
	if err == nil {
		err = waitFor(ctx, check.answers)
	}
	if err != nil {
		return notChecked{fmt.Errorf("start a MongoDB for the check: %w", err)}
	}
	if err := check.load(ctx, from.copy); err != nil {
		return err
	}
	_, err = check.js(ctx, mongoRead)
	return err
}

// mongoRead reads every document of every collection of the app's
// databases, and has MongoDB look through each collection and its
// indexes. It throws at the first one that does not read.
const mongoRead = `for (const d of databases()) {
	const database = db.getSiblingDB(d.name);
	for (const collection of database.getCollectionInfos({type: "collection"})) {
		database.getCollection(collection.name).find().itcount();
		if (!database.runCommand({validate: collection.name}).valid) throw new Error("a collection of the copy does not read");
	}
}`
