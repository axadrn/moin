package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
	"github.com/axadrn/moin/internal/link"
)

// ConnectionsHandler is Settings, Connections: the connections of the
// signed-in person and the CI tokens of the workspace, each with a page
// where its rights change and it goes.
type ConnectionsHandler struct {
	connections *service.ConnectionService
	ciTokens    *service.CITokenService
	apps        *service.AppService
}

func NewConnectionsHandler(connections *service.ConnectionService, ciTokens *service.CITokenService, apps *service.AppService) *ConnectionsHandler {
	return &ConnectionsHandler{connections: connections, ciTokens: ciTokens, apps: apps}
}

// Page lists the connections and CI tokens, with the form for a new CI
// token.
func (h *ConnectionsHandler) Page(w http.ResponseWriter, r *http.Request) {
	view, err := h.view(r, pages.NewCITokenForm())
	if err != nil {
		slog.Error("failed to load connections", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Connections unavailable", "Your connections could not be loaded. Please try again."))
		return
	}
	render(w, r, pages.SettingsConnections(view))
}

func (h *ConnectionsHandler) view(r *http.Request, form pages.CITokenForm) (pages.ConnectionsView, error) {
	user := ctxkeys.User(r.Context())
	connections, err := h.connections.List(user.ID)
	if err != nil {
		return pages.ConnectionsView{}, err
	}
	tokens, err := h.ciTokens.List(ctxkeys.WorkspaceMember(r.Context()))
	if err != nil {
		return pages.ConnectionsView{}, err
	}
	apps, err := h.apps.List(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		return pages.ConnectionsView{}, err
	}
	view := pages.ConnectionsView{Connections: connections, CITokens: tokens, Apps: apps, Form: form, Now: time.Now(), WorkspaceNames: map[string]string{}}
	for _, membership := range ctxkeys.Workspaces(r.Context()) {
		view.WorkspaceNames[membership.ID] = membership.Name
	}
	return view, nil
}

// Connection is the page of one connection of the signed-in person.
func (h *ConnectionsHandler) Connection(w http.ResponseWriter, r *http.Request) {
	connection, ok := h.connection(w, r)
	if !ok {
		return
	}
	name := "A workspace you left"
	for _, membership := range ctxkeys.Workspaces(r.Context()) {
		if membership.ID == connection.WorkspaceID {
			name = membership.Name
		}
	}
	render(w, r, pages.SettingsConnection(pages.ConnectionView{Connection: *connection, WorkspaceName: name, Now: time.Now()}))
}

// connection finds the connection of the path, or answers not found.
func (h *ConnectionsHandler) connection(w http.ResponseWriter, r *http.Request) (*model.Connection, bool) {
	connections, err := h.connections.List(ctxkeys.User(r.Context()).ID)
	if err != nil {
		renderError(w, "list connections failed", err)
		return nil, false
	}
	for _, connection := range connections {
		if link.ID(connection.ID) == r.PathValue("id") {
			return &connection, true
		}
	}
	notFound(w, r, "Connection not found", "It was removed already.")
	return nil, false
}

// SetConnectionRight turns one right of a connection on or off, at once.
func (h *ConnectionsHandler) SetConnectionRight(w http.ResponseWriter, r *http.Request) {
	connection, ok := h.connection(w, r)
	if !ok {
		return
	}
	right, ok := pathRight(r)
	if !ok {
		notFound(w, r, "Right not found", "")
		return
	}
	path := ctxkeys.AppBasePath(r.Context()) + "/settings/connections/" + link.ID(connection.ID)
	rights := withRight(connection.Rights, right, r.FormValue("on") == "1")
	updated, err := h.connections.SetRights(connection.UserID, connection.ID, rights, service.RequestSource(r.Context()))
	if err != nil {
		answerError(w, r, "Right not changed", errorMessage(err))
		render(w, r, pages.RightSwitch(path, "connection-right", right, connection.Rights.Has(right)))
		return
	}
	render(w, r, pages.RightSwitch(path, "connection-right", right, updated.Rights.Has(right)))
}

// RemoveConnection ends a connection and every token of it, and goes back
// to the list.
func (h *ConnectionsHandler) RemoveConnection(w http.ResponseWriter, r *http.Request) {
	connection, ok := h.connection(w, r)
	if !ok {
		return
	}
	if err := h.connections.Remove(connection.UserID, connection.ID, service.RequestSource(r.Context())); err != nil {
		answerFailed(w, r, "Connection not removed", errorMessage(err))
		return
	}
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/settings/connections", http.StatusSeeOther)
}

// CreateCIToken makes a CI token in the workspace and shows it once, above
// the list. A wrong field shows the form again with what is wrong.
func (h *ConnectionsHandler) CreateCIToken(w http.ResponseWriter, r *http.Request) {
	input, form := parseCITokenForm(r)
	if len(form.Errors) == 0 {
		member := ctxkeys.WorkspaceMember(r.Context())
		actor := service.Actor{WorkspaceID: member.WorkspaceID, UserID: member.UserID, Source: service.RequestSource(r.Context())}
		_, raw, err := h.ciTokens.Create(actor, input)
		if err == nil {
			view, err := h.view(r, pages.NewCITokenForm())
			if err != nil {
				slog.Error("failed to list ci tokens", "error", err)
				render(w, r, pages.CITokenReveal(raw))
				return
			}
			render(w, r, pages.CITokenCreate(view))
			render(w, r, pages.CITokenList(view))
			render(w, r, pages.CITokenReveal(raw))
			return
		}
		field, message := ciTokenFormError(err)
		if field == "" {
			answerFailed(w, r, "CI token not created", message)
			return
		}
		form.Errors[field] = message
	}
	view, err := h.view(r, form)
	if err != nil {
		answerFailed(w, r, "CI token not created", "Something went wrong. Please try again.")
		return
	}
	render(w, r, pages.CITokenCreateForm(view, true))
}

// parseCITokenForm reads the form of a new CI token. The form keeps what
// was sent and the errors of what could not be read.
func parseCITokenForm(r *http.Request) (service.NewCIToken, pages.CITokenForm) {
	form := pages.CITokenForm{Name: r.FormValue("name"), App: r.FormValue("app"), Expiry: r.FormValue("expiry"), Errors: map[string]string{}}
	rights, ok := model.ParseRights(r.Form["rights"]...)
	if !ok {
		form.Errors["rights"] = "Choose from the rights shown."
	}
	form.Rights = rights
	input := service.NewCIToken{Name: form.Name, Rights: rights, App: form.App}
	if form.Expiry != pages.CITokenExpiryNever {
		days, err := strconv.Atoi(form.Expiry)
		if err != nil || days < 1 || days > service.MaxCITokenExpiryDays {
			form.Errors["expiry"] = "Choose an expiration."
		}
		input.ExpiresInDays = days
	}
	return input, form
}

// ciTokenFormError is the field an error is about and what it says; no
// field for errors that are not about one.
func ciTokenFormError(err error) (string, string) {
	switch {
	case errors.Is(err, service.ErrCITokenName):
		return "name", "Enter a name with at most 64 characters."
	case errors.Is(err, service.ErrCITokenExpiry):
		return "expiry", "Choose an expiration."
	case errors.Is(err, repository.ErrAppNotFound):
		return "app", "Choose one of the apps."
	}
	return "", errorMessage(err)
}

// CIToken is the page of one CI token of the workspace.
func (h *ConnectionsHandler) CIToken(w http.ResponseWriter, r *http.Request) {
	token, ok := h.ciToken(w, r)
	if !ok {
		return
	}
	render(w, r, pages.SettingsCIToken(pages.CITokenView{Token: *token, Mine: token.UserID == ctxkeys.User(r.Context()).ID, Now: time.Now()}))
}

// ciToken finds the CI token of the path among those the person sees, or
// answers not found.
func (h *ConnectionsHandler) ciToken(w http.ResponseWriter, r *http.Request) (*model.CIToken, bool) {
	tokens, err := h.ciTokens.List(ctxkeys.WorkspaceMember(r.Context()))
	if err != nil {
		renderError(w, "list ci tokens failed", err)
		return nil, false
	}
	for _, token := range tokens {
		if link.ID(token.ID) == r.PathValue("id") {
			return &token, true
		}
	}
	notFound(w, r, "CI token not found", "It was removed already.")
	return nil, false
}

// SetCITokenRight turns one right of a CI token on or off, at once.
func (h *ConnectionsHandler) SetCITokenRight(w http.ResponseWriter, r *http.Request) {
	token, ok := h.ciToken(w, r)
	if !ok {
		return
	}
	right, ok := pathRight(r)
	if !ok {
		notFound(w, r, "Right not found", "")
		return
	}
	member := ctxkeys.WorkspaceMember(r.Context())
	actor := service.Actor{WorkspaceID: member.WorkspaceID, UserID: member.UserID, Source: service.RequestSource(r.Context())}
	path := ctxkeys.AppBasePath(r.Context()) + "/settings/ci-tokens/" + link.ID(token.ID)
	updated, err := h.ciTokens.SetRights(actor, token.ID, withRight(token.Rights, right, r.FormValue("on") == "1"))
	if err != nil {
		answerError(w, r, "Right not changed", errorMessage(err))
		render(w, r, pages.RightSwitch(path, "ci-token-right", right, token.Rights.Has(right)))
		return
	}
	render(w, r, pages.RightSwitch(path, "ci-token-right", right, updated.Rights.Has(right)))
}

// RemoveCIToken deletes a CI token and goes back to the list.
func (h *ConnectionsHandler) RemoveCIToken(w http.ResponseWriter, r *http.Request) {
	token, ok := h.ciToken(w, r)
	if !ok {
		return
	}
	if err := h.ciTokens.Remove(ctxkeys.WorkspaceMember(r.Context()), token.ID, service.RequestSource(r.Context())); err != nil {
		answerFailed(w, r, "CI token not removed", errorMessage(err))
		return
	}
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/settings/connections", http.StatusSeeOther)
}

// pathRight is the right named in the path: deploy, env, delete or
// servers. Read is always on.
func pathRight(r *http.Request) (model.Rights, bool) {
	for _, right := range model.AllRights[1:] {
		if right.Name() == r.PathValue("right") {
			return right, true
		}
	}
	return 0, false
}

// withRight is rights with right turned on or off.
func withRight(rights, right model.Rights, on bool) model.Rights {
	if on {
		return rights | right
	}
	return rights &^ right
}

// notFound answers a page or an action that names nothing there.
func notFound(w http.ResponseWriter, r *http.Request, title, description string) {
	if isAction(r) {
		answerFailed(w, r, title, description)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.ErrorPage(http.StatusNotFound, title, description))
}
