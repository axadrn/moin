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

type agentUpdates struct {
	Auto bool `json:"auto"`
}

// AgentUpdates reports whether the agents of the workspace update to the
// control plane's version by themselves.
func (c *Client) AgentUpdates(ctx context.Context) (bool, error) {
	var response agentUpdates
	err := c.do(ctx, http.MethodGet, "/api/v1/agent-updates", nil, &response)
	return response.Auto, err
}

// SetAgentUpdates turns automatic agent updates on or off.
func (c *Client) SetAgentUpdates(ctx context.Context, on bool) error {
	return c.do(ctx, http.MethodPut, "/api/v1/agent-updates", agentUpdates{Auto: on}, nil)
}
