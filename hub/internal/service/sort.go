package service

import (
	"sort"

	"github.com/axadrn/moin/hub/internal/model"
)

// SortApps puts apps in the one order every surface shows (the web, the
// TUI, the CLI, the API and MCP): what needs you (a failed deploy, an app on
// an offline server), then what deploys, then pinned apps, then the rest by
// name.
func SortApps(apps []model.App, servers []model.Server) []model.App {
	online := map[string]bool{}
	for _, server := range servers {
		online[server.ID] = server.Online
	}
	rank := func(app model.App) int {
		switch {
		case app.LatestStatus == model.DeployFailed, len(servers) > 0 && !online[app.ServerID]:
			return 0
		case app.LatestStatus == model.DeployRunning:
			return 1
		case app.Pinned:
			return 2
		}
		return 3
	}
	sorted := append([]model.App(nil), apps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if rank(sorted[i]) != rank(sorted[j]) {
			return rank(sorted[i]) < rank(sorted[j])
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

// ListSorted is the apps of a workspace in that order.
func (s *AppService) ListSorted(workspaceID string) ([]model.App, error) {
	apps, err := s.List(workspaceID)
	if err != nil {
		return nil, err
	}
	servers, err := s.servers.List(workspaceID)
	if err != nil {
		return nil, err
	}
	return SortApps(apps, servers), nil
}

// LastDeploys is the time of each app's newest deploy in a workspace, by
// app ID, as Unix seconds: the overview lists the recent ones.
func (s *AppService) LastDeploys(workspaceID string) (map[string]int64, error) {
	return s.apps.LastDeploys(workspaceID)
}
