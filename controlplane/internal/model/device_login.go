package model

import "time"

const (
	// DeviceLoginPending waits for the user to approve or deny it.
	DeviceLoginPending = "pending"
	// DeviceLoginApproved has a user and a workspace; the next poll of the
	// CLI gets its token.
	DeviceLoginApproved = "approved"
	// DeviceLoginDenied was turned down; the next poll of the CLI hears so.
	DeviceLoginDenied = "denied"
)

// DeviceLogin is one `moin login` in the browser (RFC 8628, device
// authorization grant). The CLI holds the device code and polls with it; the
// user opens the user code on the web. Only hashes of both are stored.
type DeviceLogin struct {
	ID             string `db:"id"`
	DeviceCodeHash string `db:"device_code_hash"`
	UserCodeHash   string `db:"user_code_hash"`
	// Hostname is the computer the CLI runs on, as it says.
	Hostname string `db:"hostname"`
	Status   string `db:"status"`
	// UserID and WorkspaceID are set once the user approved or denied.
	UserID      *string `db:"user_id"`
	WorkspaceID *string `db:"workspace_id"`
	// IntervalSeconds is how long the CLI waits between polls. Polling
	// faster adds five seconds.
	IntervalSeconds int        `db:"interval_seconds"`
	LastPolledAt    *time.Time `db:"last_polled_at"`
	ExpiresAt       time.Time  `db:"expires_at"`
	CreatedAt       time.Time  `db:"created_at"`
}

// IsExpired reports whether the login ran out of time.
func (l DeviceLogin) IsExpired(now time.Time) bool {
	return !l.ExpiresAt.After(now)
}
