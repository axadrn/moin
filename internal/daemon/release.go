package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// A release of moin is a binary, its checksums and their signature. The
// server installs no binary that a release key did not sign, wherever it
// came from. here_update.go downloads and installs with what is here.

const (
	maxBinarySize  = 300 << 20
	maxSumsSize    = 64 << 10
	downloadLimit  = 5 * time.Minute
	versionTimeout = 10 * time.Second
)

// releaseURL is where the releases live: the binary, its checksums and
// their signature come from here.
var releaseURL = "https://github.com/axadrn/" + build.Name + "/releases/download/"

// releaseKeys are the public Ed25519 keys whose signature a release must
// carry, base64: the key that signs every release and a spare kept offline.
var releaseKeys = []string{
	"dkwXdu3Ecl8Rs4tfXxIrvipOfRa5R61tfWEdKISw3LA=", // RELEASE_SIGNING_KEY
	"K2CVHFrtBYK1IT7s++2/R/LZNxO6+jGPc68EZiUDKhQ=", // RELEASE_SIGNING_KEY_SPARE, offline
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
