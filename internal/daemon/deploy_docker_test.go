//go:build docker

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// deployLog is one line of a deploy's log, for the tests.
type deployLog struct {
	DeployID string
	Line     string
}

// deployDone is how a deploy ended, for the tests.
type deployDone struct {
	DeployID    string
	OK          bool
	Error       string
	Detected    protocol.Detected
	ComposeFile string
}

// deployTo runs one deploy and hands every line of its log and how it
// ended to send, for the tests to look at.
func deployTo(ctx context.Context, a *apps, start protocol.DeployStart, send func(any)) {
	logf := func(format string, args ...any) {
		send(deployLog{DeployID: start.DeployID, Line: fmt.Sprintf(format, args...)})
	}
	done := deployDone{DeployID: start.DeployID}
	detected, err := a.runDeploy(ctx, &start, logf)
	done.Detected = detected
	if err != nil {
		done.Error = err.Error()
		logf("deploy failed: %s", err)
		a.markFailed(start.App, start.Number)
	} else {
		done.OK = true
		var fileErr error
		done.ComposeFile, fileErr = a.composeFile(start.App, start.Number)
		if fileErr != nil {
			logf("the compose file of v%d could not be made: %s", start.Number, fileErr)
		}
	}
	send(done)
}

// sourceFile writes files as the gzipped tar the CLI sends over SSH, and
// names it for a deploy.
func sourceFile(t *testing.T, files map[string]string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "source.tar.gz")
	if err := os.WriteFile(file, tarball(t, files), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}
