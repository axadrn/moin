package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// apiTokensView loads the API tokens card of the signed-in user.
func apiTokensView(r *http.Request, tokens *service.APITokenService, form pages.APITokenForm) (pages.APITokensView, error) {
	user := ctxkeys.User(r.Context())
	list, err := tokens.List(user.ID)
	if err != nil {
		return pages.APITokensView{}, err
	}
	view := pages.APITokensView{Tokens: list, Form: form, Now: time.Now()}
	names := make(map[string]string)
	for _, membership := range ctxkeys.Workspaces(r.Context()) {
		view.Workspaces = append(view.Workspaces, pages.APITokenWorkspace{ID: membership.ID, Name: membership.Name})
		names[membership.ID] = membership.Name
	}
	view.WorkspaceNames = make(map[string]string, len(list))
	for _, token := range list {
		name := names[token.WorkspaceID]
		if name == "" {
			name = "A workspace you left"
		}
		view.WorkspaceNames[token.ID] = name
	}
	return view, nil
}

// newAPITokenForm returns the empty create form, bound to the current workspace.
func newAPITokenForm(r *http.Request) pages.APITokenForm {
	return pages.NewAPITokenForm(ctxkeys.Workspace(r.Context()).ID)
}

// parseAPITokenForm reads the create form. The returned form keeps the
// submitted values and the errors of values that could not be read.
func parseAPITokenForm(r *http.Request) (service.CreateAPITokenInput, pages.APITokenForm) {
	form := pages.APITokenForm{
		Name:        r.FormValue("name"),
		Access:      r.FormValue("access"),
		Expiry:      r.FormValue("expiry"),
		WorkspaceID: r.FormValue("workspace_id"),
		Errors:      map[string]string{},
	}
	input := service.CreateAPITokenInput{
		Name:        form.Name,
		WorkspaceID: form.WorkspaceID,
	}
	switch form.Access {
	case pages.APITokenAccessRead:
		input.Scopes = []string{model.APITokenScopeRead}
	case pages.APITokenAccessWrite:
		input.Scopes = []string{model.APITokenScopeRead, model.APITokenScopeWrite}
	default:
		form.Errors["access"] = "Choose read only, or read and write."
	}
	if form.Expiry != pages.APITokenExpiryNever {
		days, err := strconv.Atoi(form.Expiry)
		if err != nil || days < 1 || days > service.MaxAPITokenExpiryDays {
			form.Errors["expiry"] = "Choose an expiration."
		}
		input.ExpiresInDays = days
	}
	return input, form
}

// apiTokenFormError turns a service error into a message for one form field.
// The bool is false for errors that are not about the input.
func apiTokenFormError(err error) (string, string, bool) {
	switch {
	case errors.Is(err, service.ErrAPITokenName):
		return "name", "Enter a name with at most 64 characters.", true
	case errors.Is(err, service.ErrAPITokenScope):
		return "access", "Choose read only, or read and write.", true
	case errors.Is(err, service.ErrAPITokenExpiry):
		return "expiry", "Choose an expiration.", true
	case errors.Is(err, service.ErrAPITokenLimit):
		return "form", "You have reached the limit of 50 API tokens. Revoke one first.", true
	case errors.Is(err, service.ErrAPITokenWorkspace):
		return "workspace_id", "Choose one of your workspaces.", true
	}
	return "", "", false
}
