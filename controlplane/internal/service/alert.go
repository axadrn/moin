package service

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/repository"
)

// AlertService mails the owners and admins of a workspace when a site goes
// down or comes back, a server goes offline or comes back, or a deploy
// fails. Only those with "Email me alerts" on and a verified address get it.
type AlertService struct {
	email      *EmailService
	workspaces repository.WorkspaceRepository
	members    repository.WorkspaceMemberRepository
	apps       repository.AppRepository
}

func NewAlertService(email *EmailService, workspaces repository.WorkspaceRepository, members repository.WorkspaceMemberRepository, apps repository.AppRepository) *AlertService {
	return &AlertService{email: email, workspaces: workspaces, members: members, apps: apps}
}

// alerted says whether an action of the activity sends an email alert.
func alerted(action string) bool {
	switch action {
	case "uptime.down", "uptime.up", "server.down", "server.up", "deploy.failed":
		return true
	}
	return false
}

// Send mails an alert for an action of the activity, written at at, to
// every owner and admin of the workspace who gets them. Other actions send
// nothing. A failure is logged.
func (s *AlertService) Send(workspaceID, action, target string, details map[string]string, at time.Time) {
	if !alerted(action) {
		return
	}
	workspace, err := s.workspaces.ByID(workspaceID)
	if err != nil {
		slog.Error("alert: find the workspace failed", "workspace_id", workspaceID, "action", action, "error", err)
		return
	}
	emails, err := s.members.AlertEmails(workspaceID)
	if err != nil {
		slog.Error("alert: list the recipients failed", "workspace_id", workspaceID, "action", action, "error", err)
		return
	}
	if len(emails) == 0 {
		return
	}
	base := s.email.appURL + "/app/" + workspace.Slug
	content := emailContent{AppName: s.email.appName}
	switch action {
	case "uptime.down":
		domain := details["domain"]
		content.Subject = "🔴 " + domain + " is down"
		content.Heading = domain + " is down"
		content.Intro = []string{
			"App: " + target,
			"Since: " + at.UTC().Format("2006-01-02 15:04 UTC"),
			"Reason: " + orDash(details["reason"]),
		}
		content.ActionURL = base + "/apps/" + target + "/uptime"
	case "uptime.up":
		domain := details["domain"]
		content.Subject = "🟢 " + domain + " is back up"
		content.Heading = domain + " is back up"
		content.Intro = []string{
			"App: " + target,
			"It was down for " + spokenDuration(details["duration"]) + ".",
		}
		content.ActionURL = base + "/apps/" + target + "/uptime"
	case "server.down":
		content.Subject = "🔴 Server " + target + " is offline"
		content.Heading = "Server " + target + " is offline"
		content.Intro = []string{
			"Its agent has not answered since " + orDash(details["since"]) + ".",
			s.appsOn(workspaceID, target),
		}
		content.ActionURL = base + "/servers"
	case "server.up":
		content.Subject = "🟢 Server " + target + " is back online"
		content.Heading = "Server " + target + " is back online"
		content.Intro = []string{"It was offline for " + spokenDuration(details["duration"]) + "."}
		content.ActionURL = base + "/servers"
	case "deploy.failed":
		content.Subject = "🔴 Deploy of " + target + " failed"
		content.Heading = "Deploy of " + target + " failed"
		content.Intro = []string{
			"Deploy v" + orDash(details["number"]) + " of " + target + " failed.",
			"Error: " + orDash(clipText(details["error"], 300)),
			s.stillLive(workspaceID, target),
		}
		content.ActionURL = base + "/apps/" + target
	}
	content.Intro = nonEmpty(content.Intro)
	content.Preheader = content.Heading
	content.ActionLabel = "Open in " + s.email.appName
	content.Outro = []string{fmt.Sprintf("You get this because you are an owner or admin of %s. Turn it off: %s/settings", workspace.Name, base)}
	for _, to := range emails {
		if err := s.email.send("alert_"+strings.ReplaceAll(action, ".", "_"), to, content); err != nil {
			slog.Error("alert: send failed", "workspace_id", workspaceID, "action", action, "error", err)
		}
	}
}

// appsOn names the apps on a server, for a server.down mail.
func (s *AlertService) appsOn(workspaceID, server string) string {
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		return ""
	}
	names := []string{}
	for _, app := range apps {
		if app.ServerName == server {
			names = append(names, app.Name)
		}
	}
	if len(names) == 0 {
		return "No apps run on it."
	}
	return "Apps on it: " + strings.Join(names, ", ") + "."
}

// stillLive says which release is live after a failed deploy: a failed
// build leaves it as it is, and a failed start starts it again.
func (s *AlertService) stillLive(workspaceID, appName string) string {
	app, err := s.apps.ByIDOrNameForWorkspace(appName, workspaceID)
	switch {
	case err != nil:
		return ""
	case app.Stopped:
		return "The app is stopped."
	case app.LiveNumber == 0:
		return "The app has no live version yet."
	}
	return fmt.Sprintf("The live version is still v%d.", app.LiveNumber)
}

// spokenDuration reads a duration of the activity, such as "4m30s", as
// "4 minutes 30 seconds": its two largest units.
func spokenDuration(value string) string {
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return orDash(value)
	}
	units := []struct {
		size time.Duration
		name string
	}{{24 * time.Hour, "day"}, {time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}}
	parts := []string{}
	for _, unit := range units {
		if n := d / unit.size; n > 0 && len(parts) < 2 {
			d -= n * unit.size
			name := unit.name
			if n != 1 {
				name += "s"
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, name))
		} else if len(parts) > 0 {
			break
		}
	}
	if len(parts) == 0 {
		return "less than a second"
	}
	return strings.Join(parts, " ")
}

func nonEmpty(paragraphs []string) []string {
	kept := paragraphs[:0]
	for _, paragraph := range paragraphs {
		if paragraph != "" {
			kept = append(kept, paragraph)
		}
	}
	return kept
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
