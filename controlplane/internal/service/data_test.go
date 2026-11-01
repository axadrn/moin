package service

import (
	"errors"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/model"
)

func TestReadOnlyError(t *testing.T) {
	readOnly := &AgentError{Message: "attempt to write a readonly database (8)"}
	if err := readOnlyError(readOnly); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("readonly answer = %v, want ErrReadOnly", err)
	}
	other := &AgentError{Message: "no such table: orders"}
	if err := readOnlyError(other); err != other {
		t.Fatalf("other answer = %v, want it unchanged", err)
	}
	if err := readOnlyError(nil); err != nil {
		t.Fatalf("nil = %v", err)
	}
}

func TestKeptDeploysFollowsTheAgentRule(t *testing.T) {
	// Newest first: v8 restart (live), v7 to v3 builds, v2 restart, v1 build,
	// and v9 a failed redeploy.
	deploys := []model.Deploy{{Number: 9, Status: model.DeployFailed, SourceKey: "server"}, {Number: 8, Status: model.DeploySucceeded}}
	for number := 7; number >= 3; number-- {
		deploys = append(deploys, model.Deploy{Number: number, Status: model.DeploySucceeded, SourceKey: "server"})
	}
	deploys = append(deploys, model.Deploy{Number: 2, Status: model.DeploySucceeded}, model.Deploy{Number: 1, Status: model.DeploySucceeded, Image: "ghcr.io/a/b:1"})
	kept := KeptDeploys(deploys, 8)
	for number, want := range map[int]bool{9: false, 8: true, 7: true, 3: true, 2: false, 1: false} {
		if kept[number] != want {
			t.Errorf("v%d kept = %v, want %v", number, kept[number], want)
		}
	}
}
