package client

import (
	"context"
	"net/http"
	"time"
)

// StatusIssue is one thing that needs you, with the web page to fix it,
// under the workspace.
type StatusIssue struct {
	Kind   string     `json:"kind"`
	Target string     `json:"target"`
	Detail string     `json:"detail"`
	Path   string     `json:"path"`
	Since  *time.Time `json:"since"`
}

// StatusUpdate is a catalog app with a newer release.
type StatusUpdate struct {
	App     string `json:"app"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Latest  string `json:"latest"`
}

// StatusCounts is what runs well.
type StatusCounts struct {
	Apps           int `json:"apps"`
	AppsLive       int `json:"apps_live"`
	Servers        int `json:"servers"`
	ServersOnline  int `json:"servers_online"`
	Databases      int `json:"databases"`
	BackupsChecked int `json:"backups_checked"`
}

// Status says whether everything is OK in the workspace.
type Status struct {
	OK      bool           `json:"ok"`
	Issues  []StatusIssue  `json:"issues"`
	Updates []StatusUpdate `json:"updates"`
	Counts  StatusCounts   `json:"counts"`
}

// Status asks whether everything is OK.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var status Status
	err := c.do(ctx, http.MethodGet, "/api/v1/status", nil, &status)
	return status, err
}
