package model

import (
	"strings"
	"time"
	"unicode/utf8"
)

type User struct {
	ID              string     `db:"id"`
	Email           string     `db:"email"`
	PasswordHash    *string    `db:"password_hash"` // Nullable for passwordless users
	PendingEmail    *string    `db:"pending_email"`
	EmailVerifiedAt *time.Time `db:"email_verified_at"`
	Name            string     `db:"name"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
	// AppSort is how the user sorts the apps: SortSmart, SortRecent or
	// SortServer. The same in the web and the TUI.
	AppSort string `db:"app_sort"`
	// EmailAlerts sends the user a mail when a site goes down or comes
	// back, a server goes offline or comes back, or a deploy fails, in the
	// workspaces they own or run as admin. On by default.
	EmailAlerts bool `db:"email_alerts"`

	// Computed fields (not in database)
	AvatarURL   string `db:"-"`
	PasswordSet bool   `db:"-"`
}

// The ways to sort the apps. Smart puts what needs you first, then what
// deploys, then the pinned apps, then the rest by name. Recent puts the
// newest deploy first. Server groups the apps by their server.
const (
	SortSmart  = "smart"
	SortRecent = "recent"
	SortServer = "server"
)

// Sort is how the user sorts the apps, smart unless they picked another.
func (u *User) Sort() string {
	if u.AppSort == SortRecent || u.AppSort == SortServer {
		return u.AppSort
	}
	return SortSmart
}

func (u *User) HasPassword() bool {
	return u.PasswordSet || (u.PasswordHash != nil && *u.PasswordHash != "")
}

// Initial is the letter of the avatar without a picture: the first of the
// name, or of the email when there is no name. Every avatar shows the same.
func (u *User) Initial() string {
	text := strings.TrimSpace(u.Name)
	if text == "" {
		text = strings.TrimSpace(u.Email)
	}
	first, _ := utf8.DecodeRuneInString(text)
	if first == utf8.RuneError {
		return "?"
	}
	return strings.ToUpper(string(first))
}
