package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

const (
	maxBinarySize  = 300 << 20
	maxSumsSize    = 64 << 10
	downloadLimit  = 5 * time.Minute
	restartPause   = 500 * time.Millisecond
	versionTimeout = 10 * time.Second
)

// releaseURL is where the releases live. The hub only names the version:
// the binary, its checksums and their signature come from here.
var releaseURL = "https://github.com/axadrn/" + build.Name + "/releases/download/"

// releaseKeys are the public Ed25519 keys whose signature a release must
// carry, base64: the key that signs every release and a spare kept offline.
// A daemon installs no binary that neither signed, so whoever holds the hub
// cannot hand it one. hub/content/docs/guides/security.md#updates
var releaseKeys = []string{
	"dkwXdu3Ecl8Rs4tfXxIrvipOfRa5R61tfWEdKISw3LA=", // RELEASE_SIGNING_KEY
	"K2CVHFrtBYK1IT7s++2/R/LZNxO6+jGPc68EZiUDKhQ=", // RELEASE_SIGNING_KEY_SPARE, offline
}

// updater swaps the daemon's binary for a signed release and restarts the
// daemon through systemd.
type updater struct {
	// url is the hub the daemon is registered with, for the command that
	// updates a daemon by hand.
	mu  sync.Mutex
	url string
	// executable is the binary to replace, systemd its unit when the daemon
	// runs under systemd, "" otherwise.
	executable string
	systemd    string
}

func newUpdater(url string) *updater {
	u := &updater{url: strings.TrimRight(url, "/")}
	if path, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			u.executable = resolved
		}
	}
	// systemd sets INVOCATION_ID for the processes of a unit.
	if os.Getenv("INVOCATION_ID") != "" {
		u.systemd = build.Name + "-daemon"
	}
	return u
}

// daemonUpdate installs the release the hub names (DaemonUpdate) and
// restarts, unless the server has rules or a deploy runs.
func (r *runner) daemonUpdate(ctx context.Context, message protocol.Message, rules *protocol.Rules) {
	var update protocol.DaemonUpdate
	if err := message.Decode(&update); err != nil {
		r.fail(message, err)
		return
	}
	// A new binary could allow what the rules stop, so with rules only
	// the owner updates it, on the server.
	if rules != nil {
		r.fail(message, fmt.Errorf("this server has rules in %s, so only its owner updates the daemon, on the server: %s", r.rulesFile, protocol.UpdateCommand(r.url(), build.Name, runtime.GOARCH)))
		return
	}
	if !r.beginUpdate() {
		r.fail(message, errors.New("a deploy or a restore is running: update the daemon when it is done"))
		return
	}
	slog.Info("update", "from", build.Version, "to", update.Version)
	if err := r.updater.install(ctx, update); err != nil {
		r.endUpdate()
		slog.Error("update failed", "error", err)
		r.fail(message, err)
		return
	}
	r.send(protocol.TypeOK, message.RequestID, nil)
	if err := r.updater.restart(); err != nil {
		// The new binary is in place, the old one keeps running: deploys
		// and restores go on, and the next start runs the new one.
		r.endUpdate()
	}
}

// install downloads the release the hub named, checks that a release key
// signed its checksums, that the binary matches them and runs as that
// version, and moves it over the running one in one rename. It never goes
// back to an older release. Nothing changes when a step fails.
func (u *updater) install(ctx context.Context, update protocol.DaemonUpdate) error {
	if u.systemd == "" {
		return fmt.Errorf("this daemon does not run under systemd, so it cannot restart itself: update it on the server: %s", protocol.UpdateCommand(u.baseURL(), build.Name, runtime.GOARCH))
	}
	if u.executable == "" {
		return errors.New("cannot find the daemon's own binary")
	}
	if !build.IsRelease(update.Version) {
		return fmt.Errorf("%q is not a release: a daemon only updates to one", update.Version)
	}
	if build.NewerVersion(build.Version, update.Version) {
		return fmt.Errorf("this daemon runs %s and never goes back to the older %s", build.Version, update.Version)
	}
	ctx, cancel := context.WithTimeout(ctx, downloadLimit)
	defer cancel()
	base := releaseURL + update.Version + "/"
	sums, err := downloadRelease(ctx, base+"SHA256SUMS", maxSumsSize)
	if err != nil {
		return err
	}
	signature, err := downloadRelease(ctx, base+"SHA256SUMS.sig", ed25519.SignatureSize)
	if err != nil {
		return err
	}
	if !signedByARelease(sums, signature) {
		return fmt.Errorf("the checksums of %s carry no valid release signature", update.Version)
	}
	asset := build.Name + "-" + runtime.GOOS + "-" + runtime.GOARCH
	want, err := checksum(sums, asset)
	if err != nil {
		return err
	}

	body, err := getRelease(ctx, base+asset)
	if err != nil {
		return err
	}
	defer body.Close()

	// The new binary goes next to the old one, so the rename is atomic.
	file, err := os.CreateTemp(filepath.Dir(u.executable), "."+build.Name+"-update-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(body, maxBinarySize+1))
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("download %s: %w", base+asset, err)
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxBinarySize {
		return errors.New("the new binary is too large")
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("the download does not match its signed SHA-256: got %s, want %s", got, want)
	}
	err = os.Chmod(temp, 0o755)
	if err != nil {
		return err
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, versionTimeout)
	defer versionCancel()
	output, err := exec.CommandContext(versionCtx, temp, "version").Output()
	if err != nil {
		return fmt.Errorf("the new binary does not run here: %w", err)
	}
	if got := strings.TrimSpace(string(output)); got != build.Name+" "+update.Version {
		return fmt.Errorf("the new binary says %q, not %s %s", got, build.Name, update.Version)
	}
	return os.Rename(temp, u.executable)
}

// getRelease starts the download of a file of a release.
func getRelease(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("download %s: %s", url, response.Status)
	}
	return response.Body, nil
}

// downloadRelease returns a small file of a release, at most limit bytes.
func downloadRelease(ctx context.Context, url string, limit int64) ([]byte, error) {
	body, err := getRelease(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return data, nil
}

// signedByARelease reports whether one of the release keys signed sums.
func signedByARelease(sums, signature []byte) bool {
	for _, encoded := range releaseKeys {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if err == nil && len(key) == ed25519.PublicKeySize && ed25519.Verify(key, sums, signature) {
			return true
		}
	}
	return false
}

// checksum returns the SHA-256 that sums, the output of sha256sum, lists
// for name.
func checksum(sums []byte, name string) (string, error) {
	for line := range strings.Lines(string(sums)) {
		sum, file, ok := strings.Cut(strings.TrimSpace(line), "  ")
		if ok && file == name && len(sum) == sha256.Size*2 {
			return strings.ToLower(sum), nil
		}
	}
	return "", fmt.Errorf("the release has no %s", name)
}

// setURL changes the hub the daemon is registered with.
func (u *updater) setURL(url string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.url = strings.TrimRight(url, "/")
}

func (u *updater) baseURL() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.url
}

// restart has systemd restart the daemon with the new binary. It does not
// wait: systemd stops this process.
func (u *updater) restart() error {
	time.Sleep(restartPause)
	slog.Info("restarting with the new binary", "unit", u.systemd)
	err := exec.Command("systemctl", "--no-block", "restart", u.systemd).Run()
	if err != nil {
		slog.Error("restart failed; restart the daemon by hand", "error", err)
	}
	return err
}
