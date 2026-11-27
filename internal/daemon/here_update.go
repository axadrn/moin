package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/axadrn/moin/internal/build"
)

// FetchRelease downloads the moin binary of a release for goos and goarch
// and checks it the way every update does: a release key signed the
// checksums, and the binary matches them. It never returns a binary that
// fails a check.
func FetchRelease(ctx context.Context, version, goos, goarch string) ([]byte, error) {
	if !build.IsRelease(version) {
		return nil, fmt.Errorf("%q is not a release, like v1.2.3", version)
	}
	ctx, cancel := context.WithTimeout(ctx, downloadLimit)
	defer cancel()
	base := releaseURL + version + "/"
	sums, err := downloadRelease(ctx, base+"SHA256SUMS", maxSumsSize)
	if err != nil {
		return nil, err
	}
	signature, err := downloadRelease(ctx, base+"SHA256SUMS.sig", ed25519.SignatureSize)
	if err != nil {
		return nil, err
	}
	if !signedByARelease(sums, signature) {
		return nil, fmt.Errorf("the checksums of %s carry no valid release signature", version)
	}
	asset := build.Name + "-" + goos + "-" + goarch
	want, err := checksum(sums, asset)
	if err != nil {
		return nil, err
	}
	body, err := downloadRelease(ctx, base+asset, maxBinarySize)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("the download of %s does not match its signed SHA-256", asset)
	}
	return body, nil
}

// InstallRelease puts the signed release version in place of the binary
// at path, after it ran as that version. One rename, so nothing changes
// when a step fails.
func InstallRelease(ctx context.Context, version, path string) error {
	body, err := FetchRelease(ctx, version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	return InstallBinary(ctx, body, version, path)
}

// InstallBinary writes body next to path, checks that it runs and says
// it is version when version is a release, then moves it over path.
func InstallBinary(ctx context.Context, body []byte, version, path string) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+build.Name+"-update-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	_, err = file.Write(body)
	if err == nil {
		// On disk before the rename, so a power cut never leaves an empty binary.
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temp, 0o755)
	}
	if err != nil {
		return err
	}
	if err := checkVersion(ctx, temp, version); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// checkVersion runs the binary at path and checks the first line it
// prints, like "moin v1.2.3". A build that is not a release only has to run.
func checkVersion(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("the new binary does not run here: %w", err)
	}
	got, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	if build.IsRelease(version) && got != build.Name+" "+version {
		return fmt.Errorf("the new binary says %q, not %s %s", got, build.Name, version)
	}
	return nil
}

// UpdateHere puts the signed release version in place of this server's
// moin, then restarts the daemon, when one runs.
func UpdateHere(ctx context.Context, version string) error {
	path, err := ownPath()
	if err != nil {
		return err
	}
	if err := InstallRelease(ctx, version, path); err != nil {
		return err
	}
	// try-restart leaves a daemon alone that does not run; a server
	// without one has nothing to restart.
	_ = exec.CommandContext(ctx, "systemctl", "try-restart", build.Name+"-daemon").Run()
	return nil
}

// ownPath is the file of the running binary, links resolved.
func ownPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}
