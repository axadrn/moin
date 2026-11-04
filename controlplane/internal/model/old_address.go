package model

import "time"

// OldAddressDay is how many requests of one kind arrived on an old address
// of the control plane on one day (UTC).
type OldAddressDay struct {
	Day      string    `json:"day"`
	Host     string    `json:"host"`
	Kind     string    `json:"kind"`
	Requests int       `json:"requests"`
	LastAt   time.Time `json:"last_at"`
}

// OldAddress is the traffic on the old addresses over the last days: when
// nothing came for a while, the old address can go.
type OldAddress struct {
	// Hosts are the old addresses the control plane still answers on.
	Hosts []string `json:"hosts"`
	// Days is how far Rows reaches back.
	Days int             `json:"days"`
	Rows []OldAddressDay `json:"rows"`
	// Total is the sum of Rows; LastAt the newest request, zero without one.
	Total  int        `json:"total"`
	LastAt *time.Time `json:"last_at,omitempty"`
	// Quiet is true when nothing arrived in the last QuietDays days.
	Quiet     bool `json:"quiet"`
	QuietDays int  `json:"quiet_days"`
}
