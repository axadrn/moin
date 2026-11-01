package model

import "time"

// WaitlistEntry is someone who wants into hosted ops while sign-up is
// closed. LetInAt is set once an admin lets them in.
type WaitlistEntry struct {
	Email       string     `db:"email"`
	CreatedAt   time.Time  `db:"created_at"`
	LetInAt     *time.Time `db:"let_in_at"`
	ForeverFree bool       `db:"forever_free"`
}
