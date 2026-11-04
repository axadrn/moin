package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// The agent of the old name (build.LegacyName) ran as the legacy unit from
// /usr/local/bin/<legacy>, kept its state in /var/lib/<legacy> and gave its
// Compose projects, proxy, tunnel, firewall, labels and WAL-G slot the old
// name. The new binary moves all of it once, by itself, with no data lost:
//
//  1. MoveUnit, in the old unit: the binary goes to /usr/local/bin/<name>,
//     the old path becomes a link to it, and the new unit starts. It
//     conflicts with the old one, so systemd stops the old agent first:
//     two never run at once.
//  2. MoveState, in the new unit: /var/lib/<legacy> becomes
//     /var/lib/<name>, with a link at the old path.
//  3. rename.run, before the first session: each app moves to its new
//     Compose project, its named volumes copied and checked file by file,
//     while the old proxy, joined to the new network, keeps serving; then
//     the proxy, the tunnel and the firewall change names. An app that
//     fails to start under its new name goes back to its old project and
//     keeps running there; the next start tries again.
//
// Every step looks before it acts, so a crash anywhere picks up at the next
// start. What the old name left (its volumes, the links, the copies of the
// compose files) goes after build.LegacyUntil.

// migrationFile keeps what the rename did, in the state dir.
const migrationFile = "migration.json"

type migrationState struct {
	// Apps is "done" per app that moved, or why it did not.
	Apps map[string]string `json:"apps"`
	// Running is whether an app ran before it stopped to move.
	Running map[string]bool `json:"running"`
	// Volumes are the old volumes copied and checked, kept until
	// build.LegacyUntil.
	Volumes []string `json:"volumes"`
	// Done is set when every app and the proxy moved.
	Done bool `json:"done"`
	// Cleaned is set when the old name's leftovers are gone.
	Cleaned bool `json:"cleaned,omitempty"`
}

// inLegacyUnit reports whether this process runs in the old unit.
func inLegacyUnit() bool {
	body, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}
	return strings.Contains(string(body), "/"+legacyUnit+".service")
}

// MoveUnit moves an agent that runs in the unit of the old name to the new
// one. It reports true when it handed over: systemd stops this process in
// a moment, and the caller waits for that.
func MoveUnit(ctx context.Context) (bool, error) {
	if os.Geteuid() != 0 || !inLegacyUnit() {
		return false, nil
	}
	slog.Info("moving the agent to its new name", "from", legacyUnit, "to", build.Name+"-agent")
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	if executable != installedPath {
		if err := copyBinary(executable, installedPath); err != nil {
			return false, fmt.Errorf("install %s: %w", installedPath, err)
		}
	}
	// The old path stays as a link to the new binary until LegacyUntil:
	// scripts and fingers that type the old name keep working.
	if err := replaceWithLink(legacyBinary, installedPath); err != nil {
		return false, fmt.Errorf("link %s: %w", legacyBinary, err)
	}
	if err := os.WriteFile(unitFile, []byte(protocol.AgentUnit(build.Name, legacy)), 0o644); err != nil {
		return false, err
	}
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", build.Name + "-agent"},
		{"disable", legacyUnit},
	} {
		if out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput(); err != nil {
			return false, fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	slog.Info("starting the agent under its new name; this one stops")
	if out, err := exec.CommandContext(ctx, "systemctl", "--no-block", "start", build.Name+"-agent").CombinedOutput(); err != nil {
		return false, fmt.Errorf("start %s-agent: %v %s", build.Name, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// copyBinary copies the running binary to target in one rename.
func copyBinary(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+"-move-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(out.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(out.Name(), target)
}

// replaceWithLink makes path a symlink to target, in one rename.
func replaceWithLink(path, target string) error {
	if current, err := os.Readlink(path); err == nil && current == target {
		return nil
	}
	temp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"-link")
	_ = os.Remove(temp)
	if err := os.Symlink(target, temp); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// MoveState moves the state of the old name to dir, the default state dir,
// and removes the old unit once the new one runs.
func MoveState(ctx context.Context, dir string) error {
	if os.Geteuid() != 0 || dir != DefaultStateDir {
		return nil
	}
	if !inLegacyUnit() {
		if _, err := os.Stat(legacyUnitFile); err == nil {
			_ = exec.CommandContext(ctx, "systemctl", "disable", legacyUnit).Run()
			if err := os.Remove(legacyUnitFile); err == nil {
				slog.Info("removed the unit of the old name", "unit", legacyUnit)
				_ = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
			}
		}
	}
	return moveStateDir(legacyStateDir, dir)
}

// moveStateDir renames from to to and leaves a link at from. A crash
// between the two leaves to without a link: the next run adds it.
func moveStateDir(from, to string) error {
	info, err := os.Lstat(from)
	if errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(to); err == nil && build.Legacy() {
			if _, err := os.Lstat(filepath.Join(to, migrationFile)); err == nil {
				return os.Symlink(to, from)
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(to, stateFile)); err == nil {
		return fmt.Errorf("both %s and %s hold an agent: leaving %s as it is", from, to, from)
	}
	if entries, err := os.ReadDir(to); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty: leaving %s as it is", to, from)
	}
	_ = os.Remove(to)
	// The mark goes in first and moves along: it says this state came from
	// the old name, so the Docker side moves too, and a crash before the
	// link still gets one.
	if _, err := os.Stat(filepath.Join(from, migrationFile)); err != nil {
		saveJSON(filepath.Join(from, migrationFile), migrationState{Apps: map[string]string{}, Running: map[string]bool{}})
	}
	slog.Info("moving the state", "from", from, "to", to)
	if err := os.Rename(from, to); err != nil {
		return err
	}
	return os.Symlink(to, from)
}

// removeLegacyStateLink removes the link the move left at the old state
// dir, when it points to dir.
func removeLegacyStateLink(dir string) {
	if target, err := os.Readlink(legacyStateDir); err == nil && target == dir {
		_ = os.Remove(legacyStateDir)
	}
}

// rename moves the Docker side: apps, proxy, tunnel.
type rename struct {
	// marked is set when the state came from the old name: only then the
	// Docker side moves. A fresh agent, or one in development with its own
	// state dir, never touches the old name's containers.
	marked   bool
	dir      string
	apps     *apps
	security *security
	state    migrationState
	// helper is an image with a shell to copy volumes with.
	helper string
}

func newRename(dir string, a *apps, s *security) *rename {
	m := &rename{dir: dir, apps: a, security: s, helper: proxyImage}
	body, err := os.ReadFile(filepath.Join(dir, migrationFile))
	m.marked = err == nil
	_ = json.Unmarshal(body, &m.state)
	if m.state.Apps == nil {
		m.state.Apps = map[string]string{}
	}
	if m.state.Running == nil {
		m.state.Running = map[string]bool{}
	}
	return m
}

func (m *rename) save() { saveJSON(filepath.Join(m.dir, migrationFile), m.state) }

// needed reports whether anything of the old name is left in Docker.
func (m *rename) needed(ctx context.Context) bool {
	if !m.marked || m.state.Done {
		return false
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	out, err := m.apps.output(ctx, "docker", "ps", "-a", "-q", "--filter", "label=com.docker.compose.project", "--filter", "name=^"+legacy+"-")
	if err == nil && strings.TrimSpace(out) != "" {
		return true
	}
	for _, check := range [][]string{
		{"container", "inspect", legacyProxyName},
		{"container", "inspect", legacyTunnelName},
		{"network", "inspect", legacyProxyNetwork},
	} {
		if _, err := m.apps.output(ctx, "docker", check...); err == nil {
			return true
		}
	}
	volumes, err := m.apps.output(ctx, "docker", "volume", "ls", "-q", "--filter", "label=com.docker.compose.project")
	if err == nil {
		for _, volume := range strings.Fields(volumes) {
			if strings.HasPrefix(volume, legacy+"-") && !slices.Contains(m.state.Volumes, volume) {
				return true
			}
		}
	}
	return false
}

// run moves everything that still has the old name. It never returns an
// error that stops the agent: an app that cannot move keeps running under
// its old name.
func (m *rename) run(ctx context.Context) {
	for app, status := range m.state.Apps {
		if status != "done" {
			legacyApps.Store(app, true)
		}
	}
	if !build.Legacy() && m.marked {
		m.cleanup(ctx)
	}
	if !m.needed(ctx) {
		return
	}
	logf := func(format string, args ...any) { slog.Info("move: " + fmt.Sprintf(format, args...)) }
	logf("moving apps, proxy and tunnel to the new name")
	if m.dir == DefaultStateDir {
		if err := rewritePaths(m.apps.dir, legacyStateDir+"/", m.dir+"/"); err != nil {
			slog.Error("move: rewrite paths", "error", err)
			return
		}
		_ = rewriteFile(filepath.Join(m.dir, "backup-targets.json"), legacyStateDir+"/", m.dir+"/")
	}
	// The companions of the old name follow the old containers: they go,
	// and the backup loop starts new ones once the apps run.
	if out, err := m.apps.output(ctx, "docker", "ps", "-a", "-q", "--filter", "label="+legacyLabelWalg); err == nil && strings.TrimSpace(out) != "" {
		_, _ = m.apps.output(ctx, "docker", append([]string{"rm", "-f"}, strings.Fields(out)...)...)
	}
	// The old proxy serves the moved apps too until the end: it joins the
	// new network, and their labels name it.
	if _, err := m.apps.output(ctx, "docker", "network", "inspect", proxyNetwork); err != nil {
		if _, err := m.apps.output(ctx, "docker", "network", "create", proxyNetwork); err != nil {
			slog.Error("move: create the network", "error", err)
			return
		}
	}
	if _, err := m.apps.output(ctx, "docker", "container", "inspect", legacyProxyName); err == nil {
		_, _ = m.apps.output(ctx, "docker", "network", "connect", proxyNetwork, legacyProxyName)
	}
	failed := false
	for _, app := range m.apps.names() {
		if m.state.Apps[app] == "done" {
			continue
		}
		if err := m.moveApp(ctx, app, logf); err != nil {
			slog.Error("move: app stays under its old name until the next start", "app", app, "error", err)
			m.state.Apps[app] = err.Error()
			legacyApps.Store(app, true)
			failed = true
		} else {
			m.state.Apps[app] = "done"
			legacyApps.Delete(app)
		}
		m.save()
	}
	// Now the proxy: the new one takes the ports and the certificates.
	if err := m.apps.ensureProxy(ctx, logf); err != nil {
		slog.Error("move: start the proxy", "error", err)
		return
	}
	m.apps.joinLegacyNetwork(ctx)
	if _, err := m.apps.output(ctx, "docker", "container", "inspect", legacyTunnelName); err == nil {
		logf("moving the Cloudflare Tunnel")
		_, _ = m.apps.output(ctx, "docker", "rm", "-f", legacyTunnelName)
		if err := m.security.ensureTunnel(ctx, m.security.current().TunnelToken); err != nil {
			slog.Error("move: start the tunnel", "error", err)
		}
	}
	if !failed {
		// Gone once nothing uses it; a control plane on this server may
		// still, until it moves too.
		_, _ = m.apps.output(ctx, "docker", "network", "rm", legacyProxyNetwork)
		m.state.Done = true
		logf("done: every app runs under the new name")
	}
	m.save()
}

// moveApp moves one app to its new Compose project.
func (m *rename) moveApp(ctx context.Context, app string, logf func(string, ...any)) error {
	old, current := legacyProject(app), ourName+"-"+app
	states, err := m.apps.output(ctx, "docker", "ps", "-a", "--filter", "label=com.docker.compose.project="+old, "--format", "{{.State}}")
	if err != nil {
		return err
	}
	volumes, err := m.oldVolumes(ctx, app)
	if err != nil {
		return err
	}
	if strings.TrimSpace(states) != "" {
		m.state.Running[app] = slices.Contains(strings.Fields(states), "running")
		m.save()
	}
	// An app without containers was removed, or never started: it gets
	// none now either. Running remembers across a crash that it had some.
	running, hadContainers := m.state.Running[app]
	releases := m.releaseFiles(app)
	if err := backupCompose(releases); err != nil {
		return err
	}
	if err := rewriteComposeFiles(releases, app); err != nil {
		return err
	}
	logf("moving %s to project %s", app, current)
	if strings.TrimSpace(states) != "" {
		if _, err := m.apps.output(ctx, "docker", "compose", "-p", old, "stop"); err != nil {
			return m.restore(ctx, app, running, fmt.Errorf("stop: %w", err))
		}
	}
	for _, volume := range volumes {
		if err := m.copyVolume(ctx, app, volume, logf); err != nil {
			return m.restore(ctx, app, running, err)
		}
	}
	number := m.apps.current(app)
	if number == 0 || !hadContainers {
		// Nothing to start again.
		_, _ = m.apps.output(ctx, "docker", "compose", "-p", old, "down", "--remove-orphans")
		return nil
	}
	if _, err := m.apps.output(ctx, "docker", "compose", "-p", old, "down", "--remove-orphans"); err != nil {
		return m.restore(ctx, app, running, fmt.Errorf("remove the old containers: %w", err))
	}
	composeFile := filepath.Join(m.apps.releaseDir(app, number), "compose.json")
	if !running {
		if err := m.apps.compose(ctx, app, composeFile, logf, "up", "--no-start"); err != nil {
			return m.restore(ctx, app, running, err)
		}
		return nil
	}
	var detected protocol.Detected
	body, _ := os.ReadFile(filepath.Join(m.apps.releaseDir(app, number), "detected.json"))
	_ = json.Unmarshal(body, &detected)
	if err := m.apps.start(ctx, app, composeFile, detected, false, logf); err != nil {
		return m.restore(ctx, app, running, err)
	}
	logf("%s runs under %s", app, current)
	return nil
}

// restore puts an app back on its old project after a failed move: its
// old volumes were only read, so it starts as it was.
func (m *rename) restore(ctx context.Context, app string, running bool, cause error) error {
	slog.Warn("move: putting the app back under its old name", "app", app, "error", cause)
	_, _ = m.apps.output(ctx, "docker", "compose", "-p", ourName+"-"+app, "down", "--remove-orphans")
	releases := m.releaseFiles(app)
	for _, file := range releases {
		if body, err := os.ReadFile(file + legacyComposeSuffix); err == nil {
			_ = os.WriteFile(file, body, 0o600)
		}
	}
	// The copies are stale once the old app runs again.
	kept := m.state.Volumes[:0]
	for _, volume := range m.state.Volumes {
		if strings.HasPrefix(volume, legacyProject(app)+"_") {
			_, _ = m.apps.output(ctx, "docker", "volume", "rm", newVolumeName(app, volume))
			continue
		}
		kept = append(kept, volume)
	}
	m.state.Volumes = kept
	m.save()
	if number := m.apps.current(app); number > 0 && running {
		composeFile := filepath.Join(m.apps.releaseDir(app, number), "compose.json")
		if _, err := m.apps.output(ctx, "docker", "compose", "-p", legacyProject(app), "-f", composeFile, "up", "-d", "--remove-orphans"); err != nil {
			return fmt.Errorf("%w; and starting it again under its old name failed: %v", cause, err)
		}
	}
	return cause
}

// oldVolumes are the named volumes of an app's old project that the
// project's name named: one the compose file names itself keeps its name.
func (m *rename) oldVolumes(ctx context.Context, app string) ([]string, error) {
	out, err := m.apps.output(ctx, "docker", "volume", "ls", "-q", "--filter", "label=com.docker.compose.project="+legacyProject(app))
	if err != nil {
		return nil, err
	}
	var volumes []string
	for _, volume := range strings.Fields(out) {
		if strings.HasPrefix(volume, legacyProject(app)+"_") {
			volumes = append(volumes, volume)
		}
	}
	return volumes, nil
}

func newVolumeName(app, old string) string {
	return ourName + "-" + app + "_" + strings.TrimPrefix(old, legacyProject(app)+"_")
}

// copyVolume copies an old volume into a new one with the labels Compose
// expects, and checks that every file arrived the same: content, name,
// owner and mode. A copy that does not match is removed.
func (m *rename) copyVolume(ctx context.Context, app, old string, logf func(string, ...any)) error {
	target := newVolumeName(app, old)
	if slices.Contains(m.state.Volumes, old) {
		if _, err := m.apps.output(ctx, "docker", "volume", "inspect", target); err == nil {
			return nil
		}
	}
	if _, err := m.apps.output(ctx, "docker", "image", "inspect", m.helper); err != nil {
		if _, err := m.apps.output(ctx, "docker", "pull", m.helper); err != nil {
			return fmt.Errorf("pull %s to copy the volumes: %w", m.helper, err)
		}
	}
	// A copy a crash left halfway starts again.
	_, _ = m.apps.output(ctx, "docker", "volume", "rm", target)
	key, _ := m.apps.output(ctx, "docker", "volume", "inspect", "-f", `{{index .Labels "com.docker.compose.volume"}}`, old)
	version, _ := m.apps.output(ctx, "docker", "volume", "inspect", "-f", `{{index .Labels "com.docker.compose.version"}}`, old)
	args := []string{"volume", "create", "--label", "com.docker.compose.project=" + ourName + "-" + app}
	if key := strings.TrimSpace(key); key != "" {
		args = append(args, "--label", "com.docker.compose.volume="+key)
	}
	if version := strings.TrimSpace(version); version != "" {
		args = append(args, "--label", "com.docker.compose.version="+version)
	}
	if _, err := m.apps.output(ctx, "docker", append(args, target)...); err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	logf("copying volume %s to %s", old, target)
	const script = `set -e
cp -a /from/. /to/
sum() { cd "$1" && { find . -exec stat -c '%n %u %g %a %F' {} + | sort; find . -type f -exec sha256sum {} + | sort; } | sha256sum | cut -d' ' -f1; }
echo "$(sum /from) $(sum /to)"`
	out, err := m.apps.output(ctx, "docker", "run", "--rm", "--network", "none", "--entrypoint", "sh",
		"-v", old+":/from:ro", "-v", target+":/to", m.helper, "-c", script)
	if err != nil {
		_, _ = m.apps.output(ctx, "docker", "volume", "rm", target)
		return fmt.Errorf("copy %s: %w", old, err)
	}
	sums := strings.Fields(lastLine(out))
	if len(sums) != 2 || sums[0] != sums[1] || len(sums[0]) != 64 {
		_, _ = m.apps.output(ctx, "docker", "volume", "rm", target)
		return fmt.Errorf("copy of %s does not match: %q", old, strings.TrimSpace(out))
	}
	logf("volume %s copied, checksum %s", target, sums[0][:16])
	m.state.Volumes = append(m.state.Volumes, old)
	m.save()
	return nil
}

// releaseFiles are the compose.json files of an app's releases.
func (m *rename) releaseFiles(app string) []string {
	entries, _ := os.ReadDir(filepath.Join(m.apps.appDir(app), "releases"))
	var files []string
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		file := filepath.Join(m.apps.appDir(app), "releases", entry.Name(), "compose.json")
		if _, err := os.Stat(file); err == nil {
			files = append(files, file)
		}
	}
	return files
}

// legacyComposeSuffix is the copy of a compose.json from before the move.
const legacyComposeSuffix = "." + legacy

// backupCompose keeps each compose.json as it was before the move, once.
func backupCompose(files []string) error {
	for _, file := range files {
		if _, err := os.Stat(file + legacyComposeSuffix); err == nil {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(file+legacyComposeSuffix, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func rewriteComposeFiles(files []string, app string) error {
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var config map[string]any
		if err := json.Unmarshal(body, &config); err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if !renameCompose(config, app) {
			continue
		}
		body, err = json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// renameCompose moves a resolved compose file from the old project name to
// the new one: the project, its default network and named volumes, the
// proxy network, and the router names in the Traefik labels. Images keep
// their tags; they are on the server under them. It reports a change.
func renameCompose(config map[string]any, app string) bool {
	old, current := legacyProject(app), ourName+"-"+app
	changed := false
	if config["name"] == old {
		config["name"] = current
		changed = true
	}
	rename := func(value string) string {
		switch {
		case value == old:
			return current
		case strings.HasPrefix(value, old+"_"), strings.HasPrefix(value, old+"-"):
			return current + value[len(old):]
		case value == legacyProxyNetwork:
			return proxyNetwork
		}
		return value
	}
	for _, section := range []string{"networks", "volumes"} {
		items, _ := config[section].(map[string]any)
		for key, item := range items {
			entry, _ := item.(map[string]any)
			if entry == nil {
				continue
			}
			if name, ok := entry["name"].(string); ok && rename(name) != name {
				entry["name"] = rename(name)
				changed = true
			}
			if key == legacyProxyNetwork {
				delete(items, key)
				items[proxyNetwork] = entry
				changed = true
			}
		}
	}
	services, _ := config["services"].(map[string]any)
	for _, item := range services {
		service, _ := item.(map[string]any)
		if service == nil {
			continue
		}
		if networks, ok := service["networks"].(map[string]any); ok {
			if entry, ok := networks[legacyProxyNetwork]; ok {
				delete(networks, legacyProxyNetwork)
				networks[proxyNetwork] = entry
				changed = true
			}
		}
		labels, _ := service["labels"].(map[string]any)
		for key, value := range labels {
			if !strings.HasPrefix(key, "traefik.") {
				continue
			}
			parts := strings.Split(key, ".")
			for index, part := range parts {
				parts[index] = rename(part)
			}
			newKey := strings.Join(parts, ".")
			if text, ok := value.(string); ok && (key == "traefik.docker.network" || strings.HasSuffix(key, ".middlewares") || strings.HasSuffix(key, ".service")) {
				words := strings.Split(text, ",")
				for index, word := range words {
					words[index] = rename(word)
				}
				value = strings.Join(words, ",")
			}
			if newKey != key || value != labels[key] {
				delete(labels, key)
				labels[newKey] = value
				changed = true
			}
		}
	}
	return changed
}

// rewritePaths replaces from with to in the agent's own JSON files under
// dir: the compose files and what the agent detected, never an app's
// source or data.
func rewritePaths(dir, from, to string) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && (entry.Name() == "src" || entry.Name() == "volumes") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		return rewriteFile(path, from, to)
	})
}

func rewriteFile(path, from, to string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !strings.Contains(string(body), from) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.ReplaceAll(string(body), from, to)), info.Mode().Perm())
}

// cleanup removes what the old name left once it stops working: the old
// volumes the move copied, the copies of the compose files, the links.
func (m *rename) cleanup(ctx context.Context) {
	if m.state.Cleaned || !m.state.Done {
		return
	}
	slog.Info("removing what the old name left", "until", build.LegacyUntil.Format(time.DateOnly))
	for _, volume := range m.state.Volumes {
		_, _ = m.apps.output(ctx, "docker", "volume", "rm", volume)
	}
	for _, app := range m.apps.names() {
		for _, file := range m.releaseFiles(app) {
			_ = os.Remove(file + legacyComposeSuffix)
		}
	}
	if target, err := os.Readlink(legacyBinary); err == nil && target == installedPath {
		_ = os.Remove(legacyBinary)
	}
	removeLegacyStateLink(m.dir)
	_, _ = m.apps.output(ctx, "docker", "network", "rm", legacyProxyNetwork)
	m.state.Cleaned = true
	m.save()
}
