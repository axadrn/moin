package service

import (
	"context"
	"errors"
	"testing"
)

// TestBadNamesStopBeforeTheServer: a made-up snapshot ID or ref name is
// refused before ops looks anything up, sends it to GitHub or to an agent.
func TestBadNamesStopBeforeTheServer(t *testing.T) {
	s := &AppService{}
	ctx := context.Background()
	if err := s.Restore(ctx, Actor{}, "blog", "app.db", "../../other/20261031T140200Z", nil); !errors.Is(err, ErrSnapshotID) {
		t.Errorf("Restore: err = %v, want ErrSnapshotID", err)
	}
	if _, err := s.DeployRef(ctx, Actor{}, "blog", "--upload-pack=x"); !errors.Is(err, ErrRefName) {
		t.Errorf("DeployRef: err = %v, want ErrRefName", err)
	}
	if _, err := s.LinkRepo(ctx, Actor{}, "blog", "acme/blog", "main..evil", true); !errors.Is(err, ErrRefName) {
		t.Errorf("LinkRepo: err = %v, want ErrRefName", err)
	}
}
