package service

import (
	"sort"

	"github.com/axadrn/moin/controlplane/internal/model"
)

// SortApps puts apps in the order a user sorts them, the one order every
// surface shows (the web, the TUI, the CLI, the API and MCP):
//
//   - smart: what needs you (a failed deploy, an offline server), then
//     what deploys, then pinned apps, then the rest by name.
//   - recent: the newest deploy first.
//   - server: by server name, then smart within each server.
//
// lastDeploy has the time of each app's newest deploy by app ID, for recent.
func SortApps(apps []model.App, how string, servers []model.Server, lastDeploy map[string]int64) []model.App {
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
	smart := func(a, b model.App) bool {
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		return a.Name < b.Name
	}
	sorted := append([]model.App(nil), apps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		switch how {
		case model.SortRecent:
			if lastDeploy[a.ID] != lastDeploy[b.ID] {
				return lastDeploy[a.ID] > lastDeploy[b.ID]
			}
			return a.Name < b.Name
		case model.SortServer:
			if a.ServerName != b.ServerName {
				return a.ServerName < b.ServerName
			}
		}
		return smart(a, b)
	})
	return sorted
}

// LastDeploys is the time of each app's newest deploy in a workspace, by
// app ID, as Unix seconds.
func (s *AppService) LastDeploys(workspaceID string) (map[string]int64, error) {
	return s.apps.LastDeploys(workspaceID)
}

// ListSorted is the apps of a workspace in the order a user sorts them.
func (s *AppService) ListSorted(workspaceID, how string) ([]model.App, error) {
	apps, err := s.List(workspaceID)
	if err != nil {
		return nil, err
	}
	servers, err := s.servers.List(workspaceID)
	if err != nil {
		return nil, err
	}
	last, err := s.LastDeploys(workspaceID)
	if err != nil {
		return nil, err
	}
	return SortApps(apps, how, servers, last), nil
}
