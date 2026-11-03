package model

import "time"

// WaitlistEntry is someone who wants into hosted ops while sign-up is
// closed. LetInAt is set once an admin lets them in. ForeverFree makes
// their first workspace free forever; without it they get the trial
// everyone gets, then pay.
type WaitlistEntry struct {
	Email       string     `db:"email"`
	CreatedAt   time.Time  `db:"created_at"`
	LetInAt     *time.Time `db:"let_in_at"`
	ForeverFree bool       `db:"forever_free"`

	// Plan is what their first workspace is on, for the admin list, and
	// TrialEndsAt when the trial ends.
	Plan        string     `db:"-"`
	TrialEndsAt *time.Time `db:"-"`
}

// The plans the admin list shows per person.
const (
	WaitlistNoAccount   = "no account yet"
	WaitlistTrial       = "trial"
	WaitlistTrialEnded  = "trial ended"
	WaitlistFreeForever = "free forever"
	WaitlistPaying      = "paying"
)
