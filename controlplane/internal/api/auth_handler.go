package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/validation"
)

// AuthHandler serves sign-in, onboarding, logout and the account summary.
type AuthHandler struct {
	authService      *service.AuthService
	userService      *service.UserService
	workspaceService *service.WorkspaceService
}

func NewAuthHandler(authService *service.AuthService, userService *service.UserService, workspaceService *service.WorkspaceService) *AuthHandler {
	return &AuthHandler{authService: authService, userService: userService, workspaceService: workspaceService}
}

// onboardingRequest carries the name and, for people who arrive through a
// workspace invitation, the invitation token.
type onboardingRequest struct {
	Name            string `json:"name"`
	InvitationToken string `json:"invitation_token"`
}

type onboardingResponse struct {
	User      User       `json:"user"`
	Workspace *Workspace `json:"workspace"`
}

// CompleteOnboarding sets the name of a new account and creates its default
// workspace when the user has none yet. An available invitation for the
// account's email address skips the default workspace, like the pending
// invitation of the web sign-in, because the account joins that workspace
// next.
func (h *AuthHandler) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	var request onboardingRequest
	if !Decode(w, r, &request) {
		return
	}
	err := validation.ValidateName(request.Name)
	if err != nil {
		ValidationError(w, map[string]string{"name": FieldMessage(err)})
		return
	}

	createDefault := !h.invitedTo(request.InvitationToken, user.Email)
	workspace, err := h.authService.CompleteOnboarding(user.ID, request.Name, createDefault)
	if err != nil {
		slog.Error("onboarding failed", "error", err, "user_id", user.ID)
		ServiceError(w, err)
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	response := onboardingResponse{User: NewUser(updatedUser)}
	if workspace != nil {
		created := NewWorkspace(workspace)
		response.Workspace = &created
	}
	slog.Info("onboarding completed", "user_id", user.ID)
	JSON(w, http.StatusOK, response)
}

// invitedTo reports whether token names an available invitation for email.
// Unknown, used, revoked, expired and foreign invitations count as none.
func (h *AuthHandler) invitedTo(token, email string) bool {
	if token == "" {
		return false
	}
	invitation, err := h.workspaceService.WorkspaceInvitation(token)
	if err != nil {
		return false
	}
	return strings.EqualFold(invitation.Email, email)
}

type accountResponse struct {
	User               User         `json:"user"`
	OnboardingRequired bool         `json:"onboarding_required"`
	Workspaces         []Membership `json:"workspaces"`
	IsAdmin            bool         `json:"is_admin"`
	AvatarURL          *string      `json:"avatar_url"`
}

// Account returns the signed-in user, whether onboarding is still open, the
// workspaces the user belongs to, and whether the session belongs to an
// operator. Subscriptions belong to workspaces and are read through the
// workspace billing endpoint.
func (h *AuthHandler) Account(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	memberships, err := h.workspaceService.ListForUser(user.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, accountResponse{
		User:               NewUser(user),
		OnboardingRequired: user.Name == "",
		Workspaces:         NewMemberships(visibleMemberships(r, memberships)),
		IsAdmin:            ctxkeys.IsAdmin(r.Context()),
		AvatarURL:          avatarURL(user),
	})
}
