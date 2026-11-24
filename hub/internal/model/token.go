package model

import (
	"time"
)

type Token struct {
	ID        string    `db:"id"`
	UserID    string    `db:"user_id"`
	Type      string    `db:"type"` // "email_verify" or "email_change"
	TokenHash string    `db:"token_hash"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
}

const (
	TokenTypeEmailVerify = "email_verify"
	TokenTypeEmailChange = "email_change"
)

func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

func (t *Token) IsValid() bool {
	return !t.IsExpired()
}
