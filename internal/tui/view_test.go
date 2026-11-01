package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/protocol"
	"regexp"
)

// frames are the screens worth checking: every place, prompt and panel.
func frames() map[string]model {
	now := time.Now()
	seen := now.Add(-3 * time.Minute)
	finished := now.Add(-time.Hour + 16*time.Second)
	servers := []client.Server{
		{ID: "1", Name: "dock-1", Online: true, OS: "linux", Arch: "arm64", AgentVersion: "dev", LastSeenAt: &now},
		{ID: "2", Name: "web-2", OS: "linux", Arch: "amd64", AgentVersion: "v3.2.1", LastSeenAt: &seen, Rules: &protocol.Rules{DataOff: true, DeployFrom: []string{"github.com/acme/*"}, NetworkLocked: true}},
		{ID: "3", Name: "tui-1"},
	}
	apps := []client.App{
		{Name: "hello", ServerName: "dock-1", LatestNumber: 42, LatestStatus: "succeeded", LiveNumber: 42},
		{Name: "blog", ServerName: "web-2", LatestNumber: 4, LatestStatus: "failed", LiveNumber: 3},
		{Name: "shop", ServerName: "dock-1", LatestNumber: 7, LatestStatus: "running", LiveNumber: 6},
		{Name: "notes", ServerName: "dock-1"},
	}
	detected := &protocol.Detected{Kind: "compose", Service: "web", Port: 8080, Databases: []protocol.Database{{Path: "/data/app.db", HostPath: "/var/lib/ops/apps/hello/volumes/data/app.db"}}}
	var deploys []client.Deploy
	for number := 43; number > 30; number-- {
		deploy := client.Deploy{Number: number, Status: "succeeded", Detected: detected, CreatedAt: now.Add(-time.Hour), FinishedAt: &finished}
		switch number {
		case 43:
			deploy.Status, deploy.FinishedAt = "running", nil
		case 40:
			deploy.Status, deploy.Error = "failed", "health check /index.html failed: 502 Bad Gateway"
		case 39:
			deploy.FromNumber = 37
		}
		deploys = append(deploys, deploy)
	}
	detail := &client.AppDetail{App: apps[0], Deploys: deploys}
	log := []string{"downloading the source", "detected compose, service web on port 8080", "building", " Image ops-hello-web:43 Building ", "#1 [internal] load local bake definitions", "#1 DONE 0.0s", "", "#2 ERROR: failed to read dockerfile", " Image ops-hello-web:43 Built ", "starting", " Container ops-hello-web-1 Started ", "health check http://172.18.0.2:8080/index.html"}

	base := model{url: "https://ops.goilerplate.com", dir: "/home/me/hello", servers: servers, apps: apps}
	out := map[string]model{}
	add := func(name string, change func(*model)) {
		m := base
		change(&m)
		out[name] = m
	}
	add("servers", func(m *model) {})
	add("servers-loading", func(m *model) { m.servers, m.loading = nil, true })
	add("servers-empty", func(m *model) { m.servers = nil })
	add("servers-add", func(m *model) { m.mode, m.input = modeAdd, "tui-1" })
	add("servers-added", func(m *model) {
		m.added = &client.NewServer{Server: client.Server{Name: "tui-1"}, InstallCommand: "curl -fsSL https://ops.goilerplate.com/install.sh | sudo sh -s -- ins_4k2j9x0c8v7b6n5m4l3k2j1h0g9f8d7s"}
	})
	add("servers-remove", func(m *model) { m.mode, m.selected = modeConfirmRemove, 2 })
	add("servers-offline", func(m *model) { m.offline = errors.New("eof") })
	add("apps", func(m *model) { m.tab = tabApps; m.notice = "v42 of hello is live" })
	add("activity", func(m *model) {
		m.tab = tabActivity
		now := time.Now()
		m.events = []client.Event{
			{Actor: "ada@example.com", Via: "api", Summary: "deployed hello v43 from main at 3f2a9c1", CreatedAt: now},
			{Actor: "GitHub", Via: "github", Summary: "deployed hello v42 from main at 9e1b7d0", CreatedAt: now.Add(-time.Hour)},
			{Actor: "grace@example.com", Via: "web", Summary: "ran UPDATE on orders in hello, 3 rows changed", CreatedAt: now.Add(-26 * time.Hour)},
		}
	})
	add("activity-empty", func(m *model) { m.tab = tabActivity })
	add("apps-empty", func(m *model) { m.tab, m.apps = tabApps, nil })
	add("detail", func(m *model) { m.tab, m.detail = tabApps, detail })
	add("detail-deploy", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "deploy v43 of hello", log
		m.running, m.runStarted, m.runStep, m.frame = true, now.Add(-8*time.Second), "health check http://172.18.0.2:8080/index.html", 2
	})
	add("detail-deployed", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "deploy v43 of hello", log
		m.outputNote, m.notice = "✓ live · 15s", "v43 of hello is live"
	})
	add("detail-failed", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "deploy v43 of hello", log
		m.err = errors.New("v43 of hello failed: health check /index.html failed: 502")
	})
	add("detail-logs", func(m *model) {
		var lines []string
		for index := range 60 {
			lines = append(lines, fmt.Sprintf("web-1  | 2026-10-31T10:24:%02d.407953672Z hello listening on 8080", index))
		}
		m.tab, m.detail, m.outputID, m.outputTitle, m.outputNote, m.output = tabApps, detail, 1, "logs of hello", "last 200 lines per container", lines
	})
	add("detail-env", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle, m.output = tabApps, detail, 1, "env of hello", []string{"GREETING=••••••", "TUI_KEY=••••••"}
		m.mode, m.input = modeEnv, "TUI_KEY=sec"
	})
	add("detail-rollback", func(m *model) { m.tab, m.detail, m.mode = tabApps, detail, modeConfirmRollback })
	add("detail-sql", func(m *model) {
		m.tab, m.detail, m.outputID, m.outputTitle = tabApps, detail, 1, "SELECT name, stock FROM products ORDER BY stock DESC LIMIT 2"
		m.output = resultLines([]string{"name", "stock"}, [][]any{{"Cable organizer", float64(61)}, {"Desk lamp", nil}})
		m.mode, m.input = modeSQL, "SELECT * FROM orders WHERE status = 'refunded' AND created_at > '2026-10-01' ORDER BY id DESC"
		m.err = errors.New("near \"FORM\": syntax error")
	})
	add("detail-backups", func(m *model) {
		at := now.Add(-time.Hour)
		list := client.AppBackups{SnapshotBeforeChanges: true, Databases: []protocol.DatabaseBackup{{
			Database: "/data/app.db", Since: &at, ReplicatedAt: &now, VerifiedAt: &at,
			Snapshots: []protocol.Snapshot{{ID: "20261031T143638Z-manual", Reason: "manual", CreatedAt: at}, {ID: "20261031T143526Z-before-v42", Reason: "before-v42", CreatedAt: at}},
		}}}
		output := backupsOutput(1, "hello", list)
		m.tab, m.detail, m.outputID, m.outputTitle, m.outputNote, m.output = tabApps, detail, 1, output.title, output.note, output.lines
		m.mode, m.input = modeBackups, "14:0"
	})
	add("detail-restore", func(m *model) {
		m.tab, m.detail, m.mode, m.pendingRestore = tabApps, detail, modeConfirmRestore, "20261031T143638Z-manual"
	})
	add("detail-sql-write", func(m *model) {
		m.tab, m.detail, m.mode = tabApps, detail, modeConfirmWrite
		m.pendingSQL = "UPDATE settings SET value = 'on' WHERE key = 'maintenance' AND value IS NOT NULL AND key != 'currency'"
	})
	return out
}

// TestViewFits renders every frame at small and large sizes: it never has
// more lines than the terminal, no line is wider, and the footer shows.
func TestViewFits(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 30}, {60, 12}, {200, 50}, {40, 6}}
	show := os.Getenv("TUI_FRAMES")
	for name, m := range frames() {
		for _, size := range sizes {
			m.width, m.height = size[0], size[1]
			content := m.View().Content
			lines := strings.Split(content, "\n")
			if len(lines) > m.height {
				t.Errorf("%s at %dx%d: %d lines", name, size[0], size[1], len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > m.width {
					t.Errorf("%s at %dx%d: line %q is %d wide", name, size[0], size[1], strip(line), lipgloss.Width(line))
				}
			}
			_, keys := m.prompt()
			if last := strip(lines[len(lines)-1]); size[1] >= 12 && !strings.Contains(content, keys[len(keys)-1][1]) {
				t.Errorf("%s at %dx%d: footer cut, last line %q", name, size[0], size[1], last)
			}
			if show == name || show == "all" {
				if os.Getenv("TUI_COLOR") == "" {
					content = strip(content)
				}
				fmt.Printf("── %s %dx%d\n%s\n", name, size[0], size[1], content)
			}
		}
	}
}

var escapes = regexp.MustCompile("\x1b\\[[0-9;:]*[A-Za-z]")

func strip(text string) string { return escapes.ReplaceAllString(text, "") }
