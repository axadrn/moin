package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/validation"
)

func (h *AccountHandler) ChangeEmail(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	if validation.ValidateEmail(email) != nil {
		fieldError(w, "Email not changed", "email", "Enter a valid email address.")
		return
	}
	if email == user.Email {
		answerDone(w, r, "Nothing changed", "This is your email already.")
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
		answerFailed(w, r, "Email not changed", "Something went wrong. Please try again.")
		return
	}
	slog.Info("email change requested", "user_id", user.ID)
	answerDone(w, r, "Confirm your new email", "We sent a link to "+email+". The change applies once you open it.")
}

func (h *AccountHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())

	confirmation := strings.TrimSpace(r.FormValue("delete_confirmation"))

	if confirmation != "DELETE" {
		answerFailed(w, r, "Account not deleted", "Type DELETE to confirm.")
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
