package service

import (
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// TestStatusAllGood: a workspace with an online server and an app that
// runs needs nothing, and counts what runs.
func TestStatusAllGood(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	status := NewStatusService(apps, servers, nil)
	status.release = "dev"
	app, err := apps.Create(owner, "shop", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	finishDeploy(t, apps, app.ID, 1, model.DeploySucceeded)
	got, err := status.Status(owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.OK() || len(got.Issues) != 0 {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if got.Apps != 1 || got.AppsLive != 1 || got.Servers != 1 || got.ServersOnline != 1 {
		t.Errorf("counts = %+v", got)
	}
}

// TestStatusIssues: each kind of trouble shows once, the most urgent first,
// with where to fix it.
func TestStatusIssues(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	status := NewStatusService(apps, servers, nil)
	status.release = "v9.0.0"
	app, err := apps.Create(owner, "shop", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	finishDeploy(t, apps, app.ID, 1, model.DeploySucceeded)
	finishDeploy(t, apps, app.ID, 2, model.DeployFailed)
	now := time.Now().UTC()
	if err := apps.apps.SetBackupStatus(model.BackupStatus{AppID: app.ID, Database: "postgres-db", Error: "WAL-G stopped", CheckError: "", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := apps.apps.SetBackupStatus(model.BackupStatus{AppID: app.ID, Database: "data-app.db", Error: "restore failed", CheckError: "restore failed", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := servers.servers.SetDiskFull(server.ID, true, now); err != nil {
		t.Fatal(err)
	}
	if err := apps.apps.SaveDomainUptime(model.DomainUptime{AppID: app.ID, Domain: "shop.example.com", State: model.UptimeDown, Since: &now}); err != nil {
		t.Fatal(err)
	}
	got, err := status.Status(owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OK() {
		t.Fatal("status is OK with trouble")
	}
	kinds := map[string]StatusIssue{}
	var order []string
	for _, issue := range got.Issues {
		if _, seen := kinds[issue.Kind]; seen {
			t.Errorf("%s twice", issue.Kind)
		}
		kinds[issue.Kind] = issue
		order = append(order, issue.Kind)
	}
	for kind, path := range map[string]string{
		IssueDeployFailed: "/apps/" + app.Link(),
		IssueBackupFailed: "/apps/" + app.Link() + "/data?db=postgres-db",
		IssueCheckFailed:  "/apps/" + app.Link() + "/data?db=data-app.db",
		IssueDomainDown:   "/apps/" + app.Link() + "/uptime",
		IssueDiskFull:     "/servers/" + server.Link() + "/metrics",
		IssueDaemonBehind: "/servers",
	} {
		issue, ok := kinds[kind]
		if !ok {
			t.Errorf("no %s in %v", kind, order)
			continue
		}
		if issue.Path != path {
			t.Errorf("%s path = %q, want %q", kind, issue.Path, path)
		}
	}
	if kinds[IssueDeployFailed].Detail != "v2 failed, v1 still runs" {
		t.Errorf("deploy detail = %q", kinds[IssueDeployFailed].Detail)
	}
	if len(order) > 0 && order[0] != IssueDeployFailed {
		t.Errorf("order = %v", order)
	}
}

// finishDeploy stores deploy number of app with status.
func finishDeploy(t *testing.T, apps *AppService, appID string, number int, status string) {
	t.Helper()
	deploy := &model.Deploy{ID: appID + "-" + time.Now().Format("150405.000000000"), AppID: appID, Number: number, Status: model.DeployRunning}
	if err := apps.apps.CreateDeploy(deploy); err != nil {
		t.Fatal(err)
	}
	if err := apps.apps.FinishDeploy(deploy.ID, status, "", "", "", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

// TestStatusLateCopies: copies that stopped coming need you, in one
// short sentence, also when the daemon said nothing.
func TestStatusLateCopies(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	status := NewStatusService(apps, servers, nil)
	status.release = "dev"
	app, err := apps.Create(owner, "shop", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	finishDeploy(t, apps, app.ID, 1, model.DeploySucceeded)
	copied := time.Now().UTC().Add(-5 * time.Hour)
	if err := apps.apps.SetBackupStatus(model.BackupStatus{AppID: app.ID, Database: "postgres-db", CopiedAt: &copied, UpdatedAt: copied}); err != nil {
		t.Fatal(err)
	}
	got, err := status.Status(owner.WorkspaceID)
	if err != nil || len(got.Issues) != 1 || got.Issues[0].Detail != "backups of postgres-db stopped: the copies are late" {
		t.Fatalf("issues = %+v %v", got.Issues, err)
	}
}

// TestStatusStoppedNotChecked: a backup whose copies stopped is not
// checked, even when its newest copy restored today.
func TestStatusStoppedNotChecked(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	status := NewStatusService(apps, servers, nil)
	status.release = "dev"
	app, err := apps.Create(owner, "shop", server.ID)
	if err != nil {
		t.Fatal(err)
	}
	finishDeploy(t, apps, app.ID, 1, model.DeploySucceeded)
	now := time.Now().UTC()
	if err := apps.apps.SetBackupStatus(model.BackupStatus{AppID: app.ID, Database: "postgres-db", CopiedAt: &now, CheckedAt: &now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := apps.apps.SetBackupStatus(model.BackupStatus{AppID: app.ID, Database: "data-app.db", CopiedAt: &now, CheckedAt: &now, Error: "the server has no key", UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err := status.Status(owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Databases != 2 || got.BackupsChecked != 1 {
		t.Errorf("backups checked = %d of %d, want 1 of 2", got.BackupsChecked, got.Databases)
	}
}

// TestStatusRegistryLogin: an app whose image is on a registry the
// workspace has a login for, which its server does not keep, needs you
// before its next deploy is refused.
func TestStatusRegistryLogin(t *testing.T) {
	apps, servers, server, _ := newAppService(t)
	status := NewStatusService(apps, servers, nil)
	status.release = "dev"
	for name, image := range map[string]string{"shop": "ghcr.io/me/shop:v1", "blog": "nginx:1.27"} {
		app, err := apps.Create(owner, name, server.ID)
		if err != nil {
			t.Fatal(err)
		}
		deploy := &model.Deploy{ID: app.ID + "-1", AppID: app.ID, Number: 1, Status: model.DeployRunning, Image: image}
		if err := apps.apps.CreateDeploy(deploy); err != nil {
			t.Fatal(err)
		}
		if err := apps.apps.FinishDeploy(deploy.ID, model.DeploySucceeded, "", "", "", "", time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	if err := apps.apps.SetRegistry(model.Registry{WorkspaceID: owner.WorkspaceID, Host: "ghcr.io", Username: "me", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	got, err := status.Status(owner.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || got.Issues[0] != (StatusIssue{Kind: IssueRegistryMissing, Target: "shop", Detail: "deploys stopped: the server has no login for ghcr.io: enter it under Registries", Path: "/registries"}) {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if err := servers.servers.SetServerSecret(server.ID, protocol.RegistrySecret("ghcr.io"), false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, err = status.Status(owner.WorkspaceID)
	if err != nil || !got.OK() {
		t.Fatalf("with the login: issues = %+v, %v", got.Issues, err)
	}
}
