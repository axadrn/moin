package model

import "testing"

func TestSummaryOfLink(t *testing.T) {
	for metadata, want := range map[string]string{
		`{"repo":"axadrn/blog","branch":"main","auto_deploy":"true"}`:  "linked blog to axadrn/blog on main, every push deploys",
		`{"repo":"axadrn/blog","branch":"main","auto_deploy":"false"}`: "linked blog to axadrn/blog on main, deploys only by hand",
		`{"repo":"axadrn/blog","branch":"main"}`:                       "linked blog to axadrn/blog on main",
	} {
		event := WorkspaceAuditEvent{Action: "repo.linked", TargetType: "app", TargetID: "blog", Metadata: metadata}
		if got := event.Summary(); got != want {
			t.Errorf("Summary() = %q, want %q", got, want)
		}
	}
}
