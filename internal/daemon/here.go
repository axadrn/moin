package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// The CLI on your laptop runs `moin here …` on the server over SSH, as
// root. The functions here do that work with the daemon's own deploy code.
// dir is the state dir (StateDir); the apps live in dir/apps.

// Deploy is one deploy the CLI asks for. Source is a gzipped tar of the
// app's folder; Image, when set, is a local image docker load just put on
// the server, which replaces the build of the main service. Catalog
// deploys an app of the catalog built into this binary, without source.
type Deploy struct {
	App     string
	Source  io.Reader
	Image   string
	Catalog string
	// NoCopy deploys without the copy of the databases just before it.
	NoCopy bool
}

// Result is what a deploy ends with: the release that runs and its
// addresses, for the line the CLI prints.
type Result struct {
	App     string `json:"app"`
	Release int    `json:"release"`
	// From is the release a rollback ran again, 0 for any other deploy.
	From      int      `json:"from,omitempty"`
	Addresses []string `json:"addresses"`
	Databases []string `json:"databases,omitempty"`
}

// DeployHere deploys d on this server and writes the deploy log to out.
func DeployHere(ctx context.Context, dir string, d Deploy, out io.Writer) (Result, error) {
	if !protocol.ValidAppName(d.App) {
		return Result{}, fmt.Errorf("%q is not a valid app name: lowercase letters, digits and dashes", d.App)
	}
	a := newApps(dir)
	start := a.newStart(d.App)
	start.NoCopy = d.NoCopy
	switch {
	case d.Catalog != "":
		if _, ok := catalog.Compose(d.Catalog); !ok {
			return Result{}, fmt.Errorf("the catalog has no %s: see them with `%s catalog`", d.Catalog, build.Name)
		}
		if err := a.catalogEnv(d.App, start.Domains); err != nil {
			return Result{}, err
		}
		start.Catalog = d.Catalog
	default:
		file, err := a.receiveSource(d.Source)
		if err != nil {
			return Result{}, err
		}
		defer os.Remove(file)
		start.SourceFile = file
	}
	if d.Image != "" {
		image, err := a.pinImage(ctx, d.App, d.Image)
		if err != nil {
			return Result{}, err
		}
		start.Image = image
	}
	return a.deployHere(ctx, start, out)
}

// ApplyHere starts the app's running release again with the env and the
// domains as they are now, without a build: a restart, or a start after
// a stop.
func ApplyHere(ctx context.Context, dir, app string, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	return a.deployHere(ctx, a.newStart(app), out)
}

// StopHere stops the app's containers. They stay stopped after a reboot,
// until a start, a restart or a deploy.
func StopHere(ctx context.Context, dir, app string) error {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return err
	}
	return a.stop(ctx, app)
}

// LogsHere writes the last tail lines of the app's containers to out and,
// with follow, what they write from then on, until ctx ends.
func LogsHere(ctx context.Context, dir, app string, tail int, follow bool, out io.Writer) error {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return err
	}
	args := []string{"compose", "-p", a.projectOf(app), "logs", "--no-color", "--timestamps", "--tail", strconv.Itoa(min(max(tail, 1), 10000))}
	if follow {
		args = append(args, "--follow")
	}
	return runTo(ctx, out, os.Stderr, nil, "docker", args...)
}

// ExecHere runs argv in a container of the app's live release, in service
// or the main service, with stdin and stdout passed through: with tty as
// on a terminal, for psql or a shell.
func ExecHere(ctx context.Context, dir, app, service string, tty bool, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return err
	}
	if len(argv) == 0 || argv[0] == "" {
		return errors.New("no command")
	}
	detected, _ := a.live(app)
	if service == "" {
		service = detected.Service
	}
	if !validService(service) {
		return fmt.Errorf("%q is not a service name", service)
	}
	args := a.composeArgs(app, a.composeJSON(app), "exec")
	if !tty {
		args = append(args, "-T")
	}
	args = append(append(args, service), argv...)
	return runTo(ctx, stdout, stderr, stdin, "docker", args...)
}

// newStart is a deploy of app with its domains. It gets its number when
// it has the app's lock (runDeploy).
func (a *apps) newStart(app string) protocol.DeployStart {
	return protocol.DeployStart{
		DeployID: newDeployID(),
		App:      app,
		Domains:  &protocol.Domains{Domains: a.domains(app), Instant: "sslip.io", InstantIP: firstIPv4()},
	}
}

// deployHere runs one deploy and says what runs after it.
func (a *apps) deployHere(ctx context.Context, start protocol.DeployStart, out io.Writer) (Result, error) {
	logf := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	detected, err := a.runDeploy(ctx, &start, logf)
	if err != nil {
		a.markFailed(start.App, start.Number)
		return Result{}, err
	}
	logf("v%d is live", start.Number)
	result := Result{App: start.App, Release: start.Number, From: start.FromNumber, Addresses: addresses(detected)}
	for _, database := range detected.Databases {
		if database.Postgres() {
			result.Databases = append(result.Databases, "Postgres")
		} else {
			result.Databases = append(result.Databases, "SQLite")
		}
	}
	slices.Sort(result.Databases)
	result.Databases = slices.Compact(result.Databases)
	return result, nil
}

// addresses are the app's https addresses: its domains, then its instant
// address.
func addresses(detected protocol.Detected) []string {
	list := []string{}
	for _, domain := range detected.Domains {
		list = append(list, "https://"+domain)
	}
	if detected.Instant != "" {
		list = append(list, "https://"+detected.Instant)
	}
	return list
}

// receiveSource writes source to a file the deploy unpacks.
func (a *apps) receiveSource(source io.Reader) (string, error) {
	if source == nil {
		return "", errors.New("no source")
	}
	if err := os.MkdirAll(a.tempDir(), 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(a.tempDir(), "source-*.tar.gz")
	if err != nil {
		return "", err
	}
	written, err := io.Copy(file, io.LimitReader(source, maxSourceSize+1))
	file.Close()
	if err == nil && written > maxSourceSize {
		err = fmt.Errorf("larger than %d MB", maxSourceSize>>20)
	}
	if err != nil {
		os.Remove(file.Name())
		return "", fmt.Errorf("receive the source: %w", err)
	}
	return file.Name(), nil
}

// pinImage tags a loaded image by its content, like moin-my-app:3f2a9c1e0b4d,
// so each release keeps its own image and a rollback finds it again.
func (a *apps) pinImage(ctx context.Context, app, image string) (string, error) {
	id, err := a.output(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", "--", image)
	if err != nil {
		return "", fmt.Errorf("image %s is not on the server: %w", image, err)
	}
	id = strings.TrimPrefix(strings.TrimSpace(id), "sha256:")
	if len(id) < 12 {
		return "", fmt.Errorf("image %s has no ID", image)
	}
	pinned := project(app) + ":" + id[:12]
	if _, err := a.output(ctx, "docker", "tag", "--", image, pinned); err != nil {
		return "", err
	}
	return pinned, nil
}

// catalogEnv gives a catalog app what its compose file needs and its env
// lacks: VERSION, latest unless set, and its own address in APP_URL and
// APP_DOMAIN, its first domain or else its instant address.
func (a *apps) catalogEnv(app string, domains *protocol.Domains) error {
	if err := os.MkdirAll(a.appDir(app), 0o700); err != nil {
		return err
	}
	host := protocol.InstantHost(app, domains.InstantIP, domains.Instant)
	if len(domains.Domains) > 0 {
		host = domains.Domains[0]
	}
	a.envMu.Lock()
	defer a.envMu.Unlock()
	env, err := a.env(app)
	if err != nil {
		return err
	}
	if _, ok := env[catalog.VersionVar]; !ok {
		env[catalog.VersionVar] = "latest"
	}
	if _, ok := env[catalog.URLVar]; !ok && host != "" {
		env[catalog.URLVar] = "https://" + host
	}
	if _, ok := env[catalog.DomainVar]; !ok && host != "" {
		env[catalog.DomainVar] = host
	}
	return writeEnv(a.envFile(app), env)
}

// runTo runs a program with its output going straight to stdout and
// stderr, for logs and exec that stream.
func runTo(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
	return command.Run()
}

// ErrNoApp is the error of a command for an app this server does not
// run. The CLI then looks for the app on your other servers.
var ErrNoApp = errors.New("no such app on this server")

// ValidServerName reports whether name can name a server, for its
// backups and its watchers: the first deploy checks it before it sets
// the server up.
func ValidServerName(name string) bool { return serverName.MatchString(name) }

// mustHave fails when app has no release on this server.
func (a *apps) mustHave(app string) error {
	if !protocol.ValidAppName(app) {
		return fmt.Errorf("%q is not a valid app name: lowercase letters, digits and dashes", app)
	}
	if a.current(app) == 0 {
		return fmt.Errorf("%w: %s", ErrNoApp, app)
	}
	return nil
}

// nextNumber is one more than the highest release of app on the server.
func (a *apps) nextNumber(app string) int {
	return slices.Max(append(a.releases(app), 0)) + 1
}

// releases are the numbers of the app's releases on the server, oldest
// first.
func (a *apps) releases(app string) []int {
	entries, _ := os.ReadDir(filepath.Join(a.appDir(app), "releases"))
	numbers := []int{}
	for _, entry := range entries {
		if n, err := strconv.Atoi(entry.Name()); err == nil && n > 0 {
			numbers = append(numbers, n)
		}
	}
	slices.Sort(numbers)
	return numbers
}

func newDeployID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// firstIPv4 is the server's public IPv4, "" when it has none.
func firstIPv4() string {
	for _, address := range publicAddresses() {
		if ip, err := netip.ParseAddr(address); err == nil && ip.Is4() {
			return address
		}
	}
	return ""
}
