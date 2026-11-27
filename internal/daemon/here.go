package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"

	"github.com/axadrn/moin/protocol"
)

// DeployHere deploys app on this server from source, a gzipped tar of the
// app's folder, and writes the deploy log to out. The CLI on your laptop
// runs it over SSH: `ssh prod moin here deploy <app>`.
func DeployHere(ctx context.Context, dir, app string, source io.Reader, out io.Writer) error {
	if !protocol.ValidAppName(app) {
		return fmt.Errorf("%q is not a valid app name: lowercase letters, digits and dashes", app)
	}
	a := newApps(dir, nil)
	a.proxyMode = func() string { return "direct" }
	a.registryLogin = func(string) string { return "" }

	if err := os.MkdirAll(a.tempDir(), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(a.tempDir(), "source-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = io.Copy(file, io.LimitReader(source, maxSourceSize+1))
	file.Close()
	if err != nil {
		return fmt.Errorf("receive the source: %w", err)
	}

	start := protocol.DeployStart{
		DeployID:   newDeployID(),
		App:        app,
		Number:     a.nextNumber(app),
		SourceFile: file.Name(),
		Domains:    &protocol.Domains{Instant: "sslip.io", InstantIP: firstIPv4()},
	}
	logf := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	detected, err := a.runDeploy(ctx, start, nil, logf)
	if err != nil {
		a.markFailed(app, start.Number)
		return err
	}
	logf("v%d is live", start.Number)
	if detected.Instant != "" {
		logf("https://%s", detected.Instant)
	}
	return nil
}

// LogsHere writes the last lines of app's logs to out.
func LogsHere(ctx context.Context, dir, app string, tail int, out io.Writer) error {
	result, err := newApps(dir, nil).logs(ctx, protocol.LogsRequest{App: app, Tail: tail})
	if err != nil {
		return err
	}
	for _, line := range result.Lines {
		fmt.Fprintln(out, line)
	}
	return nil
}

// nextNumber is one more than the highest release of app on the server.
func (a *apps) nextNumber(app string) int {
	entries, _ := os.ReadDir(filepath.Join(a.appDir(app), "releases"))
	highest := 0
	for _, entry := range entries {
		if n, err := strconv.Atoi(entry.Name()); err == nil && n > highest {
			highest = n
		}
	}
	return highest + 1
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
