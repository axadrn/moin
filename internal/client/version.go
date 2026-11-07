package client

import (
	"context"
	"net/http"
	"time"
)

// Version is the control plane's version and, on a self-hosted one, the
// newest release and the update under way.
type Version struct {
	Version         string     `json:"version"`
	SelfHosted      bool       `json:"self_hosted"`
	Latest          string     `json:"latest"`
	UpdateAvailable bool       `json:"update_available"`
	PublishedAt     *time.Time `json:"published_at"`
	NotesURL        string     `json:"notes_url"`
	Summary         string     `json:"summary"`
	CanUpdate       bool       `json:"can_update"`
	Updater         bool       `json:"updater"`
	Update          *Update    `json:"update"`
}

// Update is an update of the control plane: requested, updating, done,
// rolled_back or failed.
type Update struct {
	State   string    `json:"state"`
	Version string    `json:"version"`
	From    string    `json:"from"`
	Reason  string    `json:"reason"`
	At      time.Time `json:"at"`
}

// Running reports whether the update has not ended yet.
func (u *Update) Running() bool {
	return u != nil && (u.State == "requested" || u.State == "updating")
}

// Version returns the control plane's version and the newest release.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var response Version
	err := c.do(ctx, http.MethodGet, "/api/v1/version", nil, &response)
	return response, err
}

// UpdateControlPlane has the updater next to a self-hosted control plane
// install the newest release.
func (c *Client) UpdateControlPlane(ctx context.Context) (Version, error) {
	var response Version
	err := c.do(ctx, http.MethodPost, "/api/v1/version/update", nil, &response)
	return response, err
}

type daemonUpdates struct {
	Auto bool `json:"auto"`
}

// DaemonUpdates reports whether the daemons of the workspace update to the
// control plane's version by themselves.
func (c *Client) DaemonUpdates(ctx context.Context) (bool, error) {
	var response daemonUpdates
	err := c.do(ctx, http.MethodGet, "/api/v1/daemon-updates", nil, &response)
	return response.Auto, err
}

// SetDaemonUpdates turns automatic daemon updates on or off.
func (c *Client) SetDaemonUpdates(ctx context.Context, on bool) error {
	return c.do(ctx, http.MethodPut, "/api/v1/daemon-updates", daemonUpdates{Auto: on}, nil)
}
