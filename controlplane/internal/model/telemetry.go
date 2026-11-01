package model

import "time"

// Counts are the numbers a control plane reports about itself: never a
// name, a domain or an address.
type Counts struct {
	Servers int `db:"servers" json:"servers"`
	Apps    int `db:"apps" json:"apps"`
	Deploys int `db:"deploys" json:"deploys"`
}

// TelemetryReport is what a self-hosted control plane sends every five
// minutes.
type TelemetryReport struct {
	InstanceID string `db:"instance_id" json:"instance"`
	Version    string `db:"version" json:"version"`
	Counts     `json:"counts"`
	SeenAt     time.Time `db:"seen_at" json:"-"`
}

// Network is everything ops runs right now: the hosted control plane and
// every self-hosted one that reported in the last fifteen minutes.
type Network struct {
	SelfHosted       int
	HostedWorkspaces int
	Counts
}
