package client

import (
	"context"
	"net/http"
	"time"
)

// Version is the hub's version and, on a self-hosted one, the
// newest release.
type Version struct {
	Version         string     `json:"version"`
	SelfHosted      bool       `json:"self_hosted"`
	Latest          string     `json:"latest"`
	UpdateAvailable bool       `json:"update_available"`
	PublishedAt     *time.Time `json:"published_at"`
	NotesURL        string     `json:"notes_url"`
	Summary         string     `json:"summary"`
	ReadFirst       string     `json:"read_first"`
	// HubBackup is how the backup of the hub's own database stands; only
	// the admins of the hub get it.
	HubBackup *HubBackup `json:"hub_backup"`
}

// HubBackup is how the backup of the hub's own database stands: State is
// on, off or late.
type HubBackup struct {
	State    string     `json:"state"`
	Bucket   string     `json:"bucket"`
	LastCopy *time.Time `json:"last_copy"`
}

// Version returns the hub's version and the newest release.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var response Version
	err := c.do(ctx, http.MethodGet, "/api/v1/version", nil, &response)
	return response, err
}

type daemonUpdates struct {
	Auto bool `json:"auto"`
}

// DaemonUpdates reports whether the daemons of the workspace update to the
// hub's version by themselves.
func (c *Client) DaemonUpdates(ctx context.Context) (bool, error) {
	var response daemonUpdates
	err := c.do(ctx, http.MethodGet, "/api/v1/daemon-updates", nil, &response)
	return response.Auto, err
}

// SetDaemonUpdates turns automatic daemon updates on or off.
func (c *Client) SetDaemonUpdates(ctx context.Context, on bool) error {
	return c.do(ctx, http.MethodPut, "/api/v1/daemon-updates", daemonUpdates{Auto: on}, nil)
}
