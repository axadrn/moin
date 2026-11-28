package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/axadrn/moin/protocol"
)

// redisEngine backs up a Redis or Valkey service that keeps its data,
// with the tools of its own image, through docker exec of redis-cli. The
// daemon links no Redis driver.
//
// A copy is the dump Redis sends a replica, gzipped: redis-cli --rdb
// asks for it, and Redis writes it while the app keeps running. It lies
// on the volume of the Redis for a moment, next to its data.
//
// Redis loads a dump only when it starts, and never over what it holds.
// So the check and the restore start a second Redis of the same image
// with the dump, in a throwaway container. The check asks that one how
// many keys it has. The restore has the live Redis take everything from
// it, as a replica does: Redis itself swaps its data and writes its own
// files, the append only file too. Nothing stops, and no file of the
// live Redis is touched by hand.
//
// The restore is the one of overLive: the live data is kept as a dump on
// the server, the undo, first.
type redisEngine struct {
	overLive
}

func newRedisEngine(b *backups, app string, database protocol.Database) *redisEngine {
	e := &redisEngine{}
	e.overLive = overLive{b: b, app: app, database: database, db: e, ending: ".rdb.gz"}
	return e
}

// redisTarget is a running Redis: its container and the exact image it
// runs. password and port are what --requirepass and --port in the
// service's command name, "" for none.
type redisTarget struct {
	container string
	image     string
	password  string
	port      string
}

// redisSidePort is the port the second Redis listens on, on 127.0.0.1 of
// its container only.
const redisSidePort = "16399"

func (e *redisEngine) target(ctx context.Context) (redisTarget, error) {
	a := e.b.apps
	container, image, err := a.serviceContainer(ctx, e.app, e.database.Service, "Redis")
	if err != nil {
		return redisTarget{}, err
	}
	out, err := a.output(ctx, "docker", "inspect", "-f", "{{json .Args}}", container)
	if err != nil {
		return redisTarget{}, err
	}
	var args []string
	_ = json.Unmarshal([]byte(out), &args)
	return redisTarget{container: container, image: image, password: redisArg(args, "--requirepass"), port: redisArg(args, "--port")}, nil
}

// redisArg is the value after name in the command of a Redis, "" when
// the command does not name it.
func redisArg(args []string, name string) string {
	for i, arg := range args[:max(len(args)-1, 0)] {
		if arg == name {
			return args[i+1]
		}
	}
	return ""
}

// command is docker exec of redis-cli in the container. The password
// goes through the environment: arguments show in ps. -e makes an answer
// that is an error fail the command.
func (t redisTarget) command(ctx context.Context, args ...string) *exec.Cmd {
	run := []string{"exec", "-i"}
	if t.password != "" {
		run = append(run, "-e", "REDISCLI_AUTH")
	}
	run = append(run, t.container, "redis-cli", "-e")
	if t.port != "" {
		run = append(run, "-p", t.port)
	}
	command := exec.CommandContext(ctx, "docker", append(run, args...)...)
	command.Env = append(os.Environ(), "REDISCLI_AUTH="+t.password)
	return command
}

// cli runs redis-cli in the container, writing stdout to stdout. A
// failure carries one short line (redisError); all of stderr goes to the
// server's log only.
func (t redisTarget) cli(ctx context.Context, stdout io.Writer, args ...string) error {
	command := t.command(ctx, args...)
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = stdout, &stderr
	if err := command.Run(); err != nil {
		slog.Error("redis: a command failed", "command", args[0], "stderr", stderr.String())
		return redisError(stderr.String(), err)
	}
	return nil
}

// ask runs one command of Redis and returns its answer.
func (t redisTarget) ask(ctx context.Context, args ...string) (string, error) {
	var stdout bytes.Buffer
	err := t.cli(ctx, &stdout, args...)
	return strings.TrimSpace(stdout.String()), err
}

// answers says the Redis takes commands now: one that still loads its
// data does not. It logs nothing.
func (t redisTarget) answers(ctx context.Context) bool {
	out, err := t.command(ctx, "PING").Output()
	return err == nil && strings.TrimSpace(string(out)) == "PONG"
}

// redisError turns the stderr of redis-cli into one line.
func redisError(stderr string, err error) error {
	if strings.Contains(stderr, "NOAUTH") || strings.Contains(stderr, "WRONGPASS") {
		return errors.New("Redis did not let moin in: moin reads the password from --requirepass in the command of the service, or from REDISCLI_AUTH in its environment")
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return errors.New("redis-cli: " + last)
	}
	return fmt.Errorf("redis-cli: %w", err)
}

func (e *redisEngine) Copy(ctx context.Context, w io.Writer) error { return e.dump(ctx, w) }

// dump has redis-cli fetch a dump of the live Redis into a file on the
// volume of its data, writes it gzipped to w and removes the file. The
// file is needed: redis-cli cuts the end mark of the transfer off a file,
// but not off a pipe.
func (e *redisEngine) dump(ctx context.Context, w io.Writer) error {
	t, err := e.target(ctx)
	if err != nil {
		return err
	}
	file := "/data/" + tempName(strings.ToLower(rand.Text())+".rdb")
	defer exec.CommandContext(context.WithoutCancel(ctx), "docker", "exec", t.container, "rm", "-f", file).Run()
	if err := t.cli(ctx, io.Discard, "--rdb", file); err != nil {
		return err
	}
	zipped := gzip.NewWriter(w)
	read := exec.CommandContext(ctx, "docker", "exec", t.container, "cat", file)
	read.Stdout = zipped
	if err := read.Run(); err != nil {
		return fmt.Errorf("read the dump out of the Redis container: %w", err)
	}
	return zipped.Close()
}

// Size is how many bytes the data takes in the memory of the Redis. Its
// dump is smaller.
func (e *redisEngine) Size(ctx context.Context) (int64, error) {
	t, err := e.target(ctx)
	if err != nil {
		return 0, err
	}
	info, err := t.ask(ctx, "INFO", "memory")
	if err != nil {
		return 0, fmt.Errorf("see how big the database is: %w", err)
	}
	var size int64
	for _, line := range strings.Split(info, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "used_memory:"); ok {
			_, err = fmt.Sscan(value, &size)
		}
	}
	if err != nil || size == 0 {
		return 0, errors.New("see how big the database is: Redis did not say")
	}
	return size, nil
}

// second starts a throwaway container of the target's image in network.
// It only sleeps, until serve starts a Redis in it. what is "check" or
// "restore". remove takes the container and its data away.
func (e *redisEngine) second(ctx context.Context, t redisTarget, what, network string) (second redisTarget, remove func(), err error) {
	name, remove := e.b.apps.throwawayContainer(ctx, what, e.app, e.database.Service)
	// One that was cut off left.
	remove()
	_, err = e.b.apps.output(ctx, "docker", "run", "-d", "--name", name, "--network", network, "--entrypoint", "sleep", t.image, "86400")
	if err != nil {
		remove()
		return redisTarget{}, nil, err
	}
	return redisTarget{container: name, image: t.image, port: redisSidePort}, remove, nil
}

// serve puts a gzipped dump into the throwaway container, has the image's
// own redis-check-rdb read it, and starts a Redis there that loads it.
// That Redis listens on 127.0.0.1 only and writes nothing to the disk.
func (t redisTarget) serve(ctx context.Context, dump io.Reader) error {
	unzipped, err := gzip.NewReader(dump)
	if err != nil {
		return fmt.Errorf("the copy is not gzipped: %w", err)
	}
	run := func(stdin io.Reader, args ...string) error {
		command := exec.CommandContext(ctx, "docker", append([]string{"exec", "-i"}, args...)...)
		command.Stdin = stdin
		out, err := command.CombinedOutput()
		if err != nil {
			slog.Error("redis: the second Redis", "command", args, "output", string(out))
		}
		return err
	}
	if err := run(unzipped, t.container, "sh", "-c", "cat > /data/dump.rdb"); err != nil {
		return fmt.Errorf("write the dump: %w", err)
	}
	if err := run(nil, t.container, "redis-check-rdb", "/data/dump.rdb"); err != nil {
		return errors.New("the copy is not a dump Redis reads")
	}
	if err := run(nil, "-d", t.container, "redis-server", "--port", t.port, "--bind", "127.0.0.1", "--dir", "/data", "--save", "", "--appendonly", "no"); err != nil {
		return fmt.Errorf("start a Redis with the dump: %w", err)
	}
	if err := waitFor(ctx, t.answers); err != nil {
		return fmt.Errorf("the Redis with the dump: %w", err)
	}
	return nil
}

// Check loads a copy into a second Redis of the same image, in a
// throwaway container without a network, and asks it how many keys it
// has: a Redis reads every key of a dump to load it. Then it removes the
// container. Nothing of the check touches the app's own Redis.
func (e *redisEngine) Check(ctx context.Context, from source) error {
	t, err := e.target(ctx)
	if err == nil {
		err = e.b.apps.roomForCheck(ctx, from.size)
	}
	if err != nil {
		return notChecked{err}
	}
	check, remove, err := e.second(ctx, t, "check", "none")
	if err != nil {
		return notChecked{fmt.Errorf("start a container for the check: %w", err)}
	}
	defer remove()
	if err := check.serve(ctx, from.copy); err != nil {
		return err
	}
	_, err = check.ask(ctx, "DBSIZE")
	return err
}

// replace has the live Redis take everything from a second Redis that
// loaded the dump. The second one shares the network of the live
// container, so the live Redis finds it on 127.0.0.1 and nothing else
// does. A dump that does not load fails before the live Redis hears of
// it.
func (e *redisEngine) replace(ctx context.Context, dump io.Reader) error {
	t, err := e.target(ctx)
	if err == nil {
		err = waitFor(ctx, t.answers)
	}
	if err != nil {
		return err
	}
	second, remove, err := e.second(ctx, t, "restore", "container:"+t.container)
	if err != nil {
		return fmt.Errorf("start a container for the restore: %w", err)
	}
	defer remove()
	if err := second.serve(ctx, dump); err != nil {
		return err
	}
	_, err = t.ask(ctx, "REPLICAOF", "127.0.0.1", redisSidePort)
	if err == nil {
		err = t.followed(ctx)
	}
	// The live Redis is its own again, whatever happened.
	_, alone := t.ask(ctx, "REPLICAOF", "NO", "ONE")
	return errors.Join(err, alone)
}

// followed waits until the live Redis has taken all the data of the one
// it follows. It waits as long as the data moves, and three minutes for
// it to begin.
func (t redisTarget) followed(ctx context.Context) error {
	for waited := 0; ; waited++ {
		info, err := t.ask(ctx, "INFO", "replication")
		if err != nil {
			return err
		}
		if strings.Contains(info, "master_link_status:up") {
			return nil
		}
		if waited >= 90 && !strings.Contains(info, "master_sync_in_progress:1") {
			return errors.New("the Redis did not take the data in three minutes")
		}
		time.Sleep(2 * time.Second)
	}
}
