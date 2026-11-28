package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// Postgres runs as a service of the app. The daemon reaches it only through
// docker exec and the tools of the app's own Postgres image: psql, pg_dump
// and pg_restore over the unix socket in the container, which the official
// image trusts. No password leaves the container and the daemon links no
// Postgres driver.

// pgTarget is a running Postgres service: its container, image, user and
// database.
type pgTarget struct {
	container string
	image     string
	user      string
	database  string
}

// postgres finds the running container of a Postgres service of an app,
// with the user and database its environment names.
func (a *apps) postgres(ctx context.Context, app string, database protocol.Database) (pgTarget, error) {
	if !protocol.ValidAppName(app) || !validService(database.Service) {
		return pgTarget{}, errors.New("invalid app or service name")
	}
	out, err := a.output(ctx, "docker", "ps", "-q", "--filter", "status=running",
		"--filter", "label=com.docker.compose.project="+a.projectOf(app),
		"--filter", "label=com.docker.compose.service="+database.Service)
	if err != nil {
		return pgTarget{}, err
	}
	container, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if container == "" {
		return pgTarget{}, fmt.Errorf("the Postgres service %s of %s is %w", database.Service, app, errPostgresDown)
	}
	inspected, err := a.output(ctx, "docker", "inspect", "-f", "{{json .Config.Env}}\n{{.Config.Image}}", container)
	if err != nil {
		return pgTarget{}, err
	}
	envJSON, image, _ := strings.Cut(strings.TrimSpace(inspected), "\n")
	var env []string
	_ = json.Unmarshal([]byte(envJSON), &env)
	user, name := pgNames(env)
	return pgTarget{container: container, image: strings.TrimSpace(image), user: user, database: name}, nil
}

// errPostgresDown is a Postgres service that is not running, such as one
// that restarts over and over.
var errPostgresDown = errors.New("not running")

// pgNames reads the user and database the official image creates:
// POSTGRES_USER, default postgres, and POSTGRES_DB, default the user.
func pgNames(env []string) (user, database string) {
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		switch key {
		case "POSTGRES_USER":
			user = value
		case "POSTGRES_DB":
			database = value
		}
	}
	if user == "" {
		user = "postgres"
	}
	if database == "" {
		database = user
	}
	return user, database
}

var servicePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func validService(name string) bool { return len(name) <= 63 && servicePattern.MatchString(name) }

// exec runs a command in the container with stdin, writing stdout to
// stdout. A failure carries the errors of stderr (pgError); all of stderr
// goes to the server's log only.
func (t pgTarget) exec(ctx context.Context, stdin io.Reader, stdout io.Writer, args ...string) error {
	command := exec.CommandContext(ctx, "docker", append([]string{"exec", "-i", t.container}, args...)...)
	var stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, &stderr
	err := command.Run()
	if err != nil {
		slog.Error("postgres: a command failed", "command", args[0], "stderr", stderr.String())
		if message := pgError(stderr.String()); message != "" {
			return errors.New(message)
		}
		return err
	}
	return nil
}

// pgPrivate start the parts of an error that quote the app's data: the
// row a COPY stopped at, the key that is there twice, the command
// pg_restore ran. pgMessage start the next message, where such a part ends.
var (
	pgPrivate = []string{"CONTEXT:", "DETAIL:", "Command was:"}
	pgMessage = []string{"pg_restore:", "pg_dump:", "psql:", "ERROR:", "FATAL:", "HINT:", "WARNING:", "NOTICE:"}
)

// pgError turns psql's and pg_restore's stderr into one line: the errors
// without the input position psql puts before them, and without the parts
// that quote the app's data, so none of it lands in an alert or a report.
func pgError(stderr string) string {
	var lines []string
	private := false
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case startsWith(line, pgPrivate):
			private = true
		case startsWith(line, pgMessage):
			private = false
		}
		// The query echo and its caret say where; the message says what.
		if private || line == "" || line == "^" || strings.HasPrefix(line, "LINE ") {
			continue
		}
		if strings.HasPrefix(line, "psql:") {
			if _, rest, ok := strings.Cut(line, ": ERROR:"); ok {
				line = "ERROR:" + rest
			} else if parts := strings.SplitN(line, ": ", 3); len(parts) == 3 {
				line = parts[2]
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "; ")
}

// startsWith says line starts with one of starts.
func startsWith(line string, starts []string) bool {
	return slices.ContainsFunc(starts, func(start string) bool { return strings.HasPrefix(line, start) })
}

// psql runs a script on the database: quiet unless tags, unaligned, rows
// only, stopping at the first error. vars become psql variables the script
// reads as :'name', which psql quotes as literals.
func (t pgTarget) psql(ctx context.Context, script string, vars map[string]string, tags bool) (string, error) {
	args := []string{"psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-h", pgSocket, "-U", t.user, "-d", t.database}
	if !tags {
		args = append(args, "-q")
	}
	for name, value := range vars {
		args = append(args, "-v", name+"="+value)
	}
	var stdout bytes.Buffer
	err := t.exec(ctx, strings.NewReader(script), &stdout, args...)
	return stdout.String(), err
}

// pgSocket is the folder of the unix socket in the official image. Naming
// it keeps a PGHOST in the container's environment from sending the tools
// over TCP, where the image asks for a password.
const pgSocket = "/var/run/postgresql"
