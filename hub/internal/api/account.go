package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/validation"
)

type deleteAccountRequest struct {
	ConfirmEmail string `json:"confirm_email"`
}

// UpdateName changes the display name and returns the user.
func (h *AccountHandler) UpdateName(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	var request nameRequest
	if !Decode(w, r, &request) {
		return
	}
	err := validation.ValidateName(request.Name)
	if err != nil {
		ValidationError(w, map[string]string{"name": FieldMessage(err)})
		return
	}

	updatedUser, err := h.userService.UpdateName(user.ID, request.Name)
	if err != nil {
		slog.Error("failed to update name", "error", err, "user_id", user.ID)
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, UserResponse{User: NewUser(updatedUser)})
}

// EmailAlerts is the body and the answer of PUT /api/v1/account/email-alerts.
type EmailAlerts struct {
	On bool `json:"on"`
}

// SetEmailAlerts turns the email alerts of the signed-in user on or off.
func (h *AccountHandler) SetEmailAlerts(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	var body struct {
		On *bool `json:"on"`
	}
	if !Decode(w, r, &body) {
		return
	}
	if body.On == nil {
		ValidationError(w, map[string]string{"on": "Say true or false."})
		return
	}
	if err := h.userService.SetEmailAlerts(user.ID, *body.On); err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, EmailAlerts{On: *body.On})
}

// ChangeEmail sends a verification link to the new address. The address
// changes once the link is verified.
func (h *AccountHandler) ChangeEmail(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	var request emailRequest
	if !Decode(w, r, &request) {
		return
	}
	email := strings.TrimSpace(strings.ToLower(request.Email))
	err := validation.ValidateEmail(email)
	if err != nil {
		ValidationError(w, map[string]string{"email": FieldMessage(err)})
		return
	}
	if email == user.Email {
		ValidationError(w, map[string]string{"email": "Email address is already set to this value."})
		return
	}

	err = h.authService.RequestEmailChange(user.ID, email)
	if err != nil {
		slog.Warn("email change request failed", "error", err, "user_id", user.ID)
		ServiceError(w, err)
		return
	}

	slog.Info("email change requested", "user_id", user.ID)
	w.WriteHeader(http.StatusAccepted)
}

// DeleteAccount deletes the account after the caller confirms its email
// address. Every session of the account ends with it.
func (h *AccountHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	var request deleteAccountRequest
	if !Decode(w, r, &request) {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(request.ConfirmEmail), user.Email) {
		ValidationError(w, map[string]string{"confirm_email": "Type your email address to confirm."})
		return
	}

	err := h.userService.DeleteAccount(user.ID)
	if err != nil {
		accountDeletionError(w, user.ID, err)
		return
	}

	slog.Info("account deleted", "user_id", user.ID)
	h.authService.ClearSessionCookie(w)
	NoContent(w)
}
