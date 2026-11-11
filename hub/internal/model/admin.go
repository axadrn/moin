package model

import "time"

const AdminActivityRetention = 180 * 24 * time.Hour

type AdminMetrics struct {
	Users          int
	VerifiedUsers  int
	Workspaces     int
	PaidWorkspaces int
	ActiveSessions int
}

type AdminUser struct {
	ID                 string     `db:"id"`
	Email              string     `db:"email"`
	Name               string     `db:"name"`
	EmailVerifiedAt    *time.Time `db:"email_verified_at"`
	CreatedAt          time.Time  `db:"created_at"`
	WorkspaceCount     int        `db:"workspace_count"`
	ActiveSessionCount int        `db:"active_session_count"`
	IsAdmin            bool       `db:"-"`
	// Plan is what the first workspace the user created is on, and
	// TrialEndsAt when its trial ends.
	Plan        string     `db:"-"`
	TrialEndsAt *time.Time `db:"-"`
	// Offer is the offer the user got, or the one that waits for their
	// address, and Note what admins wrote about them. Only the page of
	// one user has them.
	Offer *Offer    `db:"-"`
	Note  *UserNote `db:"-"`
}

// The plans the admin list shows per user.
const (
	AdminPlanNoWorkspace = "no workspace"
	AdminPlanTrial       = "trial"
	AdminPlanTrialEnded  = "trial ended"
	AdminPlanFreeForever = "free forever"
	AdminPlanPaying      = "paying"
)

type AdminUserPage struct {
	Users      []AdminUser
	Query      string
	Page       int
	TotalPages int
	TotalUsers int
}

type AdminOverview struct {
	Metrics AdminMetrics
	Users   AdminUserPage
	Events  []AdminActivityEvent
	// Network is everything moin runs, hosted and self-hosted; only the
	// hosted hub has it, since self-hosted ones report to it.
	Network *Network
}

type AdminActivityEvent struct {
	ID          string  `db:"id"`
	ActorUserID *string `db:"actor_user_id"`
	ActorEmail  string  `db:"actor_email"`
	// TargetEmail is the address of the user an event touched, when it
	// touched one that still exists.
	TargetEmail string    `db:"target_email"`
	WorkspaceID *string   `db:"workspace_id"`
	Action      string    `db:"action"`
	TargetType  string    `db:"target_type"`
	TargetID    string    `db:"target_id"`
	Metadata    string    `db:"metadata"`
	CreatedAt   time.Time `db:"created_at"`
}

func AdminActivityActionLabel(action string) string {
	switch action {
	case "admin.offer_created":
		return "Offer added"
	case "admin.offer_deleted":
		return "Offer removed"
	case "admin.offer_applied":
		return "Offer applied"
	case "admin.offers_claimed":
		return "Open offers applied"
	case "admin.note_saved":
		return "Note saved"
	case "admin.impersonation_started":
		return "Impersonation started"
	case "admin.impersonation_stopped":
		return "Impersonation stopped"
	case "admin.sessions_revoked":
		return "User sessions revoked"
	case "admin.free_forever_started":
		return "Free forever started"
	case "admin.free_forever_ended":
		return "Free forever ended"
	case "workspace.deleted":
		return "Workspace deleted"
	case "subscription.updated":
		return "Subscription updated"
	default:
		return action
	}
}
