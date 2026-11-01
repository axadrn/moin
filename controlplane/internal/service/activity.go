package service

import (
	"encoding/json"
	"log/slog"

	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/google/uuid"
)

// record writes what an actor did to the activity of the workspace. A failed
// write is logged and never fails the action itself.
func (s *ServerService) record(actor Actor, action, targetType, target string, details map[string]string) {
	if s.audit == nil || actor.WorkspaceID == "" {
		return
	}
	if details == nil {
		details = map[string]string{}
	}
	if actor.Via != "" {
		details["via"] = actor.Via
	}
	metadata, _ := json.Marshal(details)
	event := &model.WorkspaceAuditEvent{
		ID:          uuid.New().String(),
		WorkspaceID: actor.WorkspaceID,
		Action:      action,
		TargetType:  targetType,
		TargetID:    target,
		Metadata:    string(metadata),
		CreatedAt:   s.now().UTC(),
	}
	if actor.UserID != "" {
		event.ActorUserID = &actor.UserID
	}
	err := s.audit.Create(event)
	if err != nil {
		slog.Error("record activity failed", "workspace_id", actor.WorkspaceID, "action", action, "error", err)
	}
}
