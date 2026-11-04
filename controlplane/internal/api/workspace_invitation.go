package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// WorkspaceInvitationHandler shows and accepts workspace invitations by
// the token from the invitation email.
type WorkspaceInvitationHandler struct {
	workspaces *service.WorkspaceService
}

func NewWorkspaceInvitationHandler(workspaces *service.WorkspaceService) *WorkspaceInvitationHandler {
	return &WorkspaceInvitationHandler{workspaces: workspaces}
}

// InvitationPreview is what the holder of an invitation token may see
// before accepting it.
type InvitationPreview struct {
	WorkspaceName string    `json:"workspace_name"`
	InviterName   string    `json:"inviter_name"`
	Role          string    `json:"role"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// Show returns the workspace name, the inviter, the role and the expiry of
// an open invitation. It needs no session.
func (h *WorkspaceInvitationHandler) Show(w http.ResponseWriter, r *http.Request) {
	invitation, err := h.workspaces.WorkspaceInvitation(r.PathValue("token"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, InvitationPreview{
		WorkspaceName: invitation.WorkspaceName,
		InviterName:   invitation.InviterName,
		Role:          invitation.Role,
		ExpiresAt:     invitation.ExpiresAt.UTC(),
	})
}

// Accept adds the signed-in user to the workspace of the invitation and
// returns the membership.
func (h *WorkspaceInvitationHandler) Accept(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	workspace, err := h.workspaces.AcceptInvitation(r.PathValue("token"), user, source(r))
	if err != nil {
		ServiceError(w, err)
		return
	}
	_, member, err := h.workspaces.BySlugForUser(workspace.Slug, user.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, Membership{Workspace: NewWorkspace(workspace), Role: member.Role})
}
