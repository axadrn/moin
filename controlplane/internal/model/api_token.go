package model

import (
	"strings"
	"time"
)

const (
	// APITokenScopeRead lets a token call GET endpoints.
	APITokenScopeRead = "read"
	// APITokenScopeWrite also lets a token change data.
	APITokenScopeWrite = "write"

	// APITokenKindPersonal is a token made in Settings, for scripts, CI
	// and AI agents.
	APITokenKindPersonal = "personal"
	// APITokenKindDevice is the token of one computer, from `moin login`
	// in the browser. It expires after DeviceTokenIdle without use.
	APITokenKindDevice = "device"
)

// DeviceTokenIdle is how long a device token lives without being used.
// Each use starts it again.
const DeviceTokenIdle = 90 * 24 * time.Hour

// APIToken is a personal API token. The raw token is shown once at creation
// and only its SHA-256 hash is stored. Prefix is the start of the raw token
// after the "pat_" marker, so people can tell their tokens apart.
type APIToken struct {
	ID     string `db:"id"`
	UserID string `db:"user_id"`
	// WorkspaceID binds the token to one workspace. Other workspaces are not
	// visible to it.
	WorkspaceID string `db:"workspace_id"`
	Name        string `db:"name"`
	TokenHash   string `db:"token_hash"`
	Prefix      string `db:"prefix"`
	// Scopes is "read" or "read,write".
	Scopes string `db:"scopes"`
	// Kind is APITokenKindPersonal or APITokenKindDevice.
	Kind       string     `db:"kind"`
	LastUsedAt *time.Time `db:"last_used_at"`
	ExpiresAt  *time.Time `db:"expires_at"`
	CreatedAt  time.Time  `db:"created_at"`
}

// ScopeList returns the scopes as a list, for example ["read", "write"].
func (t APIToken) ScopeList() []string {
	return strings.Split(t.Scopes, ",")
}

// HasScope reports whether the token carries scope.
func (t APIToken) HasScope(scope string) bool {
	for _, granted := range t.ScopeList() {
		if granted == scope {
			return true
		}
	}
	return false
}

// CanWrite reports whether the token may change data.
func (t APIToken) CanWrite() bool {
	return t.HasScope(APITokenScopeWrite)
}

// IsExpired reports whether the token has an expiry that has passed.
func (t APIToken) IsExpired(now time.Time) bool {
	return t.ExpiresAt != nil && !t.ExpiresAt.After(now)
}

// IsDevice reports whether the token is a computer's login from the browser.
func (t APIToken) IsDevice() bool {
	return t.Kind == APITokenKindDevice
}
