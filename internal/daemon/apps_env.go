package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

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
	env, err := readEnvFile(a.envFile(app))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	return env, err
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

// Up to v1.0.0-beta.34 the hub kept the env values and sent them with
// every deploy, and the daemon wrote them into the release:
//
//   - a deploy from source, and any deploy of an app with a service to
//     build, wrote them to releases/<n>/app.env;
//   - every deploy but a rollback wrote them into compose.json: the main
//     service's environment is the one of base.json, the compose file as
//     docker compose read it, plus each env key, with $ written $$;
//   - a rollback copied compose.json and base.json of the older release.
//
// Now the hub keeps none, so the running release is the only copy.
// moveOldEnv writes it to apps/<app>/app.env when the app has no env file
// yet: from releases/<current>/app.env when there is one, else from its
// compose.json (envOfCompose), where a key an older app.env names counts
// as env, and so does each env key of a catalog app (catalogNames). The
// app keeps its values, a catalog app its passwords and keys. It runs when
// the daemon starts (moveOldEnvs) and before every deploy (ensureEnv), and
// logs where the env came from. The caller holds envMu. Remove this once
// no server runs beta.34.
func (a *apps) moveOldEnv(app string) error {
	current := a.current(app)
	if current == 0 || exists(a.envFile(app)) {
		return nil
	}
	dir := a.releaseDir(app, current)
	from := filepath.Join("releases", strconv.Itoa(current), "app.env")
	env, err := readEnvFile(filepath.Join(a.appDir(app), from))
	withoutBase := false
	if errors.Is(err, os.ErrNotExist) {
		from = filepath.Join("releases", strconv.Itoa(current), "compose.json")
		withoutBase = !exists(filepath.Join(dir, "base.json"))
		named := a.oldEnvKeys(app)
		for _, name := range a.catalogNames(app) {
			named[name] = true
		}
		env, err = envOfCompose(dir, named)
	}
	if err != nil {
		slog.Warn("no env found in the running release: the next deploy starts with an empty env, set it again with moin env set", "app", app, "release", current, "error", err)
		return nil
	}
	err = writeEnv(a.envFile(app), env)
	if err != nil {
		return err
	}
	keys := slices.Sorted(maps.Keys(env))
	if withoutBase {
		slog.Warn("env taken from the running release without its base.json: only the keys an older app.env or the catalog names", "app", app, "release", current, "from", from, "keys", keys)
		return nil
	}
	slog.Info("env taken from the running release", "app", app, "release", current, "from", from, "keys", keys)
	return nil
}

// readEnvFile reads an env file.
func readEnvFile(file string) (map[string]string, error) {
	body, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	env, err := dotenv.Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return env, nil
}

// oldEnvKeys names the keys of every releases/<n>/app.env of the app.
func (a *apps) oldEnvKeys(app string) map[string]bool {
	files, _ := filepath.Glob(filepath.Join(a.appDir(app), "releases", "*", "app.env"))
	keys := map[string]bool{}
	for _, file := range files {
		env, _ := readEnvFile(file)
		for key := range env {
			keys[key] = true
		}
	}
	return keys
}

// envOfCompose is the env beta.34 wrote into the compose.json of the
// release in dir, with $$ read as $ again. Its main service has the
// environment of base.json plus the env, so a key is the env's when
// base.json lacks it or has another value. A key with the value base.json
// has too, such as ADMIN_PASSWORD: ${ADMIN_PASSWORD}, is the env's when
// an app.env of the app or the catalog names it (named); any other, such
// as TZ: UTC, is the compose file's. Without base.json only the named keys count.
func envOfCompose(dir string, named map[string]bool) (map[string]string, error) {
	compose, err := readJSON(filepath.Join(dir, "compose.json"))
	if err != nil {
		return nil, err
	}
	var detected protocol.Detected
	body, err := os.ReadFile(filepath.Join(dir, "detected.json"))
	if err == nil {
		err = json.Unmarshal(body, &detected)
	}
	if err != nil || detected.Service == "" {
		return nil, errors.New("detected.json names no main service")
	}
	base, _ := readJSON(filepath.Join(dir, "base.json"))
	environment := serviceEnvironment(compose, detected.Service)
	baseEnvironment := serviceEnvironment(base, detected.Service)
	env := map[string]string{}
	for key, item := range environment {
		value, ok := item.(string)
		if !ok || !envNamePattern.MatchString(key) {
			continue
		}
		baseValue, inBase := baseEnvironment[key].(string)
		if named[key] || base != nil && (!inBase || baseValue != value) {
			env[key] = strings.ReplaceAll(value, "$$", "$")
		}
	}
	return env, nil
}

// serviceEnvironment is the environment of a service of a compose file
// docker compose read, nil for none.
func serviceEnvironment(config map[string]any, name string) map[string]any {
	services, _ := config["services"].(map[string]any)
	service, _ := services[name].(map[string]any)
	environment, _ := service["environment"].(map[string]any)
	return environment
}

// moveOldEnvs runs moveOldEnv and repairCatalogEnv for every app on the
// server.
func (a *apps) moveOldEnvs(ctx context.Context) {
	entries, _ := os.ReadDir(a.dir)
	for _, entry := range entries {
		if !entry.IsDir() || !protocol.ValidAppName(entry.Name()) {
			continue
		}
		a.envMu.Lock()
		err := a.moveOldEnv(entry.Name())
		if err == nil {
			err = a.repairCatalogEnv(ctx, entry.Name())
		}
		a.envMu.Unlock()
		if err != nil {
			slog.Warn("take the env of the running release", "app", entry.Name(), "error", err)
		}
	}
}

// Up to v1.0.0-beta.35 moveOldEnv left out a key of a catalog app whose
// value base.json has too when no app.env named it, such as
// APP_SECRET: ${APP_SECRET} of umami, so the app would start again
// without it. repairCatalogEnv adds each env key of a catalog app
// (catalogNames) its app.env lacks: from the compose.json of the running
// release (envOfCompose), else from the container of its main service, as
// Docker runs it. A value app.env has stays. It runs when the daemon
// starts (moveOldEnvs), before hello names the keys, and logs the keys it
// added, never a value. The caller holds envMu. Remove this once no server
// runs beta.35.
func (a *apps) repairCatalogEnv(ctx context.Context, app string) error {
	if !exists(a.envFile(app)) {
		return nil
	}
	env, err := a.env(app)
	if err != nil {
		return err
	}
	missing := map[string]bool{}
	for _, name := range a.catalogNames(app) {
		if _, ok := env[name]; !ok {
			missing[name] = true
		}
	}
	if len(missing) == 0 {
		return nil
	}
	fromCompose, _ := envOfCompose(a.releaseDir(app, a.current(app)), missing)
	var fromDocker map[string]string
	added := []string{}
	for _, name := range slices.Sorted(maps.Keys(missing)) {
		value := fromCompose[name]
		// A release from beta.35 names the key and holds no value.
		if value == "" || value == "${"+name+"}" {
			if fromDocker == nil {
				fromDocker = a.containerEnv(ctx, app)
			}
			value = fromDocker[name]
		}
		if value != "" {
			env[name] = value
			added = append(added, name)
			delete(missing, name)
		}
	}
	if len(added) > 0 {
		err = writeEnv(a.envFile(app), env)
		if err != nil {
			return err
		}
		slog.Info("env of the catalog app repaired", "app", app, "keys", added)
	}
	if len(missing) > 0 {
		slog.Warn("a key of the catalog app is neither in its env nor in the running release: set it with moin env set if the app needs it", "app", app, "keys", slices.Sorted(maps.Keys(missing)))
	}
	return nil
}

// catalogNames names the env keys of a catalog app: the ${NAME}s of its
// compose file the hub or the server gave a value, its secrets, VERSION
// and the URL vars. None for an app of your own. A server keeps no note of
// which catalog app an app is, so the image of the main service of the
// running release tells: the catalog app whose compose file runs it with
// VERSION as the tag, such as umami for ghcr.io/umami-software/umami:2.19.0.
// Remove this once no server runs beta.35.
func (a *apps) catalogNames(app string) []string {
	detected, _ := a.live(app)
	image := a.serviceImage(app, detected.Service)
	colon := strings.LastIndex(image, ":")
	if colon < 0 || strings.Contains(image[colon:], "/") {
		return nil
	}
	line := "image: " + image[:colon] + ":${" + catalog.VersionVar + "}"
	folders, _ := fs.ReadDir(catalog.Files, ".")
	for _, folder := range folders {
		compose, _ := catalog.Compose(folder.Name())
		if !strings.Contains(string(compose), line) {
			continue
		}
		names := catalog.Secrets(compose)
		for _, name := range []string{catalog.VersionVar, catalog.URLVar, catalog.DomainVar} {
			if strings.Contains(string(compose), "${"+name+"}") {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}

// containerEnv is the env of the container of the app's main service, as
// Docker has it: docker inspect. Empty when there is none.
func (a *apps) containerEnv(ctx context.Context, app string) map[string]string {
	env := map[string]string{}
	detected, _ := a.live(app)
	if !validService(detected.Service) {
		return env
	}
	out, err := a.output(ctx, "docker", "ps", "-aq",
		"--filter", "label=com.docker.compose.project="+a.projectOf(app),
		"--filter", "label=com.docker.compose.service="+detected.Service)
	container, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if err != nil || container == "" {
		return env
	}
	inspected, err := a.output(ctx, "docker", "inspect", "-f", "{{json .Config.Env}}", container)
	var items []string
	if err != nil || json.Unmarshal([]byte(inspected), &items) != nil {
		return env
	}
	for _, item := range items {
		key, value, _ := strings.Cut(item, "=")
		env[key] = value
	}
	return env
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
