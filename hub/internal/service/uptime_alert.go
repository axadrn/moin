package service

import (
	"log/slog"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// recordUptimeChange stores the outage of a domain that went down or came
// back and writes it to the activity: uptime.down with the reason,
// uptime.up with how long it was down.
func (s *AppService) recordUptimeChange(workspaceID, appName string, before, stored model.DomainUptime, failure string) {
	actor := System(workspaceID)
	switch {
	case stored.State == model.UptimeDown:
		_ = s.apps.StartUptimeOutage(stored.AppID, stored.Domain, *stored.Since, failure)
		s.servers.record(actor, "uptime.down", "app", appName, map[string]string{"domain": stored.Domain, "reason": failure})
	case before.State == model.UptimeDown:
		_ = s.apps.EndUptimeOutage(stored.AppID, stored.Domain, *stored.Since)
		s.servers.record(actor, "uptime.up", "app", appName, map[string]string{"domain": stored.Domain, "duration": stored.Since.Sub(*before.Since).Round(time.Second).String()})
	}
}

// watchServers writes a server whose daemon is gone for a while to the
// activity as down, and back up when it connects again.
func (s *AppService) watchServers(now time.Time) {
	servers, err := s.apps.ServersSeen()
	if err != nil {
		slog.Error("list servers for uptime failed", "error", err)
		return
	}
	for _, server := range servers {
		online := s.servers.Online(server.ID)
		switch {
		case online && server.DownSince != nil:
			err = s.apps.EndServerOutage(server.ID, now)
			if err == nil {
				s.servers.record(System(server.WorkspaceID), "server.up", "server", server.Name, map[string]string{"duration": now.Sub(*server.DownSince).Round(time.Second).String()})
			}
		case !online && server.DownSince == nil:
			gone := server.LastSeenAt
			if gone.Before(s.uptime.started) {
				gone = s.uptime.started
			}
			if now.Sub(gone) < serverDownAfter {
				continue
			}
			err = s.apps.StartServerOutage(server.ID, server.LastSeenAt)
			if err == nil {
				s.servers.record(System(server.WorkspaceID), "server.down", "server", server.Name, map[string]string{"since": server.LastSeenAt.UTC().Format("2006-01-02 15:04 UTC")})
			}
		}
		if err != nil {
			slog.Error("store server outage failed", "error", err, "server_id", server.ID)
		}
	}
}
