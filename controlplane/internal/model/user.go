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
	// AppsView and ServersView are how the user sees the Apps and Servers
	// pages: ViewList, ViewGrid, or empty for the page's default.
	AppsView    string `db:"apps_view"`
	ServersView string `db:"servers_view"`

	// Computed fields (not in database)
	AvatarURL   string `db:"-"`
	PasswordSet bool   `db:"-"`
}

// The ways the Apps and Servers pages show their items.
const (
	ViewList = "list"
	ViewGrid = "grid"
)

// View is how the user sees the "apps" or the "servers" page: apps as a
// list and servers as a grid, unless they picked the other.
func (u *User) View(page string) string {
	view, fallback := u.AppsView, ViewList
	if page == "servers" {
		view, fallback = u.ServersView, ViewGrid
	}
	if view == ViewList || view == ViewGrid {
		return view
	}
	return fallback
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
