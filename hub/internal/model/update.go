package model

import "time"

// Release is a release of moin on GitHub.
type Release struct {
	Version     string
	URL         string
	PublishedAt time.Time
	// Summary is the "In short" line of the notes.
	Summary string
	// Notes are the release notes in Markdown.
	Notes string
}

// UpdateState is an update of the hub: requested, under way or
// ended, as the updater next to it reports.
type UpdateState struct {
	// State is requested, updating, done, rolled_back or failed.
	State   string    `json:"state"`
	Version string    `json:"version"`
	From    string    `json:"from"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// Running reports whether the update has not ended yet.
func (u UpdateState) Running() bool {
	return u.State == "requested" || u.State == "updating"
}

// VersionInfo is the version of the hub and, on a self-hosted
// one, the newest release and how to get it.
type VersionInfo struct {
	Version string
	// SelfHosted is false on the hosted hub, which updates with
	// every release and shows nothing of this.
	SelfHosted      bool
	Latest          string
	UpdateAvailable bool
	PublishedAt     *time.Time
	NotesURL        string
	Summary         string
	Notes           string
	CheckedAt       *time.Time
	// Admin is whether the user runs this hub.
	Admin bool
	// Updater is whether the updater service runs next to it.
	Updater bool
	// CanUpdate is whether the user can start an update now.
	CanUpdate bool
	// Update is the update under way, or the last one of the past day.
	Update *UpdateState
}
