package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// registry is `moin registry <server>`: the registries the server is
// logged in to, for images that are not public. `registry set` logs it
// in, `registry rm` logs it out. The login is Docker's own, on the server
// only.
func registry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "set" {
		return setRegistry(ctx, args[1:], stdin, stdout)
	}
	if len(args) > 0 && args[0] == "rm" {
		return removeRegistry(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("registry", "registry <server> [--json], or registry set <server> <registry> <user>, or registry rm <server> <registry>").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, _, err := flags.serverArgs(stdin, 0)
	if err != nil {
		return err
	}
	var list []daemon.Registry
	if err := sshJSON(ctx, server, []string{"registry"}, nil, &list); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, list)
	}
	if len(list) == 0 {
		fmt.Fprintln(stdout, style.Muted.Render(server+" is logged in to no registry. It pulls public images only."))
		hint(stdout, "Log it in with", build.Name+" registry set "+server+" ghcr.io <user>")
		return nil
	}
	rows := [][]string{}
	for _, each := range list {
		rows = append(rows, []string{each.Registry, each.User})
	}
	table(stdout, []string{"REGISTRY", "USER"}, rows)
	return nil
}

// setRegistry logs the server in to a registry. The token is asked
// without showing it, or read from stdin: never a flag, which would stay
// in your shell's history.
func setRegistry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("registry set", "registry set <server> <registry> <user>, the token is asked or read from stdin")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, rest, err := flags.serverArgs(stdin, 2)
	if err != nil {
		return err
	}
	var login daemon.Registry
	switch {
	case len(rest) == 2:
		login = daemon.Registry{Registry: rest[0], User: rest[1]}
	case !isTerminal(stdin):
		return flags.wrong()
	default:
		if login, err = askLogin(rest, stdin, stdout); err != nil {
			return err
		}
	}
	if isTerminal(stdin) {
		fmt.Fprint(stdout, style.Warning.Render("?")+" Token of "+login.User+" at "+login.Registry+": ")
	}
	token, err := readSecret(stdin)
	if isTerminal(stdin) {
		fmt.Fprintln(stdout)
	}
	if err != nil {
		return err
	}
	if token == "" {
		return errors.New("no token, nothing changed. Type it when asked, or pipe it in")
	}
	login.Token = token
	if err := sendJSON(ctx, server, login, "registry", "set"); err != nil {
		return err
	}
	done(stdout, "%s pulls from %s as %s now", server, login.Registry, login.User)
	fmt.Fprintln(stdout, "  "+style.Muted.Render("The token is on "+server+" only, in root's Docker config."))
	return nil
}

// askLogin asks for the registry and the user that rest leaves out.
// Enter takes ghcr.io.
func askLogin(rest []string, stdin io.Reader, stdout io.Writer) (daemon.Registry, error) {
	var login daemon.Registry
	var err error
	if len(rest) > 0 {
		login.Registry = rest[0]
	} else if login.Registry, err = ask(stdin, stdout, "Registry:", "ghcr.io"); err != nil {
		return login, err
	}
	if login.User, err = ask(stdin, stdout, "User at "+login.Registry+":", ""); err != nil {
		return login, err
	}
	if login.User == "" {
		return login, errors.New("no user, nothing changed")
	}
	return login, nil
}

// removeRegistry logs the server out of a registry.
func removeRegistry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("registry rm", "registry rm <server> <registry> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, rest, err := flags.serverArgs(stdin, 1)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return flags.wrong()
	}
	name := rest[0]
	if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Log %s out of %s? It can no longer pull private images from it.", server, name)); err != nil {
		return err
	}
	if _, err := sshOutput(ctx, server, hereLine("registry", "rm", name), nil); err != nil {
		return err
	}
	done(stdout, "%s is logged out of %s", server, name)
	return nil
}

// bringImage puts the image of a deploy on the server. An image on this
// computer is the one that gets deployed: the server pulls it only when
// its registry has the very same one. Else it is sent over SSH. An image
// that is not here, the server pulls.
func bringImage(ctx context.Context, server, image string, stdout io.Writer) error {
	pull := func() error {
		why, err := pullImage(ctx, server, image)
		if why != "" {
			return fmt.Errorf("the image %s is in no registry %s can pull from, and not on this computer. Docker said: %s. A private registry needs a login: `%s registry set %s <registry> <user>`", image, server, why, build.Name, server)
		}
		return err
	}
	here, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", daemon.RepoDigests, "--", image).Output()
	if err != nil {
		return task(stdout, "pulling image "+image+" on "+server, pull)
	}
	var remote daemon.Digest
	err = task(stdout, "asking the registry for image "+image, func() error {
		return sshJSON(ctx, server, []string{"registry", "digest", image}, nil, &remote)
	})
	if err != nil {
		return err
	}
	if daemon.InDigests(string(here), remote.Digest) {
		return task(stdout, "pulling image "+image+" on "+server, pull)
	}
	return task(stdout, "sending image "+image, func() error {
		return sendImage(ctx, server, image)
	})
}

// pullImage makes the server pull image. When Docker there refuses, why
// says what it said, like "manifest unknown"; err is any other failure.
func pullImage(ctx context.Context, server, image string) (why string, err error) {
	var said bytes.Buffer
	var pulled daemon.Pulled
	err = sshAnswer(ctx, server, []string{"registry", "pull", image}, nil, &said, &pulled)
	var refused exitCode
	if !errors.As(err, &refused) {
		return "", err
	}
	why = strings.TrimSpace(strings.TrimPrefix(strings.Join(strings.Fields(said.String()), " "), style.SymbolFailed))
	return cmp.Or(strings.TrimSuffix(why, "."), err.Error()), nil
}

// startFollowImage sets the follow of an image on the server and pulls
// the image once.
func startFollowImage(ctx context.Context, server, app, image string, stdout io.Writer) error {
	body, _ := json.Marshal(map[string]string{"image": image})
	var f daemon.Follow
	if err := sshJSON(ctx, server, []string{"follow", app}, bytes.NewReader(body), &f); err != nil {
		return err
	}
	var check struct {
		Head string `json:"head"`
	}
	err := sshJSON(ctx, server, []string{"follow", "check", app}, nil, &check)
	if err != nil {
		// The server said why already, unless ssh failed.
		var said exitCode
		if !errors.As(err, &said) {
			printError(stdout, err)
		}
		hint(stdout, server+" tries again every minute, and alerts once while it cannot pull it. A private registry needs a login:", build.Name+" registry set "+server+" <registry> <user>")
		return errReported
	}
	id := strings.TrimPrefix(check.Head, "sha256:")
	done(stdout, "%s follows %s. It deploys %s within a minute, then each new image under that tag", app, image, id[:min(len(id), 12)])
	fmt.Fprintln(stdout, "  "+style.Muted.Render("Whoever can push to "+image+" deploys to "+server+". A deploy that fails alerts."))
	return nil
}

// hereRegistry is `here registry`, `here registry set` with the login as
// JSON on stdin, `here registry rm <registry>`, `here registry pull
// <image>` and `here registry digest <image>`.
func hereRegistry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		list, err := daemon.RegistriesHere()
		return respond(stdout, list, err)
	case len(args) == 1 && args[0] == "set":
		var login daemon.Registry
		if err := json.NewDecoder(stdin).Decode(&login); err != nil {
			return fmt.Errorf("read the login: %w", err)
		}
		return daemon.SetRegistryHere(ctx, login)
	case len(args) == 2 && args[0] == "rm":
		return daemon.RemoveRegistryHere(ctx, args[1])
	case len(args) == 2 && args[0] == "pull":
		pulled, err := daemon.PullHere(ctx, args[1])
		return respond(stdout, pulled, err)
	case len(args) == 2 && args[0] == "digest":
		digest, err := daemon.DigestHere(ctx, args[1])
		return respond(stdout, digest, err)
	}
	return errors.New("usage: here registry [set | rm <registry> | pull <image> | digest <image>]")
}
