package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
	"github.com/axadrn/moin/controlplane/internal/validation"
)

func (h *AccountHandler) ChangeEmail(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if validation.ValidateEmail(email) != nil {
		fieldError(w, "Email not changed", "email", "Enter a valid email address.")
		return
	}
	if email == user.Email {
		toastDone(w, r, "Nothing changed", "This is your email already.")
		return
	}
	err := h.authService.RequestEmailChange(user.ID, email)
	if errors.Is(err, service.ErrEmailAlreadyExists) {
		fieldError(w, "Email not changed", "email", "Another account uses this email.")
		return
	}
	if errors.Is(err, service.ErrInvalidEmail) {
		fieldError(w, "Email not changed", "email", "Enter a valid email address.")
		return
	}
	if err != nil {
		slog.Warn("email change request failed", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Email not changed", "Something went wrong. Please try again.")
		return
	}
	slog.Info("email change requested", "user_id", user.ID)
	toastDone(w, r, "Confirm your new email", "We sent a link to "+email+". The change applies once you open it.")
}

// passwordFields checks the fields of the password forms in their order and
// marks the first that is wrong. current is false for the form that sets a
// first password.
func passwordFields(w http.ResponseWriter, r *http.Request, title string, current bool) bool {
	if current && r.FormValue("current_password") == "" {
		fieldError(w, title, "current_password", "Enter your current password.")
		return false
	}
	if r.FormValue("new_password") == "" {
		fieldError(w, title, "new_password", "Enter a new password.")
		return false
	}
	if validation.ValidatePassword(r.FormValue("new_password")) != nil {
		fieldError(w, title, "new_password", passwordProblem(r.FormValue("new_password")))
		return false
	}
	if r.FormValue("confirm_password") != r.FormValue("new_password") {
		fieldError(w, title, "confirm_password", "The passwords do not match.")
		return false
	}
	return true
}

// passwordProblem says in a few words why ValidatePassword refused
// password, short enough for the hint line under the field.
func passwordProblem(password string) string {
	switch {
	case len(password) < 12:
		return "Use at least 12 characters."
	case len(password) > 72:
		return "Use at most 72 characters."
	}
	return "Too common. Choose another one."
}

func (h *AccountHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if !passwordFields(w, r, "Password not changed", true) {
		return
	}
	err := h.userService.UpdatePassword(user.ID, r.FormValue("current_password"), r.FormValue("new_password"))
	if errors.Is(err, service.ErrInvalidCurrentPassword) {
		fieldError(w, "Password not changed", "current_password", "This is not your current password.")
		return
	}
	if err != nil {
		slog.Warn("password update failed", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Password not changed", "Something went wrong. Please try again.")
		return
	}
	slog.Info("password updated", "user_id", user.ID)
	renderFragment(w, r, pages.SettingsPasswordSection(), "settings-password-form")
	h.passwordDone(w, r, user.ID, "Password changed")
}

func (h *AccountHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if !passwordFields(w, r, "Password not set", false) {
		return
	}
	err := h.authService.SetPassword(user.ID, r.FormValue("new_password"))
	if err != nil {
		slog.Warn("set password failed", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Password not set", "Something went wrong. Please try again.")
		return
	}
	slog.Info("password set", "user_id", user.ID)
	h.renderPasswordForm(w, r, user.ID)
	h.passwordDone(w, r, user.ID, "Password set")
}

func (h *AccountHandler) RemovePassword(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	err := h.authService.RemovePassword(user.ID)
	if err != nil {
		slog.Warn("remove password failed", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Password not removed", "Something went wrong. Please try again.")
		return
	}
	slog.Info("password removed", "user_id", user.ID)
	h.renderPasswordForm(w, r, user.ID)
	h.passwordDone(w, r, user.ID, "Password removed")
}

// renderPasswordForm answers the password form for the password as it is
// now: set one, or change and remove it.
func (h *AccountHandler) renderPasswordForm(w http.ResponseWriter, r *http.Request, userID string) {
	updated, err := h.userService.ByID(userID)
	if err != nil {
		slog.Error("failed to reload user after password change", "error", err, "user_id", userID)
		return
	}
	renderFragment(w, r.WithContext(ctxkeys.WithUser(r.Context(), updated)), pages.SettingsPasswordSection(), "settings-password-form")
}

// passwordDone signs out the other sessions after a change of the password
// and says so, or that they could not be signed out.
func (h *AccountHandler) passwordDone(w http.ResponseWriter, r *http.Request, userID, title string) {
	if err := h.revokeOtherAccess(r, userID); err != nil {
		slog.Error("failed to revoke other sessions after password change", "error", err, "user_id", userID)
		toastWarning(w, r, title, "Other sessions could not be signed out. Use Sign out everywhere under Sessions.")
		return
	}
	toastDone(w, r, title, "Your other sessions are signed out.")
}

func (h *AccountHandler) revokeOtherAccess(r *http.Request, userID string) error {
	session := ctxkeys.Session(r.Context())
	if session == nil {
		return nil
	}
	return h.authService.RevokeOtherAccess(userID, session.ID)
}

func (h *AccountHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())

	confirmation := strings.TrimSpace(r.FormValue("delete_confirmation"))

	if confirmation != "DELETE" {
		toastFailed(w, r, "Account not deleted", "Type DELETE to confirm.")
		return
	}

	err := h.userService.DeleteAccount(user.ID)
	if err != nil {
		accountDeletionError(w, r, user.ID, err)
		return
	}

	slog.Info("account deleted", "user_id", user.ID)
	h.authService.ClearSessionCookie(w)

	redirect(w, r, "/", http.StatusSeeOther)
}
