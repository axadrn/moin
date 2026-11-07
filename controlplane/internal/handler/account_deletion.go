package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/service"
)

func accountDeletionError(w http.ResponseWriter, r *http.Request, userID string, err error) {
	if errors.Is(err, service.ErrSoleWorkspaceOwner) {
		slog.Warn("account deletion failed: sole workspace owner", "error", err, "user_id", userID)
		answerFailed(w, r, "Account not deleted", "Transfer ownership or delete the workspaces where you are the only owner first.")
		return
	}
	slog.Error("account deletion failed", "error", err, "user_id", userID)
	answerFailed(w, r, "Account not deleted", "Something went wrong. Please try again.")
}
