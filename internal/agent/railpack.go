package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// Railpack builds a repo that brings no compose.yml, Dockerfile or Go
// module: it reads the repo, finds the language and its start command, and
// writes a build plan that Docker's own BuildKit builds through the Railpack
// frontend. https://railpack.com
const (
	railpackVersion  = "0.43.0"
	railpackFrontend = "ghcr.io/railwayapp/railpack-frontend:v" + railpackVersion
	// railpackPlan is the plan in the release folder, next to src, so it
	// never lands in the image.
	railpackPlan = "railpack-plan.json"
	railpackInfo = "railpack-info.json"
	// railpackPort is the port a Railpack app listens on: moin sets PORT to it,
	// as Railway does, since Railpack sets neither PORT nor EXPOSE.
	railpackPort = 8080
)

// railpackSums are the sha256 of the release archives, from checksums.txt of
// the GitHub release.
var railpackSums = map[string]string{
	"arm64": "732ece14eb4f4676a43ebba19431f954edf7b130acb76159ef0f2ff86ac42ac9",
	"amd64": "248a8f60f36854aabed6124113631078a802de3461660855fc4de9c26065c1fb",
}

// railpackArch is the architecture in the release's asset names.
var railpackArch = map[string]string{"arm64": "arm64", "amd64": "x86_64"}

// railpackBinary returns the railpack CLI, downloaded once into the agent's
// bin folder and checked against its sha256. MOIN_RAILPACK_BINARY names one
// already on disk, as the tests do.
func (a *apps) railpackBinary(ctx context.Context) (string, error) {
	if path := build.Getenv("RAILPACK_BINARY"); path != "" {
		return path, nil
	}
	sum, ok := railpackSums[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("Railpack has no build for %s: add a Dockerfile to the repo", runtime.GOARCH)
	}
	dir := filepath.Join(filepath.Dir(a.dir), "bin")
	path := filepath.Join(dir, "railpack-"+railpackVersion)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	url := fmt.Sprintf("https://github.com/railwayapp/railpack/releases/download/v%s/railpack-v%s-%s-unknown-linux-musl.tar.gz", railpackVersion, railpackVersion, railpackArch[runtime.GOARCH])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download Railpack: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download Railpack: %s", response.Status)
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, 200<<20))
	if err != nil {
		return "", fmt.Errorf("download Railpack: %w", err)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != sum {
		return "", errors.New("the Railpack download does not match its checksum: not using it")
	}
	binary, err := untarFile(archive, "railpack")
	if err != nil {
		return "", fmt.Errorf("unpack Railpack: %w", err)
	}
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return "", err
	}
	temp := path + ".new"
	err = os.WriteFile(temp, binary, 0o755)
	if err != nil {
		return "", err
	}
	return path, os.Rename(temp, path)
}

// untarFile returns the file name from a gzipped tar archive.
func untarFile(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err != nil {
			return nil, fmt.Errorf("%s is not in the archive", name)
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == name {
			return io.ReadAll(io.LimitReader(reader, 200<<20))
		}
	}
}

// railpackCompose is the compose.yml moin writes for a Railpack app: one
// service built from the plan through the Railpack frontend, listening on
// PORT, with its data in ./data. The cache key keeps each app's build caches
// apart.
func railpackCompose(app string) string {
	return fmt.Sprintf(`services:
  web:
    build:
      context: .
      dockerfile: ../%s
      args:
        BUILDKIT_SYNTAX: %s
        cache-key: %s
    environment:
      PORT: "%d"
      DATA_DIR: /data
    volumes:
      - ./data:/data
`, railpackPlan, railpackFrontend, project(app), railpackPort)
}

// envNamePattern is a name a shell and a BuildKit secret both take.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// railpackPrepareArgs are the arguments of railpack prepare. Railpack learns
// the app's env names, so the build gets them as secrets, but never their
// values: those go to the build as secrets of BuildKit. Its own settings,
// RAILPACK_*, are build config and go with their values: RAILPACK_START_CMD
// sets the start command.
func railpackPrepareArgs(src, release string, env map[string]string) []string {
	args := []string{"prepare", src, "--plan-out", filepath.Join(release, railpackPlan), "--info-out", filepath.Join(release, railpackInfo)}
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !envNamePattern.MatchString(name) {
			continue
		}
		if strings.HasPrefix(name, "RAILPACK_") {
			args = append(args, "--env", name+"="+env[name])
			continue
		}
		// A value is needed for Railpack to count the name; any will do.
		args = append(args, "--env", name+"=set")
	}
	return args
}

// railpackInfoFile is what railpack prepare reports about the repo.
type railpackInfoFile struct {
	Success           bool     `json:"success"`
	DetectedProviders []string `json:"detectedProviders"`
	ResolvedPackages  map[string]struct {
		Name            string `json:"name"`
		ResolvedVersion string `json:"resolvedVersion"`
	} `json:"resolvedPackages"`
	Logs []struct {
		Level string `json:"Level"`
		Msg   string `json:"Msg"`
	} `json:"logs"`
}

// railpackPlanFile is the part of the plan moin reads.
type railpackPlanFile struct {
	Deploy struct {
		StartCommand string `json:"startCommand"`
	} `json:"deploy"`
}

// providerNames are the names of Railpack's providers as people write them.
var providerNames = map[string]string{
	"node": "Node", "python": "Python", "ruby": "Ruby", "php": "PHP", "golang": "Go",
	"java": "Java", "rust": "Rust", "elixir": "Elixir", "deno": "Deno", "dotnet": ".NET",
	"staticfile": "Static site", "gleam": "Gleam", "cpp": "C++", "shell": "Shell", "zig": "Zig",
}

// railpackSummary says what Railpack found, like "Node 24.22.1, start: npm
// run start".
func railpackSummary(info railpackInfoFile, plan railpackPlanFile) string {
	parts := []string{}
	for _, provider := range info.DetectedProviders {
		name := providerNames[provider]
		if name == "" {
			name = provider
		}
		if pkg, ok := info.ResolvedPackages[provider]; ok && pkg.ResolvedVersion != "" {
			name += " " + pkg.ResolvedVersion
		} else if provider == "node" {
			if pkg, ok := info.ResolvedPackages["bun"]; ok && pkg.ResolvedVersion != "" {
				name = "Bun " + pkg.ResolvedVersion
			}
		}
		parts = append(parts, name)
	}
	summary := strings.Join(parts, ", ")
	if summary == "" {
		summary = "unknown language"
	}
	if plan.Deploy.StartCommand != "" {
		summary += ", start: " + plan.Deploy.StartCommand
	}
	return summary
}

// railpackErrors are the error lines of a failed prepare.
func railpackErrors(info railpackInfoFile) string {
	lines := []string{}
	for _, log := range info.Logs {
		if strings.EqualFold(log.Level, "error") && log.Msg != "" {
			lines = append(lines, log.Msg)
		}
	}
	return strings.Join(lines, "; ")
}

// railpackPrepare writes the build plan of src into release and says what
// it found. Exit code 75 is a passing failure, so it tries once more.
func (a *apps) railpackPrepare(ctx context.Context, src, release string, env map[string]string) (string, error) {
	binary, err := a.railpackBinary(ctx)
	if err != nil {
		return "", err
	}
	args := railpackPrepareArgs(src, release, env)
	var output []byte
	for attempt := 0; attempt < 2; attempt++ {
		command := exec.CommandContext(ctx, binary, args...)
		// Railpack's own colors and boxes stay out of the deploy log.
		command.Env = append(os.Environ(), "NO_COLOR=1")
		output, err = command.CombinedOutput()
		var exit *exec.ExitError
		if err == nil || !errors.As(err, &exit) || exit.ExitCode() != 75 {
			break
		}
	}
	var info railpackInfoFile
	body, _ := os.ReadFile(filepath.Join(release, railpackInfo))
	_ = json.Unmarshal(body, &info)
	if err != nil || !info.Success {
		reason := railpackErrors(info)
		if reason == "" {
			reason = lastLine(string(output))
		}
		if reason == "" && err != nil {
			reason = err.Error()
		}
		return "", fmt.Errorf("Railpack cannot build this repo: %s. Add a Dockerfile, or a compose.yml, to tell moin how", strings.TrimSuffix(reason, "."))
	}
	var plan railpackPlanFile
	body, err = os.ReadFile(filepath.Join(release, railpackPlan))
	if err != nil {
		return "", fmt.Errorf("Railpack wrote no build plan: %w", err)
	}
	_ = json.Unmarshal(body, &plan)
	return railpackSummary(info, plan), nil
}

// railpackSecrets hands the app's env to the Railpack build of service as
// BuildKit secrets: names in the compose file, values only in the
// environment of docker compose build. secrets-hash rebuilds the steps that
// use them when a value changes.
func railpackSecrets(config, service map[string]any, env map[string]string) {
	build, _ := service["build"].(map[string]any)
	if build == nil || len(env) == 0 {
		return
	}
	names := make([]string, 0, len(env))
	for name := range env {
		if envNamePattern.MatchString(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	secrets, _ := config["secrets"].(map[string]any)
	if secrets == nil {
		secrets = map[string]any{}
	}
	hash := sha256.New()
	list := make([]any, 0, len(names))
	for _, name := range names {
		secrets[name] = map[string]any{"environment": name}
		list = append(list, name)
		fmt.Fprintf(hash, "%s=%s\n", name, env[name])
	}
	config["secrets"] = secrets
	build["secrets"] = list
	args, _ := build["args"].(map[string]any)
	if args == nil {
		args = map[string]any{}
	}
	args["secrets-hash"] = hex.EncodeToString(hash.Sum(nil))
	build["args"] = args
}

// buildEnv is the environment of docker compose build: the app's env, for
// the secrets a Railpack build reads.
func buildEnv(env map[string]string) []string {
	environment := make([]string, 0, len(env))
	for name, value := range env {
		if envNamePattern.MatchString(name) {
			environment = append(environment, name+"="+value)
		}
	}
	slices.Sort(environment)
	return environment
}
