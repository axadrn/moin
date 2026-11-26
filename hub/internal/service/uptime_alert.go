package service

import (
	"log/slog"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// storeUptimeChange stores the new state of a domain that went down or
// came back, and starts or ends its outage.
func (s *AppService) storeUptimeChange(before, stored model.DomainUptime, failure string) {
	err := s.apps.SaveUptimeState(stored)
	switch {
	case err != nil:
	case stored.State == model.UptimeDown:
		err = s.apps.StartUptimeOutage(stored.AppID, stored.Domain, *stored.Since, failure)
	case before.State == model.UptimeDown:
		err = s.apps.EndUptimeOutage(stored.AppID, stored.Domain, *stored.Since)
	}
	if err != nil {
		slog.Error("store uptime change failed", "error", err, "domain", stored.Domain)
	}
}

// alertUptimeChange writes a domain that went down or came back to the
// activity, which sends the alert: uptime.down with the reason, uptime.up
// with how long it was down.
func (s *AppService) alertUptimeChange(workspaceID, appName string, before, stored model.DomainUptime, failure string) {
	switch {
	case stored.State == model.UptimeDown:
		s.servers.record(System(workspaceID), "uptime.down", "app", appName, map[string]string{"domain": stored.Domain, "reason": failure})
	case before.State == model.UptimeDown:
		s.servers.record(System(workspaceID), "uptime.up", "app", appName, map[string]string{"domain": stored.Domain, "duration": stored.Since.Sub(*before.Since).Round(time.Second).String()})
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
