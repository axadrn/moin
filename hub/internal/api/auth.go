package api

import (
	"log/slog"
	"net/http"
)

type emailRequest struct {
	Email string `json:"email"`
}

type tokenRequest struct {
	Token string `json:"token"`
}

type nameRequest struct {
	Name string `json:"name"`
}

// Logout ends the cookie session and clears its cookie.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	err := h.authService.EndSession(r)
	if err != nil {
		slog.Error("failed to revoke session during logout", "error", err)
		ServiceError(w, err)
		return
	}
	h.authService.ClearSessionCookie(w)
	NoContent(w)
}

// VerifyEmailChange consumes an email change token, moves the pending
// address to the account and returns the user. Like the link on the web,
// it ends every session of the user but the caller's own, when the caller
// is signed in as that user.
func (h *AuthHandler) VerifyEmailChange(w http.ResponseWriter, r *http.Request) {
	var request tokenRequest
	if !Decode(w, r, &request) {
		return
	}

	user, _, err := h.authService.ConfirmEmailChange(r, request.Token)
	if user == nil {
		slog.Warn("email change verification failed", "error", err)
		invalidToken(w)
		return
	}
	if err != nil {
		slog.Error("failed to replace the sessions after an email change", "error", err, "user_id", user.ID)
		ServiceError(w, err)
		return
	}

	slog.Info("email changed", "user_id", user.ID)
	JSON(w, http.StatusOK, UserResponse{User: NewUser(user)})
}

func invalidToken(w http.ResponseWriter) {
	Error(w, http.StatusBadRequest, "invalid_token", "This link is invalid or has expired.")
}
