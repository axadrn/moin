package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// moin never updates itself or a server on its own. Once a day it asks
// GitHub for the newest release and says so loudly when there is one.

// releasesURL is the page of the newest release; GitHub redirects it to
// the release's own page, whose last part is the version.
var releasesURL = "https://github.com/axadrn/" + build.Name + "/releases/latest"

// checkEvery is how often moin asks for a newer release.
const checkEvery = 24 * time.Hour

// updateCheck is what the last check found, kept in ~/.config/moin.
type updateCheck struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

// version is `moin version`. The first line is the version alone, which
// a server's update checks.
func version(args []string, stdout io.Writer) error {
	if err := noArguments("version", args); err != nil {
		return err
	}
	fmt.Fprintln(stdout, build.Name, build.Version)
	return nil
}

// warnNewer prints a warning on stderr when a newer release is out. It
// asks GitHub at most once a day and never holds a command up for long.
func warnNewer(ctx context.Context, stderr io.Writer) {
	if !build.IsRelease(build.Version) {
		return
	}
	latest := latestRelease(ctx, time.Now())
	if !build.NewerVersion(latest, build.Version) {
		return
	}
	fmt.Fprintln(stderr, style.Warning.Bold(true).Render(fmt.Sprintf("%s %s is out, you run %s.", build.Name, latest, build.Version)))
	fmt.Fprintln(stderr, "  "+style.Muted.Render("What is new: "+releasePage(latest)))
	hint(stderr, "Update this CLI with", build.Name+" update")
	fmt.Fprintln(stderr)
}

// releasePage is the release notes of version on GitHub.
func releasePage(version string) string {
	return "https://github.com/axadrn/" + build.Name + "/releases/tag/" + version
}

// latestRelease is the newest release, from the check of today or a new
// one. "" when it cannot tell.
func latestRelease(ctx context.Context, now time.Time) string {
	file, err := checkFile()
	if err != nil {
		return ""
	}
	last := readCheck(file)
	if !checkDue(last, now) {
		return last.Latest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	latest, err := askGitHub(ctx)
	if err != nil {
		// Asked again tomorrow, not on every command while offline.
		latest = last.Latest
	}
	writeCheck(file, updateCheck{Checked: now, Latest: latest})
	return latest
}

// checkDue reports whether the last check is a day old or more.
func checkDue(last updateCheck, now time.Time) bool {
	return now.Sub(last.Checked) >= checkEvery || last.Checked.After(now)
}

// checkFile is where the last check is kept.
func checkFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-check.json"), nil
}

// readCheck reads the last check, an empty one when there is none.
func readCheck(file string) updateCheck {
	var check updateCheck
	body, err := os.ReadFile(file)
	if err == nil {
		_ = json.Unmarshal(body, &check)
	}
	return check
}

// writeCheck keeps a check for the next day.
func writeCheck(file string, check updateCheck) {
	body, _ := json.Marshal(check)
	if os.MkdirAll(filepath.Dir(file), 0o700) == nil {
		_ = os.WriteFile(file, body, 0o600)
	}
}

// askGitHub reads the newest release from where GitHub redirects its
// latest release page to, like …/releases/tag/v1.2.3.
func askGitHub(ctx context.Context) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, releasesURL, nil)
	if err != nil {
		return "", err
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	response.Body.Close()
	_, latest, ok := strings.Cut(response.Header.Get("Location"), "/releases/tag/")
	if !ok || !build.IsRelease(latest) {
		return "", errors.New("GitHub named no release")
	}
	return latest, nil
}

// update is `moin update`, this CLI to the newest release, and `moin
// update <server>`, the server to this CLI's version.
func update(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("update", "update, or update <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	switch flags.NArg() {
	case 0:
		return updateSelf(ctx, stdout)
	case 1:
		return updateServer(ctx, flags.Arg(0), stdout)
	}
	return flags.wrong()
}

// updateSelf puts the newest signed release in place of this binary.
func updateSelf(ctx context.Context, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	latest, err := askGitHub(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("cannot tell the newest release: %w. See %s", err, releasesURL)
	}
	if !build.NewerVersion(latest, build.Version) {
		done(stdout, "%s %s is the newest release", build.Name, build.Version)
		return nil
	}
	path, err := os.Executable()
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	if err != nil {
		return err
	}
	step(stdout, "downloading %s %s", build.Name, latest)
	if err := daemon.InstallRelease(ctx, latest, path); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("cannot write %s: run `sudo %s update`", path, build.Name)
		}
		return err
	}
	done(stdout, "%s %s is installed. What is new: %s", build.Name, latest, releasePage(latest))
	return nil
}

// updateServer puts this CLI's version of moin on a server. A release is
// downloaded by the server itself and checked there; a build of your own
// is copied over.
func updateServer(ctx context.Context, server string, stdout io.Writer) error {
	info, err := probe(ctx, server)
	if err != nil {
		return err
	}
	if info.version == "" {
		return fmt.Errorf("%s is not on %s yet. Set it up with `%s deploy %s`", build.Name, server, build.Name, server)
	}
	if build.IsRelease(build.Version) && info.version == build.Version {
		done(stdout, "%s runs %s %s already", server, build.Name, build.Version)
		return nil
	}
	if build.NewerVersion(info.version, build.Version) {
		return fmt.Errorf("%s runs %s, newer than this CLI. Update the CLI first with `%s update`", server, info.version, build.Name)
	}
	step(stdout, "putting %s %s on %s, it runs %s now", build.Name, build.Version, server, info.version)
	if build.IsRelease(build.Version) {
		err = sshRun(ctx, server, hereLine("update", build.Version), nil, io.Discard, os.Stderr)
	} else {
		err = installMoin(ctx, server, info.arch)
	}
	if err != nil {
		return err
	}
	done(stdout, "%s runs %s %s", server, build.Name, build.Version)
	return nil
}
