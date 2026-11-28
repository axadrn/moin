package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
)

// here is `moin here …`: what the CLI on your laptop runs on a server over
// SSH, as root. Data goes to stdout, as JSON for the CLI to read; what
// people should see goes to stderr, like the log of a deploy.
func here(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(build.Name + " here runs on a server, for the CLI on your laptop")
	}
	// When you press Ctrl-C on your laptop, ssh goes and so do stdout and
	// stderr. A deploy goes on to its end instead of dying on a write.
	signal.Ignore(syscall.SIGPIPE)
	// What goes to stdout is data, JSON or bytes: it leaves as it is.
	stdout = raw(stdout)
	// The server's log of this command goes to its journal, not to you.
	daemon.LogToJournal()
	dir := daemon.StateDir()
	log := os.Stderr
	verb, args := args[0], args[1:]
	// name is the one argument of a here command without flags, like the
	// app of `here stop my-app`.
	name := func() (string, error) { return hereApp(newFlags("here "+verb, "here "+verb+" <name>"), args) }
	switch verb {
	case "apps":
		return printJSON(stdout, daemon.StatusHere(ctx, dir))
	case "deploy":
		return hereDeploy(ctx, dir, args, stdin, stdout)
	case "rollback":
		flags := newFlags("here rollback", "here rollback <app> [--to 3]")
		to := flags.Int("to", 0, "the release")
		app, err := hereApp(flags, args)
		if err != nil {
			return err
		}
		value, err := daemon.RollbackHere(ctx, dir, app, *to, log)
		return respond(stdout, value, err)
	case "deploys":
		return hereDeploys(dir, args, stdout)
	case "releases":
		app, err := name()
		if err != nil {
			return err
		}
		value, err := daemon.ReleasesHere(dir, app)
		return respond(stdout, value, err)
	case "restart", "start":
		app, err := name()
		if err != nil {
			return err
		}
		value, err := daemon.ApplyHere(ctx, dir, app, log)
		return respond(stdout, value, err)
	case "stop":
		app, err := name()
		if err != nil {
			return err
		}
		return daemon.StopHere(ctx, dir, app)
	case "rm":
		app, err := name()
		if err != nil {
			return err
		}
		return daemon.RemoveHere(ctx, dir, app)
	case "compose":
		app, err := name()
		if err != nil {
			return err
		}
		value, err := daemon.ComposeHere(dir, app)
		return respond(stdout, value, err)
	case "logs":
		flags := newFlags("here logs", "here logs <app> [--tail 200] [--follow]")
		tail := flags.Int("tail", 200, "lines")
		follow := flags.Bool("follow", false, "follow")
		app, err := hereApp(flags, args)
		if err != nil {
			return err
		}
		return daemon.LogsHere(ctx, dir, app, *tail, *follow, stdout)
	case "exec":
		return hereExec(ctx, dir, args, stdin, stdout)
	case "env":
		return hereEnv(ctx, dir, args, stdin, stdout)
	case "domains":
		return hereDomains(ctx, dir, args, stdout)
	case "keys":
		return hereKeys(ctx, args, stdin, stdout)
	case "update":
		version, err := name()
		if err != nil {
			return err
		}
		return daemon.UpdateHere(ctx, version)
	case "update-app":
		flags := newFlags("here update-app", "here update-app <app> [--version <version>]")
		version := flags.String("version", "", "the version, like 2.15.1")
		app, err := hereApp(flags, args)
		if err != nil {
			return err
		}
		value, err := daemon.UpdateAppHere(ctx, dir, app, *version, log)
		return respond(stdout, value, err)
	case "setup":
		server, err := name()
		if err != nil {
			return err
		}
		return daemon.SetUpHere(ctx, server)
	case "backups":
		flags := newFlags("here backups", "here backups <app> [--from <server>]")
		from := flags.String("from", "", "another server's copies")
		app, err := hereApp(flags, args)
		if err != nil {
			return err
		}
		value, err := daemon.BackupsHere(ctx, dir, app, *from)
		return respond(stdout, value, err)
	case "backup":
		app, err := name()
		if err != nil {
			return err
		}
		value, err := daemon.BackupHere(ctx, dir, app, log)
		return respond(stdout, value, err)
	case "schedule":
		flags := newFlags("here schedule", "here schedule [app] [--every 1h] [--keep 7d]")
		var set daemon.Schedule
		flags.StringVar(&set.Every, "every", "", "a copy this often")
		flags.StringVar(&set.Keep, "keep", "", "each kept this long")
		if err := flags.parse(args); err != nil {
			return err
		}
		value, err := daemon.ScheduleHere(flags.Arg(0), set)
		return respond(stdout, value, err)
	case "restore":
		flags := newFlags("here restore", "here restore <app> --to <copy|time|undo-…> [--database <name>] [--from <server>] [--key-stdin]")
		to := flags.String("to", "", "what to go back to")
		database := flags.String("database", "", "the database")
		from := flags.String("from", "", "another server's copies")
		keyOnStdin := flags.Bool("key-stdin", false, "your backup key comes on stdin")
		app, err := hereApp(flags, args)
		if err != nil {
			return err
		}
		restore := daemon.Restore{App: app, To: *to, Database: *database, From: *from}
		if *keyOnStdin {
			if restore.Key, err = readLine(stdin); err != nil {
				return err
			}
		}
		value, err := daemon.RestoreHere(ctx, dir, restore, log)
		return respond(stdout, value, err)
	case "cloud-restore":
		return hereCloudRestore(ctx, dir, stdin, stdout)
	case "bucket":
		return hereBucket(ctx, args, stdin, stdout)
	case "alerts":
		return hereAlerts(ctx, args, stdin, stdout)
	case "watchers":
		return hereWatchers(args, stdin, stdout)
	case "tailnet":
		return hereTailnet(ctx, args, stdin, stdout)
	case "secure":
		return hereSecure(ctx, args, stdin, stdout)
	case "follow":
		return hereFollow(ctx, args, stdin, stdout)
	case "registry":
		return hereRegistry(ctx, args, stdin, stdout)
	case "password":
		return herePassword(ctx, dir, args, stdin, stdout)
	case "sql":
		return hereSQL(ctx, dir, args, stdin, stdout)
	case "knocks":
		value, err := daemon.KnocksHere(dir)
		return respond(stdout, value, err)
	}
	return fmt.Errorf("%s on this server does not know `here %s`: put the CLI's version here with `%s update <server>`", build.Name, verb, build.Name)
}

// hereDeploy reads the source of a deploy from stdin, unless it is an app
// of the catalog.
func hereDeploy(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("here deploy", "here deploy <app> [--image <image>] [--catalog <name> [--domain <domain>]] [--no-copy]")
	noCopy := flags.Bool("no-copy", false, "no copy of the databases before it")
	image := flags.String("image", "", "a loaded image")
	catalogApp := flags.String("catalog", "", "an app of the catalog")
	domain := flags.String("domain", "", "the catalog app's domain")
	app, err := hereApp(flags, args)
	if err != nil {
		return err
	}
	deploy := daemon.Deploy{App: app, Image: *image, Catalog: *catalogApp, Domain: *domain, NoCopy: *noCopy}
	if *catalogApp == "" {
		deploy.Source = stdin
	}
	value, err := daemon.DeployHere(ctx, dir, deploy, os.Stderr)
	return respond(stdout, value, err)
}

// hereExec runs the command after -- in the app's container.
func hereExec(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	own, command := execArgs(args)
	flags := newFlags("here exec", "here exec <app> [--service web] [--tty] -- <command>")
	service := flags.String("service", "", "the service")
	tty := flags.Bool("tty", false, "a terminal")
	app, err := hereApp(flags, own)
	if err != nil {
		return err
	}
	err = daemon.ExecHere(ctx, dir, app, *service, *tty, command, stdin, stdout, os.Stderr)
	if code := exitCodeOf(err); code > 0 {
		return exitCode(code)
	}
	if errors.Is(err, daemon.ErrNoApp) {
		// Said here, not quietly: the CLI never runs a command twice, so
		// it does not look on other servers after exec.
		return fmt.Errorf("there is no app %s on this server", app)
	}
	return err
}

// hereEnv is `here env <app>`, the names, `here env get <app>`, the .env
// text, `here env set <app>`, a new .env text from stdin, and `here env
// set <app> <NAME>`, the value of one from stdin.
func hereEnv(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 3 && args[0] == "set" {
		value, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
		if err != nil {
			return err
		}
		saved, err := daemon.SetEnvVarHere(dir, args[1], args[2], string(value))
		return respond(stdout, saved, err)
	}
	if len(args) == 2 && args[0] == "get" {
		text, err := daemon.EnvFileHere(dir, args[1])
		if err != nil {
			return err
		}
		_, err = io.WriteString(stdout, text)
		return err
	}
	if len(args) == 2 && args[0] == "set" {
		text, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
		if err != nil {
			return err
		}
		value, err := daemon.SetEnvHere(dir, args[1], string(text))
		return respond(stdout, value, err)
	}
	if len(args) != 1 {
		return errors.New("usage: here env <app> | get <app> | set <app> [<NAME>]")
	}
	value, err := daemon.EnvNamesHere(dir, args[0])
	return respond(stdout, value, err)
}

// hereDomains is `here domains <app>`, `here domains add <app> <domain>
// [service]` and `here domains rm <app> <domain>`.
func hereDomains(ctx context.Context, dir string, args []string, stdout io.Writer) error {
	if (len(args) == 3 || len(args) == 4) && args[0] == "add" {
		service := ""
		if len(args) == 4 {
			service = args[3]
		}
		value, err := daemon.AddDomainHere(ctx, dir, args[1], args[2], service, os.Stderr)
		return respond(stdout, value, err)
	}
	if len(args) == 3 && args[0] == "rm" {
		value, err := daemon.RemoveDomainHere(ctx, dir, args[1], args[2], os.Stderr)
		return respond(stdout, value, err)
	}
	if len(args) != 1 {
		return errors.New("usage: here domains <app> | add <app> <domain> [service] | rm <app> <domain>")
	}
	value, err := daemon.DomainsHere(dir, args[0])
	return respond(stdout, value, err)
}

// hereKeys is `here keys`, `here keys add` with the key on stdin, and
// `here keys rm <name> --session <$SSH_CONNECTION>`.
func hereKeys(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "add" {
		line, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
		if err != nil {
			return err
		}
		value, err := daemon.AddKeyHere(string(line))
		return respond(stdout, value, err)
	}
	if len(args) > 0 && args[0] == "rm" {
		flags := newFlags("here keys rm", "here keys rm <name> --session <ssh connection>")
		session := flags.String("session", "", "$SSH_CONNECTION")
		name, err := hereApp(flags, args[1:])
		if err != nil {
			return err
		}
		value, err := daemon.RemoveKeyHere(ctx, name, *session)
		return respond(stdout, value, err)
	}
	value, err := daemon.KeysHere()
	return respond(stdout, value, err)
}

// hereApp parses the flags of a here command and returns its one argument.
func hereApp(flags *flagSet, args []string) (string, error) {
	if err := flags.parse(args); err != nil {
		return "", err
	}
	return flags.oneArg()
}

// respond prints the value a here command returns as JSON, or returns
// its error.
func respond(stdout io.Writer, value any, err error) error {
	if err != nil {
		return err
	}
	return printJSON(stdout, value)
}

// exitCodeOf is the exit code of a command that ran and failed, 0 for
// anything else.
func exitCodeOf(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 0
}

// hereBucket is `moin here bucket`, which answers the bucket without its
// secret key, `moin here bucket check`, which checks it first, and `moin
// here bucket set`, which reads one from stdin.
func hereBucket(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		bucket, err := daemon.BucketHere()
		return respond(stdout, bucket, err)
	}
	if args[0] == "key" {
		return hereBucketKey(ctx, args[1:], stdin, stdout)
	}
	if len(args) == 1 && args[0] == "check" {
		check, err := daemon.CheckBucketHere(ctx)
		return respond(stdout, check, err)
	}
	if len(args) != 1 || args[0] != "set" {
		return errors.New("usage: here bucket [check|set]")
	}
	var bucket daemon.Bucket
	if err := json.NewDecoder(stdin).Decode(&bucket); err != nil {
		return fmt.Errorf("read the bucket: %w", err)
	}
	saved, err := daemon.SetBucketHere(ctx, bucket)
	return respond(stdout, saved, err)
}

// hereAlerts is `moin here alerts`, and `alerts add` with the destination
// on stdin, `alerts rm <n>` and `alerts test`.
func hereAlerts(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		names, err := daemon.AlertsHere()
		return respond(stdout, names, err)
	case len(args) == 1 && args[0] == "add":
		var destination alert.Destination
		if err := json.NewDecoder(stdin).Decode(&destination); err != nil {
			return fmt.Errorf("read the alert: %w", err)
		}
		return daemon.AddAlertHere(ctx, destination)
	case len(args) == 2 && args[0] == "rm":
		number, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("%q is not the number of an alert: see them with `%s alerts <server>`", args[1], build.Name)
		}
		return daemon.RemoveAlertHere(number)
	case len(args) == 1 && args[0] == "test":
		return daemon.SendTestAlertHere(ctx)
	}
	return errors.New("usage: here alerts [add | rm <n> | test]")
}

// hereWatchers is `moin here watchers`, and `watchers connect` with the
// watcher on stdin and `watchers disconnect <url>`.
func hereWatchers(args []string, stdin io.Reader, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		addresses, err := daemon.WatchersHere()
		return respond(stdout, addresses, err)
	case len(args) == 1 && args[0] == "connect":
		var watcher daemon.Watcher
		if err := json.NewDecoder(stdin).Decode(&watcher); err != nil {
			return fmt.Errorf("read the watcher: %w", err)
		}
		return daemon.ConnectWatcherHere(watcher)
	case len(args) == 2 && args[0] == "disconnect":
		return daemon.DisconnectWatcherHere(args[1])
	}
	return errors.New("usage: here watchers [connect | disconnect <url>]")
}
