package service

import (
	"strconv"

	"github.com/axadrn/moin/hub/internal/model"
)

// NextServerName is the name a server added without one gets: web-1,
// web-2 and so on, the lowest number none of servers has.
func NextServerName(servers []model.Server) string {
	taken := map[string]bool{}
	for _, server := range servers {
		taken[server.Name] = true
	}
	for number := 1; ; number++ {
		name := "web-" + strconv.Itoa(number)
		if !taken[name] {
			return name
		}
	}
}

// SuggestName is the name a server added to the workspace now without one
// gets, for the web to fill in.
func (s *ServerService) SuggestName(workspaceID string) string {
	servers, _ := s.servers.ListByWorkspaceID(workspaceID)
	return NextServerName(servers)
}
