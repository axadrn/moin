package service

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/secret"
)

// AlertService tells the team when a site goes down or comes back, a
// server goes offline or comes back, or a deploy fails. It mails the owners
// and admins of the workspace with "Email me alerts" on and a verified
// address, and posts to the alert destinations of the workspace that get
// the event: Slack, Discord, Telegram or a webhook (alert_destination.go).
type AlertService struct {
	email        *EmailService
	workspaces   repository.WorkspaceRepository
	members      repository.WorkspaceMemberRepository
	apps         repository.AppRepository
	destinations *repository.AlertDestinations
	audit        repository.WorkspaceAuditRepository
	box          *secret.Box
	sender       *alertSender
	now          func() time.Time
}

func NewAlertService(email *EmailService, workspaces repository.WorkspaceRepository, members repository.WorkspaceMemberRepository, apps repository.AppRepository, destinations *repository.AlertDestinations, audit repository.WorkspaceAuditRepository, box *secret.Box) *AlertService {
	return &AlertService{
		email: email, workspaces: workspaces, members: members, apps: apps,
		destinations: destinations, audit: audit, box: box,
		sender: newAlertSender(false), now: time.Now,
	}
}

// alerted says whether an action of the activity sends an alert.
func alerted(action string) bool {
	return slices.Contains(model.AlertEvents, action)
}

// alertMessage is one alert as every destination says it: the symbol and
// the heading, the lines below, the link into moin, and the fields of the
// webhook body.
type alertMessage struct {
	Symbol  string
	Heading string
	Lines   []string
	URL     string
	Payload alertPayload
}

// Title is the symbol and the heading, the subject of the mail.
func (m alertMessage) Title() string { return m.Symbol + " " + m.Heading }

// alertPayload is the JSON body a webhook gets. Fields that do not belong
// to the event are empty.
type alertPayload struct {
	Event     string `json:"event"`
	Workspace string `json:"workspace"`
	App       string `json:"app"`
	Server    string `json:"server"`
	Domain    string `json:"domain"`
	Reason    string `json:"reason"`
	Since     string `json:"since"`
	Duration  string `json:"duration"`
	URL       string `json:"url"`
}

// Send tells of an action of the activity, written at at: a mail to every
// owner and admin of the workspace who gets them, and a post to every
// destination that gets the action. Other actions send nothing. A failure
// is logged; a destination that begins to fail is in the activity once.
func (s *AlertService) Send(workspaceID, action, target string, details map[string]string, at time.Time) {
	if !alerted(action) {
		return
	}
	workspace, err := s.workspaces.ByID(workspaceID)
	if err != nil {
		slog.Error("alert: find the workspace failed", "workspace_id", workspaceID, "action", action, "error", err)
		return
	}
	message := s.message(workspace, action, target, details, at)
	s.mail(workspace, action, message)
	s.post(workspace, action, message)
}

// message is what an alert says, the same in every mail and destination.
func (s *AlertService) message(workspace *model.Workspace, action, target string, details map[string]string, at time.Time) alertMessage {
	base := s.email.appURL + "/app/" + workspace.Slug
	message := alertMessage{Payload: alertPayload{Event: action, Workspace: workspace.Slug}}
	switch action {
	case "uptime.down":
		domain := details["domain"]
		message.Symbol, message.Heading = "🔴", domain+" is down"
		message.Lines = []string{
			"App: " + target,
			"Since: " + at.UTC().Format("2006-01-02 15:04 UTC"),
			"Reason: " + orDash(details["reason"]),
		}
		message.URL = base + "/apps/" + target + "/uptime"
		message.Payload.App, message.Payload.Server, message.Payload.Domain = target, s.serverOf(workspace.ID, target), domain
		message.Payload.Reason, message.Payload.Since = details["reason"], at.UTC().Format(time.RFC3339)
	case "uptime.up":
		domain := details["domain"]
		message.Symbol, message.Heading = "🟢", domain+" is back up"
		message.Lines = []string{
			"App: " + target,
			"It was down for " + spokenDuration(details["duration"]) + ".",
		}
		message.URL = base + "/apps/" + target + "/uptime"
		message.Payload.App, message.Payload.Server, message.Payload.Domain = target, s.serverOf(workspace.ID, target), domain
		message.Payload.Duration = details["duration"]
	case "server.down":
		message.Symbol, message.Heading = "🔴", "Server "+target+" is offline"
		message.Lines = []string{
			"Its agent has not answered since " + orDash(details["since"]) + ".",
			s.appsOn(workspace.ID, target),
		}
		message.URL = base + "/servers"
		message.Payload.Server, message.Payload.Since = target, rfc3339(details["since"])
	case "server.up":
		message.Symbol, message.Heading = "🟢", "Server "+target+" is back online"
		message.Lines = []string{"It was offline for " + spokenDuration(details["duration"]) + "."}
		message.URL = base + "/servers"
		message.Payload.Server, message.Payload.Duration = target, details["duration"]
	case "deploy.failed":
		message.Symbol, message.Heading = "🔴", "Deploy of "+target+" failed"
		message.Lines = []string{
			"Deploy v" + orDash(details["number"]) + " of " + target + " failed.",
			"Error: " + orDash(clipText(details["error"], 300)),
			s.stillLive(workspace.ID, target),
		}
		message.URL = base + "/apps/" + target
		message.Payload.App, message.Payload.Server, message.Payload.Reason = target, s.serverOf(workspace.ID, target), clipText(details["error"], 300)
	case "backup.failed":
		database := details["database"]
		message.Symbol, message.Heading = "🔴", "Backups of "+target+" stopped"
		message.Lines = []string{
			"Database: " + database + ", app " + target + ".",
			"Error: " + orDash(clipText(details["error"], 300)),
			"New changes are not in your bucket until this is fixed. What is there stays. See: moin backups " + target,
		}
		message.URL = base + "/apps/" + target + "/backups"
		message.Payload.App, message.Payload.Server, message.Payload.Reason = target, s.serverOf(workspace.ID, target), clipText(details["error"], 300)
	case "backup.recovered":
		message.Symbol, message.Heading = "🟢", "Backups of "+target+" work again"
		message.Lines = []string{"Database: " + details["database"] + ", app " + target + ". Every change goes to your bucket again."}
		message.URL = base + "/apps/" + target + "/backups"
		message.Payload.App, message.Payload.Server = target, s.serverOf(workspace.ID, target)
	case "drill.failed":
		message.Symbol, message.Heading = "🔴", "Daily check of "+target+" failed"
		message.Lines = []string{
			"Database: " + details["database"] + ", app " + target + ".",
			"Error: " + orDash(clipText(details["error"], 300)),
			"The daily check could not go back to the newest state. Download a copy now and look: moin backups " + target,
		}
		message.URL = base + "/apps/" + target + "/backups"
		message.Payload.App, message.Payload.Server, message.Payload.Reason = target, s.serverOf(workspace.ID, target), clipText(details["error"], 300)
	case "drill.recovered":
		message.Symbol, message.Heading = "🟢", "Daily check of "+target+" passes again"
		message.Lines = []string{"Database: " + details["database"] + ", app " + target + ". Going back works again."}
		message.URL = base + "/apps/" + target + "/backups"
		message.Payload.App, message.Payload.Server = target, s.serverOf(workspace.ID, target)
	case "disk.full":
		message.Symbol, message.Heading = "🟠", "Disk of "+target+" is "+details["used_percent"]+"% full"
		message.Lines = []string{
			details["used"] + " of " + details["total"] + " used.",
			"A full disk stops databases from writing. Free space or grow the disk. " + s.appsOn(workspace.ID, target),
		}
		message.URL = base + "/servers"
		message.Payload.Server, message.Payload.Reason = target, details["used_percent"]+"% used"
	case "update.available":
		message.Symbol, message.Heading = "⬆️", details["catalog"]+" "+details["version"]+" is out for "+target
		message.Lines = []string{
			details["catalog"] + " " + details["version"] + " is out for " + target + " (you run " + details["current"] + "). Update in one click; moin backs up first.",
			"Or run: moin update " + target,
		}
		message.URL = base + "/apps/" + target
		message.Payload.App, message.Payload.Server = target, s.serverOf(workspace.ID, target)
		message.Payload.Reason = details["catalog"] + " " + details["version"] + ", runs " + details["current"]
	case "disk.ok":
		message.Symbol, message.Heading = "🟢", "Disk of "+target+" has room again"
		message.Lines = []string{details["used"] + " of " + details["total"] + " used, " + details["used_percent"] + "%."}
		message.URL = base + "/servers"
		message.Payload.Server, message.Payload.Reason = target, details["used_percent"]+"% used"
	}
	message.Lines = nonEmpty(message.Lines)
	message.Payload.URL = message.URL
	return message
}

// mail sends the alert to every owner and admin who gets them.
func (s *AlertService) mail(workspace *model.Workspace, action string, message alertMessage) {
	emails, err := s.members.AlertEmails(workspace.ID)
	if err != nil {
		slog.Error("alert: list the recipients failed", "workspace_id", workspace.ID, "action", action, "error", err)
		return
	}
	if len(emails) == 0 {
		return
	}
	content := emailContent{
		AppName:     s.email.appName,
		Subject:     message.Title(),
		Heading:     message.Heading,
		Preheader:   message.Heading,
		Intro:       message.Lines,
		ActionURL:   message.URL,
		ActionLabel: "Open in " + s.email.appName,
		Outro:       []string{fmt.Sprintf("You get this because you are an owner or admin of %s. Turn it off: %s/app/%s/settings", workspace.Name, s.email.appURL, workspace.Slug)},
	}
	for _, to := range emails {
		if err := s.email.send("alert_"+strings.ReplaceAll(action, ".", "_"), to, content); err != nil {
			slog.Error("alert: send failed", "workspace_id", workspace.ID, "action", action, "error", err)
		}
	}
}

// serverOf names the server of an app, for the webhook body.
func (s *AlertService) serverOf(workspaceID, appName string) string {
	app, err := s.apps.ByIDOrNameForWorkspace(appName, workspaceID)
	if err != nil {
		return ""
	}
	return app.ServerName
}

// appsOn names the apps on a server, for a server.down alert.
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

// rfc3339 turns a time of the activity, such as "2026-11-03 14:00 UTC",
// into RFC 3339 for the webhook body; anything else stays as it is.
func rfc3339(value string) string {
	at, err := time.Parse("2006-01-02 15:04 UTC", value)
	if err != nil {
		return value
	}
	return at.UTC().Format(time.RFC3339)
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
