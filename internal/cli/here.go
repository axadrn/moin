package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/axadrn/moin/internal/daemon"
)

// here is `moin here …`: what the CLI on your laptop runs on a server over
// SSH, as root. Data goes to stdout, as JSON for the CLI to read; what
// people should see goes to stderr, like the log of a deploy.
func here(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("moin here runs on a server, for the CLI on your laptop")
	}
	dir := daemon.StateDir()
	log := os.Stderr
	verb, args := args[0], args[1:]
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
	case "releases":
		value, err := daemon.ReleasesHere(dir, oneArg(args))
		return respond(stdout, value, err)
	case "restart", "start":
		value, err := daemon.ApplyHere(ctx, dir, oneArg(args), log)
		return respond(stdout, value, err)
	case "stop":
		return daemon.StopHere(ctx, dir, oneArg(args))
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
		return daemon.UpdateHere(ctx, oneArg(args), nil)
	case "setup", "backups", "backup", "restore", "bucket", "alerts", "watchers":
		return hereBackups(ctx, append([]string{verb}, args...), stdin, stdout)
	}
	return fmt.Errorf("moin on this server does not know `here %s`: put the CLI's version here with `moin update <server>`", verb)
}

// hereDeploy reads the source of a deploy from stdin, unless it is an app
// of the catalog.
func hereDeploy(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("here deploy", "here deploy <app> [--image <image>] [--catalog <name>]")
	image := flags.String("image", "", "a loaded image")
	catalogApp := flags.String("catalog", "", "an app of the catalog")
	app, err := hereApp(flags, args)
	if err != nil {
		return err
	}
	deploy := daemon.Deploy{App: app, Image: *image, Catalog: *catalogApp}
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
	return err
}

// hereEnv is `here env <app>`, the names, `here env get <app>`, the .env
// text, and `here env set <app>`, a new .env text from stdin.
func hereEnv(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
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
	value, err := daemon.EnvNamesHere(dir, oneArg(args))
	return respond(stdout, value, err)
}

// hereDomains is `here domains <app>` and `here domains add|rm <app>
// <domain>`.
func hereDomains(ctx context.Context, dir string, args []string, stdout io.Writer) error {
	if len(args) == 3 && args[0] == "add" {
		value, err := daemon.AddDomainHere(ctx, dir, args[1], args[2], os.Stderr)
		return respond(stdout, value, err)
	}
	if len(args) == 3 && args[0] == "rm" {
		value, err := daemon.RemoveDomainHere(ctx, dir, args[1], args[2], os.Stderr)
		return respond(stdout, value, err)
	}
	value, err := daemon.DomainsHere(dir, oneArg(args))
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
	if flags.NArg() != 1 {
		return "", flags.wrong()
	}
	return flags.Arg(0), nil
}

// oneArg is the only argument, "" when there is not exactly one: the
// daemon then says the name is not valid.
func oneArg(args []string) string {
	if len(args) != 1 {
		return ""
	}
	return args[0]
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
