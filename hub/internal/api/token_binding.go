package api

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

// visibleMemberships keeps only the workspace a connection or CI token is
// bound to. Session callers see every membership.
func visibleMemberships(r *http.Request, memberships []model.WorkspaceMembership) []model.WorkspaceMembership {
	bearer := ctxkeys.Bearer(r.Context())
	if bearer == nil {
		return memberships
	}
	visible := make([]model.WorkspaceMembership, 0, 1)
	for _, membership := range memberships {
		if membership.ID == bearer.WorkspaceID {
			visible = append(visible, membership)
		}
	}
	return visible
}
