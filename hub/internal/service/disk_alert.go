package service

import (
	"fmt"
	"log/slog"

	"github.com/axadrn/moin/internal/human"
)

// A server's disk alerts once it is diskFullAt percent full, and says so
// again only after it went under diskOKAt: two lines, so a disk that sits
// at 90% does not alert every few seconds.
const (
	diskFullAt = 90.0
	diskOKAt   = 85.0
)

// checkDisk compares a server's disk with the alert lines and records
// disk.full or disk.ok when it crosses one; the activity sends the alert.
func (s *ServerService) checkDisk(serverID string, used, total uint64) {
	if total == 0 {
		return
	}
	percent := float64(used) / float64(total) * 100
	s.mu.Lock()
	full, known := s.diskFull[serverID]
	s.mu.Unlock()
	if !known {
		stored, err := s.servers.DiskFull(serverID)
		if err != nil {
			slog.Error("read disk alert state failed", "server_id", serverID, "error", err)
			return
		}
		full = stored
	}
	next := diskState(full, percent)
	s.mu.Lock()
	if s.diskFull == nil {
		s.diskFull = map[string]bool{}
	}
	s.diskFull[serverID] = next
	s.mu.Unlock()
	if next == full {
		return
	}
	if err := s.servers.SetDiskFull(serverID, next, s.now()); err != nil {
		slog.Error("store disk alert state failed", "server_id", serverID, "error", err)
		return
	}
	workspaceID, ok := s.UserOf(serverID)
	if !ok {
		return
	}
	server, err := s.servers.ByIDOrNameForWorkspace(serverID, workspaceID)
	if err != nil {
		return
	}
	details := map[string]string{
		"used_percent": fmt.Sprintf("%.0f", percent),
		"used":         human.Size(float64(used)),
		"total":        human.Size(float64(total)),
	}
	action := "disk.ok"
	if next {
		action = "disk.full"
	}
	s.record(System(workspaceID), action, "server", server.Name, details)
}

// diskState is whether a disk counts as full now: it turns full at
// diskFullAt and back at diskOKAt, and keeps its state in between.
func diskState(full bool, percent float64) bool {
	if full {
		return percent >= diskOKAt
	}
	return percent >= diskFullAt
}
