package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
)

func TestAdminDashboardRendersOperatorDataAndProtectedActions(t *testing.T) {
	now := time.Now()
	operatorID := "operator"
	workspaceID := "workspace-1"
	data := &model.AdminDashboard{
		Metrics: model.AdminMetrics{Users: 2, VerifiedUsers: 1, Workspaces: 2, PaidWorkspaces: 1, ActiveSessions: 3},
		Users: model.AdminUserPage{
			Users: []model.AdminUser{
				{ID: operatorID, Email: "operator@example.com", Name: "Operator", IsAdmin: true, CreatedAt: now},
				{ID: "target", Email: "target@example.com", Name: "Target", WorkspaceCount: 2, ActiveSessionCount: 1, CreatedAt: now},
			},
			Page:       1,
			TotalPages: 1,
			TotalUsers: 2,
		},
		Events: []model.AdminAuditEvent{{
			ID:          "event-1",
			ActorEmail:  "operator@example.com",
			WorkspaceID: &workspaceID,
			Action:      "workspace.deleted",
			TargetType:  "workspace",
			TargetID:    workspaceID,
			CreatedAt:   now,
		}},
	}
	ctx := context.Background()
	ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{})
	ctx = ctxkeys.WithURLPath(ctx, "/admin")
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	ctx = ctxkeys.WithUser(ctx, &model.User{ID: operatorID, Email: "operator@example.com"})

	var output bytes.Buffer
	if err := AdminDashboard(data).Render(ctx, &output); err != nil {
		t.Fatalf("render AdminDashboard(): %v", err)
	}
	html := output.String()
	for _, expected := range []string{"noindex, nofollow", ">Admin</span>", "target@example.com", "View as user", "Workspace deleted", "180 days"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("admin dashboard missing %q", expected)
		}
	}
	if strings.Contains(html, "/admin/users/operator/impersonate") {
		t.Fatal("admin dashboard allows self impersonation")
	}
	if !strings.Contains(html, "/admin/users/target/impersonate") || !strings.Contains(html, "csrf-token") {
		t.Fatal("admin dashboard omitted protected target action")
	}
}

func TestAdminFreeForeverAsksForTheEmail(t *testing.T) {
	trialEnds := time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC)
	data := &model.AdminDashboard{Users: model.AdminUserPage{Page: 1, TotalPages: 1, Users: []model.AdminUser{
		{ID: "trial", Email: "trial@example.com", Plan: model.AdminPlanTrial, TrialEndsAt: &trialEnds},
		{ID: "free", Email: "free@example.com", Plan: model.AdminPlanFreeForever},
		{ID: "payer", Email: "payer@example.com", Plan: model.AdminPlanPaying},
	}}}
	ctx := ctxkeys.WithCSRFToken(context.Background(), "csrf-token")
	ctx = ctxkeys.WithUser(ctx, &model.User{ID: "operator", Email: "operator@example.com"})
	var page bytes.Buffer
	if err := AdminDashboardContent(data).Render(ctx, &page); err != nil {
		t.Fatal(err)
	}
	html := page.String()
	for _, want := range []string{
		"trial until 2026-12-02", "paying",
		`data-confirm-name="trial@example.com"`, "Make free forever",
		`data-confirm-name="free@example.com"`, "End free forever",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page does not show %q", want)
		}
	}
	if strings.Contains(html, `data-confirm-name="payer@example.com"`) {
		t.Error("a paying user can be made free forever")
	}
}
