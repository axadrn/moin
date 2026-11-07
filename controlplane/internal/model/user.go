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
	// EmailAlerts sends the user a mail when a site goes down or comes
	// back, a server goes offline or comes back, or a deploy fails, in the
	// workspaces they own or run as admin. On by default.
	EmailAlerts bool `db:"email_alerts"`

	// Computed fields (not in database)
	AvatarURL   string `db:"-"`
	PasswordSet bool   `db:"-"`
}

func (u *User) HasPassword() bool {
	return u.PasswordSet || (u.PasswordHash != nil && *u.PasswordHash != "")
}

// Initial is the first letter of text in upper case, or "?" without one:
// the tile of an app or a workspace, the avatar of a member.
func Initial(text string) string {
	first, _ := utf8.DecodeRuneInString(strings.TrimSpace(text))
	if first == utf8.RuneError {
		return "?"
	}
	return strings.ToUpper(string(first))
}

// Initial is the letter of the avatar without a picture: the first of the
// name, or of the email when there is no name. Every avatar shows the same.
func (u *User) Initial() string {
	if strings.TrimSpace(u.Name) != "" {
		return Initial(u.Name)
	}
	return Initial(u.Email)
}

// Initials are the letters of your own avatar without a picture: the first
// and the last word of a name of two words or more, "AL" for Ada Lovelace,
// else Initial.
func (u *User) Initials() string {
	words := strings.Fields(u.Name)
	if len(words) < 2 {
		return u.Initial()
	}
	return Initial(words[0]) + Initial(words[len(words)-1])
}
