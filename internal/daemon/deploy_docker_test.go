//go:build docker

package daemon

import (
	"context"
	"fmt"

	"github.com/axadrn/moin/protocol"
)

// deployTo runs one deploy and hands every line of its log and how it
// ended to send, for the tests to look at.
func deployTo(ctx context.Context, a *apps, start protocol.DeployStart, send func(any)) {
	logf := func(format string, args ...any) {
		send(protocol.DeployLog{DeployID: start.DeployID, Line: fmt.Sprintf(format, args...)})
	}
	done := protocol.DeployDone{DeployID: start.DeployID}
	detected, err := a.runDeploy(ctx, start, logf)
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
