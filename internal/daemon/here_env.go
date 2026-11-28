package daemon

import (
	"fmt"
	"maps"
	"slices"

	"github.com/axadrn/moin/internal/dotenv"
)

// The env of an app works before its first deploy too: an app that needs
// DATABASE_URL to start gets it first. So these check the app's name,
// not that it runs.

// EnvSaved is what a change of the env did, by name, and whether the app
// runs a release that starts again with it. Before the first deploy it
// does not: the env waits for that deploy.
type EnvSaved struct {
	dotenv.Change
	Live bool `json:"live"`
}

// ValidEnvName reports whether name can name an env var, like
// DATABASE_URL.
func ValidEnvName(name string) bool { return envNamePattern.MatchString(name) }

// EnvNamesHere names the app's env vars, never a value.
func EnvNamesHere(dir, app string) ([]string, error) {
	a := newApps(dir)
	if err := checkAppName(app); err != nil {
		return nil, err
	}
	env, err := a.env(app)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(env)), nil
}

// EnvFileHere is the app's env as a .env text, values and all, for the
// editor of `moin env edit` on your laptop.
func EnvFileHere(dir, app string) (string, error) {
	a := newApps(dir)
	if err := checkAppName(app); err != nil {
		return "", err
	}
	env, err := a.env(app)
	if err != nil {
		return "", err
	}
	return dotenv.Format(env), nil
}

// SetEnvHere replaces the app's env with the .env text and says what
// changed, by name. The app takes it on its next start.
func SetEnvHere(dir, app, text string) (EnvSaved, error) {
	next, err := dotenv.Parse(text)
	if err != nil {
		return EnvSaved{}, err
	}
	return newApps(dir).saveEnv(app, func(map[string]string) map[string]string { return next })
}

// SetEnvVarHere sets one env var of the app, the others stay.
func SetEnvVarHere(dir, app, name, value string) (EnvSaved, error) {
	return newApps(dir).saveEnv(app, func(old map[string]string) map[string]string {
		next := maps.Clone(old)
		next[name] = value
		return next
	})
}

// saveEnv writes the env change makes of the one there is, when it
// changes anything.
func (a *apps) saveEnv(app string, change func(old map[string]string) map[string]string) (EnvSaved, error) {
	if err := checkAppName(app); err != nil {
		return EnvSaved{}, err
	}
	a.envMu.Lock()
	defer a.envMu.Unlock()
	old, err := a.env(app)
	if err != nil {
		return EnvSaved{}, err
	}
	next := change(old)
	for name := range next {
		if !ValidEnvName(name) {
			return EnvSaved{}, fmt.Errorf("%q is not an env name: letters, digits and _, like DATABASE_URL", name)
		}
	}
	saved := EnvSaved{Change: dotenv.Diff(old, next), Live: a.current(app) > 0}
	if saved.None() {
		return saved, nil
	}
	return saved, writeEnv(a.envFile(app), next)
}
