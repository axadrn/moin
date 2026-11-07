package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axadrn/moin/protocol"
)

// An app lives in its folder, the name it was created with, which never
// changes. Its Compose project is its name now, so docker ps shows
// my-app-web-1; the project file in the folder says which one runs. Its
// named volumes have fixed names, kept in volumes.json, so a rename never
// loses data: moin-<app ID>-<volume>, or for an app from before renames
// the name compose gave it, moin-<folder>_<volume>. Its networks follow
// the project.

// projectOf is the Compose project the app runs as: the project file, or
// for an app from before renames moin-<folder>.
func (a *apps) projectOf(app string) string {
	body, _ := os.ReadFile(filepath.Join(a.appDir(app), "project"))
	if name := strings.TrimSpace(string(body)); protocol.ValidAppName(name) {
		return name
	}
	return project(app)
}

// nameOf is the name a deploy gives the app's project: its name now, or
// its folder from a hub that sends none.
func nameOf(start protocol.DeployStart) string {
	if protocol.ValidAppName(start.Name) {
		return start.Name
	}
	return start.App
}

// switchProject makes name the app's project before it starts. After a
// rename it takes the containers of the old project down first, so
// nothing runs twice; volumes and images stay. A project another app of
// this server runs as is refused.
func (a *apps) switchProject(ctx context.Context, app, name string, logf func(string, ...any)) error {
	old := a.projectOf(app)
	if old == name {
		return nil
	}
	for _, other := range a.names() {
		if other != app && a.projectOf(other) == name {
			return fmt.Errorf("another app on this server runs as %s: rename one of them", name)
		}
	}
	logf("the containers take the name %s", name)
	if _, err := a.output(ctx, "docker", "compose", "-p", old, "down", "--remove-orphans"); err != nil {
		return fmt.Errorf("stop the containers of %s: %w", old, err)
	}
	return os.WriteFile(filepath.Join(a.appDir(app), "project"), []byte(name), 0o600)
}

// volumeName is the fixed name of a named volume of the app, chosen once
// and kept in volumes.json: the name compose gave it before renames when
// that volume exists, else moin-<app ID>-<volume>.
func (a *apps) volumeName(ctx context.Context, app, appID, volume string) string {
	file := filepath.Join(a.appDir(app), "volumes.json")
	names := map[string]string{}
	body, _ := os.ReadFile(file)
	_ = json.Unmarshal(body, &names)
	if name := names[volume]; name != "" {
		return name
	}
	name := project(app) + "_" + volume
	if _, err := a.output(ctx, "docker", "volume", "inspect", name); err != nil && appIDPattern.MatchString(appID) {
		name = ourName + "-" + strings.ReplaceAll(appID, "-", "") + "-" + volume
	}
	names[volume] = name
	saveJSON(file, names)
	return name
}

// volumeNames are the fixed names of the app's named volumes.
func (a *apps) volumeNames(app string) []string {
	names := map[string]string{}
	body, _ := os.ReadFile(filepath.Join(a.appDir(app), "volumes.json"))
	_ = json.Unmarshal(body, &names)
	var out []string
	for _, name := range names {
		out = append(out, name)
	}
	return out
}

// pinNames gives a compose file of the app the names that outlive a
// rename: each named volume compose named after the project gets its
// fixed name, each network compose named after a project takes name's,
// and every service carries the app's folder in a label, for metrics.
// Names someone chose stay, and so do external volumes and networks.
func pinNames(config map[string]any, app, name string, volume func(string) string) {
	volumes, _ := config["volumes"].(map[string]any)
	for key, item := range volumes {
		spec, _ := item.(map[string]any)
		if spec == nil {
			spec = map[string]any{}
			volumes[key] = spec
		}
		current, _ := spec["name"].(string)
		if spec["external"] == true || current != "" && current != project(app)+"_"+key {
			continue
		}
		spec["name"] = volume(key)
	}
	networks, _ := config["networks"].(map[string]any)
	for key, item := range networks {
		spec, _ := item.(map[string]any)
		if spec == nil {
			spec = map[string]any{}
			networks[key] = spec
		}
		current, _ := spec["name"].(string)
		if spec["external"] == true || current != "" && !strings.HasSuffix(current, "_"+key) {
			continue
		}
		spec["name"] = name + "_" + key
	}
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		if service == nil {
			continue
		}
		labels, _ := service["labels"].(map[string]any)
		if labels == nil {
			labels = map[string]any{}
		}
		labels[labelApp] = app
		service["labels"] = labels
	}
}

// pinFile pins the names of a compose file of a deploy.
func (a *apps) pinFile(ctx context.Context, start protocol.DeployStart, composeFile string) error {
	body, err := os.ReadFile(composeFile)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(body, &config); err != nil {
		return err
	}
	pinNames(config, start.App, nameOf(start), func(volume string) string { return a.volumeName(ctx, start.App, start.AppID, volume) })
	body, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(composeFile, body, 0o600)
}
