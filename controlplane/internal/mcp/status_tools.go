package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/axadrn/moin/controlplane/internal/api"
)

func registerStatusTools(server *mcpsdk.Server, services Services) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: "get_status", Title: "Is everything OK?", Annotations: readOnly("Is everything OK?"),
		Description: "Answers whether everything is OK in the workspace: what needs the user, most urgent first (servers offline, failed deploys, broken backups or restore checks, domains down, disks over 90%, agents behind), each with the web page to fix it; catalog apps with a newer release; and counts of apps live, servers online and backups checked in the last 24 hours. ok is false when something needs the user. Needs the read scope.",
	}, Read(member(services, func(_ context.Context, caller Caller, _ struct{}) (Status, error) {
		status, err := services.Status.Status(caller.Token.WorkspaceID)
		if err != nil {
			return Status{}, err
		}
		return Status{api.NewStatus(status)}, nil
	})))
}

// Status is the output of get_status.
type Status struct {
	api.StatusResponse
}

func (s Status) Summary() string {
	counts := fmt.Sprintf("%d of %d apps live, %d of %d servers online, %d of %d backups checked today", s.Counts.AppsLive, s.Counts.Apps, s.Counts.ServersOnline, s.Counts.Servers, s.Counts.BackupsChecked, s.Counts.Databases)
	if s.OK {
		return "All good: " + counts + "."
	}
	issues := make([]string, 0, len(s.Issues))
	for _, issue := range s.Issues {
		issues = append(issues, issue.Target+": "+issue.Detail)
	}
	return fmt.Sprintf("%d need you: %s. %s.", len(s.Issues), strings.Join(issues, "; "), counts)
}
