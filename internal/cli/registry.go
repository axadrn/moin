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
	server, err := flags.server()
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
	if flags.NArg() != 3 {
		return flags.wrong()
	}
	server := flags.Arg(0)
	login := daemon.Registry{Registry: flags.Arg(1), User: flags.Arg(2)}
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

// removeRegistry logs the server out of a registry.
func removeRegistry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("registry rm", "registry rm <server> <registry> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	server, name := flags.Arg(0), flags.Arg(1)
	if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Log %s out of %s? It can no longer pull private images from it.", server, name)); err != nil {
		return err
	}
	if _, err := sshOutput(ctx, server, hereLine("registry", "rm", name), nil); err != nil {
		return err
	}
	done(stdout, "%s is logged out of %s", server, name)
	return nil
}

// bringImage puts the image of a deploy on the server. The server pulls
// it from its registry. An image no registry has, like shop:dev, is one
// you built here: it is sent over SSH.
func bringImage(ctx context.Context, server, image string, stdout io.Writer) error {
	here := exec.CommandContext(ctx, "docker", "image", "inspect", "--", image).Run() == nil
	if !here {
		return task(stdout, "pulling image "+image+" on "+server, func() error {
			why, err := pullImage(ctx, server, image)
			if why != "" {
				return fmt.Errorf("%s cannot pull %s, and this computer has no such image. Docker said: %s. A private registry needs a login: `%s registry set %s <registry> <user>`", server, image, why, build.Name, server)
			}
			return err
		})
	}
	pulled := false
	err := task(stdout, "asking the registry for image "+image, func() error {
		why, err := pullImage(ctx, server, image)
		pulled = why == "" && err == nil
		return err
	})
	if err != nil || pulled {
		return err
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
// JSON on stdin, `here registry rm <registry>` and `here registry pull
// <image>`.
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
	}
	return errors.New("usage: here registry [set | rm <registry> | pull <image>]")
}
