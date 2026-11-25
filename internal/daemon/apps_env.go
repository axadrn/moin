package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/protocol"
)

// An app's env lives in one file on the server, apps/<app>/app.env, 0600,
// with the values as they are now: the hub forwards a value once and
// forgets it. No other file holds a value: the compose.json of a release
// names it as ${NAME}, and Docker fills it in from app.env when it starts
// the containers. Every release reads that file when it starts, a rollback
// too, so a value replaced or removed is gone from the server.

// envFile is the app's env file.
func (a *apps) envFile(app string) string { return filepath.Join(a.appDir(app), "app.env") }

// env reads the app's env, empty when it has none.
func (a *apps) env(app string) (map[string]string, error) {
	body, err := os.ReadFile(a.envFile(app))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	env, err := dotenv.Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", a.envFile(app), err)
	}
	return env, nil
}

// setEnv sets and removes env values of an app (EnvSet) and answers the
// keys the file has now. The file is replaced in one step, so a deploy
// reads it whole, before or after.
func (a *apps) setEnv(request protocol.EnvSet) (protocol.AppEnv, error) {
	if !protocol.ValidAppName(request.App) {
		return protocol.AppEnv{}, errors.New("invalid app name")
	}
	a.envMu.Lock()
	defer a.envMu.Unlock()
	env, err := a.env(request.App)
	if err != nil {
		return protocol.AppEnv{}, err
	}
	for key, value := range request.Set {
		if !envNamePattern.MatchString(key) {
			return protocol.AppEnv{}, fmt.Errorf("invalid env key %q", key)
		}
		env[key] = value
	}
	for _, key := range request.Remove {
		delete(env, key)
	}
	if err := writeEnv(a.envFile(request.App), env); err != nil {
		return protocol.AppEnv{}, err
	}
	slog.Info("env set", "app", request.App, "set", slices.Sorted(maps.Keys(request.Set)), "removed", request.Remove)
	return a.envKeys(request.App), nil
}

// makeSecrets gives each secret the compose file of the catalog app slug
// needs a random value, when the app has none yet. The server makes them,
// so they never pass the hub, and a newer version that asks for a new one
// gets it on its update.
func (a *apps) makeSecrets(app, slug string) error {
	compose, ok := catalog.Compose(slug)
	if !ok {
		return fmt.Errorf("the catalog of this daemon has no %s: update the daemon", slug)
	}
	a.envMu.Lock()
	defer a.envMu.Unlock()
	env, err := a.env(app)
	if err != nil {
		return err
	}
	made := []string{}
	for _, name := range catalog.Secrets(compose) {
		if _, ok := env[name]; !ok {
			env[name] = catalog.NewSecret(name)
			made = append(made, name)
		}
	}
	if len(made) == 0 {
		return nil
	}
	err = writeEnv(a.envFile(app), env)
	if err != nil {
		return err
	}
	slog.Info("secrets made", "app", app, "keys", made)
	return nil
}

// envKeys names the keys of the app's env file and when it last changed,
// never a value. An app without one, or with one moin cannot read, has
// none.
func (a *apps) envKeys(app string) protocol.AppEnv {
	keys := protocol.AppEnv{Keys: []string{}}
	info, err := os.Stat(a.envFile(app))
	if err != nil {
		return keys
	}
	keys.ChangedAt = info.ModTime().UTC()
	env, err := a.env(app)
	if err != nil {
		slog.Warn("read the env file", "app", app, "error", err)
		return keys
	}
	keys.Keys = slices.Sorted(maps.Keys(env))
	return keys
}

// allEnvKeys names the env keys of every app on the server, by its folder.
func (a *apps) allEnvKeys() map[string]protocol.AppEnv {
	entries, _ := os.ReadDir(a.dir)
	all := map[string]protocol.AppEnv{}
	for _, entry := range entries {
		if entry.IsDir() && protocol.ValidAppName(entry.Name()) {
			all[entry.Name()] = a.envKeys(entry.Name())
		}
	}
	return all
}

// ensureEnv makes the env file for an app that has none, so every release
// can name it: the one of the running release when there is one
// (moveOldEnv), else an empty one.
func (a *apps) ensureEnv(app string) error {
	a.envMu.Lock()
	defer a.envMu.Unlock()
	err := a.moveOldEnv(app)
	if err != nil || exists(a.envFile(app)) {
		return err
	}
	return writeEnv(a.envFile(app), map[string]string{})
}

// Up to v1.0.0-beta.34 the daemon wrote the env into the folder of each
// release, releases/<n>/app.env, and the hub kept the values. Now the hub
// keeps none, so the running release's file is the only copy. moveOldEnv
// copies it to apps/<app>/app.env when the app has no env file yet: the
// app keeps its values, a catalog app its passwords and keys. It runs when
// the daemon starts (moveOldEnvs) and before every deploy (ensureEnv). The
// caller holds envMu. Remove this once no server runs beta.34.
func (a *apps) moveOldEnv(app string) error {
	current := a.current(app)
	if current == 0 || exists(a.envFile(app)) {
		return nil
	}
	body, err := os.ReadFile(filepath.Join(a.releaseDir(app, current), "app.env"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	err = writeWhole(a.envFile(app), body)
	if err != nil {
		return err
	}
	slog.Info("env taken from the running release", "app", app, "release", current)
	return nil
}

// moveOldEnvs runs moveOldEnv for every app on the server.
func (a *apps) moveOldEnvs() {
	entries, _ := os.ReadDir(a.dir)
	for _, entry := range entries {
		if !entry.IsDir() || !protocol.ValidAppName(entry.Name()) {
			continue
		}
		a.envMu.Lock()
		err := a.moveOldEnv(entry.Name())
		a.envMu.Unlock()
		if err != nil {
			slog.Warn("take the env of the running release", "app", entry.Name(), "error", err)
		}
	}
}

// writeEnv replaces the env file with env, 0600, whole (writeWhole).
func writeEnv(file string, env map[string]string) error {
	return writeWhole(file, []byte(dotenv.Format(env)))
}

// envArgs are the --env-file arguments compose reads a compose file with,
// to fill its ${NAME}: the .env in dir first, when there is one, then the
// app's env, whose values win. dir is the source for the compose file of a
// repo, and the release's folder for the compose.json moin runs, which
// keeps a copy of the source's .env.
//
// The env goes to compose in a file, never in docker's own environment:
// docker reads variables like DOCKER_CONFIG and DOCKER_HOST itself, so an
// env var of the app could make it run another program, as root.
func (a *apps) envArgs(app, dir string) []string {
	var args []string
	for _, file := range []string{filepath.Join(dir, ".env"), a.envFile(app)} {
		if exists(file) {
			args = append(args, "--env-file", file)
		}
	}
	return args
}
