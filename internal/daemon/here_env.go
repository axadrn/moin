package daemon

import (
	"fmt"
	"maps"
	"slices"

	"github.com/axadrn/moin/internal/dotenv"
)

// EnvNamesHere names the app's env vars, never a value.
func EnvNamesHere(dir, app string) ([]string, error) {
	a := hereApps(dir)
	if err := a.mustHave(app); err != nil {
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
	a := hereApps(dir)
	if err := a.mustHave(app); err != nil {
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
func SetEnvHere(dir, app, text string) (dotenv.Change, error) {
	a := hereApps(dir)
	if err := a.mustHave(app); err != nil {
		return dotenv.Change{}, err
	}
	next, err := dotenv.Parse(text)
	if err != nil {
		return dotenv.Change{}, err
	}
	for key := range next {
		if !envNamePattern.MatchString(key) {
			return dotenv.Change{}, fmt.Errorf("%q is not an env name: letters, digits and _, like DATABASE_URL", key)
		}
	}
	a.envMu.Lock()
	defer a.envMu.Unlock()
	old, err := a.env(app)
	if err != nil {
		return dotenv.Change{}, err
	}
	change := dotenv.Diff(old, next)
	if change.None() {
		return change, nil
	}
	return change, writeEnv(a.envFile(app), next)
}
