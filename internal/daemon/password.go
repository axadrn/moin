package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/axadrn/moin/internal/build"
)

// An app's password lives on the server in apps/<app>/htpasswd: one line,
// user:hash, the hash made here with bcrypt. With it the proxy asks every
// visitor of the app for the user and the password, on every address.
//
// The proxy reads a password file once, when a router that names it
// appears. So every release gets its own copy of the file and names that
// one: a new password reaches the proxy with the release that starts
// for it.

// PasswordUser is the user of an app's password when you name none.
const PasswordUser = build.Name

// passwordUser is a user name: no colon, which ends it in the file, and
// no space.
var passwordUser = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// bcryptLimit is how many bytes of a password bcrypt reads.
const bcryptLimit = 72

// passwordFile is the file of the app's password.
func (a *apps) passwordFile(app string) string { return filepath.Join(a.appDir(app), "htpasswd") }

// hasPassword reports whether the app asks for a password.
func (a *apps) hasPassword(app string) bool { return exists(a.passwordFile(app)) }

// SetPasswordHere makes the app ask every visitor for user and password,
// and starts it with them, without a build. When that fails, the
// password stays as it was.
func SetPasswordHere(ctx context.Context, dir, app, user, password string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	if user == "" {
		user = PasswordUser
	}
	if !passwordUser.MatchString(user) {
		return Result{}, fmt.Errorf("%q is not a user name: letters, digits, dots and dashes", user)
	}
	line, err := passwordLine(user, password)
	if err != nil {
		return Result{}, err
	}
	return a.changePassword(ctx, app, []byte(line), out)
}

// RemovePasswordHere lets every visitor in again and starts the app
// without its password.
func RemovePasswordHere(ctx context.Context, dir, app string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	if !a.hasPassword(app) {
		return Result{}, fmt.Errorf("%s has no password", app)
	}
	return a.changePassword(ctx, app, nil, out)
}

// passwordLine is the line of a password file: the user and the bcrypt
// hash of the password.
func passwordLine(user, password string) (string, error) {
	if password == "" {
		return "", errors.New("no password, nothing changed")
	}
	if len(password) > bcryptLimit {
		return "", fmt.Errorf("the password is longer than %d bytes, and the rest would not count: take a shorter one", bcryptLimit)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return user + ":" + string(hash) + "\n", nil
}

// changePassword writes the app's password file, or removes it for no
// line, and applies it. A failed deploy puts the old file back, as the
// old release runs again.
func (a *apps) changePassword(ctx context.Context, app string, line []byte, out io.Writer) (Result, error) {
	file := a.passwordFile(app)
	old, _ := os.ReadFile(file)
	if err := writePassword(file, line); err != nil {
		return Result{}, err
	}
	result, err := a.deployHere(ctx, a.newStart(app), out)
	if err != nil {
		_ = writePassword(file, old)
	}
	return result, err
}

// writePassword writes a password file for root alone, or removes it for
// no line.
func writePassword(file string, line []byte) error {
	if len(line) == 0 {
		err := os.Remove(file)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return writeWhole(file, line)
}

// proxyApps is where the proxy sees the apps folder, read-only.
const proxyApps = "/apps"

// releasePassword copies the app's password file into a release and says
// where the proxy reads the copy, "" for an app without a password.
func releasePassword(release *release) (string, error) {
	line, err := os.ReadFile(release.passwordFile)
	if release.passwordFile == "" || errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := writeWhole(filepath.Join(release.dir, "htpasswd"), line); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/releases/%d/htpasswd", proxyApps, release.start.App, release.start.Number), nil
}

// dropPassword takes the app's password out of a compose file to run
// elsewhere: the file it names stays on this server.
func dropPassword(app string, config map[string]any) {
	auth := project(app) + "-auth"
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		labels, _ := service["labels"].(map[string]any)
		for key, value := range labels {
			if strings.HasPrefix(key, "traefik.http.middlewares."+auth+".") {
				delete(labels, key)
				continue
			}
			if !strings.HasPrefix(key, "traefik.http.routers.") || !strings.HasSuffix(key, ".middlewares") {
				continue
			}
			others := slices.DeleteFunc(strings.Split(fmt.Sprint(value), ","), func(name string) bool { return name == auth })
			if len(others) == 0 {
				delete(labels, key)
			} else {
				labels[key] = strings.Join(others, ",")
			}
		}
	}
}
