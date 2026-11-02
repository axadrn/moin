package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// APITokensHandler creates and revokes personal API tokens from the account
// settings.
type APITokensHandler struct {
	tokens *service.APITokenService
}

func NewAPITokensHandler(tokens *service.APITokenService) *APITokensHandler {
	return &APITokensHandler{tokens: tokens}
}

// Create stores a token and answers with the fresh dialog, the list and the
// one-time reveal. Invalid input re-renders the form inside the open dialog.
func (h *APITokensHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	input, form := parseAPITokenForm(r)
	if len(form.Errors) == 0 {
		input.Source = service.RequestSource(r.Context())
		token, raw, err := h.tokens.Create(user.ID, input)
		if err == nil {
			slog.Info("api token created", "token_id", token.ID, "user_id", user.ID)
			view, err := apiTokensView(r, h.tokens, newAPITokenForm(r))
			if err != nil {
				slog.Error("failed to list api tokens", "error", err, "user_id", user.ID)
				toastDone(w, r, "Token created", "Reload the page to see it in the list.")
				render(w, r, pages.APITokenReveal(raw))
				return
			}
			render(w, r, pages.APITokenCreateDialog(view))
			render(w, r, pages.APITokenList(view))
			render(w, r, pages.APITokenReveal(raw))
			return
		}
		field, message, ok := apiTokenFormError(err)
		if !ok {
			slog.Error("failed to create api token", "error", err, "user_id", user.ID)
			toastFailed(w, r, "Token not created", "Something went wrong. Please try again.")
			return
		}
		if field == "form" {
			toastFailed(w, r, "Token not created", message)
			return
		}
		form.Errors[field] = message
	}
	view, err := apiTokensView(r, h.tokens, form)
	if err != nil {
		slog.Error("failed to list api tokens", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Token not created", "Something went wrong. Please try again.")
		return
	}
	render(w, r, pages.APITokenCreateForm(view, true))
}

// Revoke deletes a token and answers with the updated list.
func (h *APITokensHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	tokenID := r.PathValue("id")
	err := h.tokens.Revoke(user.ID, tokenID, service.RequestSource(r.Context()))
	switch {
	case errors.Is(err, repository.ErrAPITokenNotFound):
		toastError(w, r, "Token not found", "That token was already revoked.")
	case err != nil:
		slog.Error("failed to revoke api token", "error", err, "user_id", user.ID, "token_id", tokenID)
		toastFailed(w, r, "Token not revoked", "Something went wrong. Please try again.")
		return
	default:
		slog.Info("api token revoked", "token_id", tokenID, "user_id", user.ID)
		toastDone(w, r, "Token revoked", "Requests with it no longer work.")
	}
	view, err := apiTokensView(r, h.tokens, newAPITokenForm(r))
	if err != nil {
		slog.Error("failed to list api tokens", "error", err, "user_id", user.ID)
		return
	}
	render(w, r, pages.APITokenList(view))
}
