package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// OAuthHandler is the authorization endpoint of OAuth, where a person lets
// an AI agent in: the approval page of `moin login`, with the workspace,
// the rights and the passkey, and where it goes back to.
type OAuthHandler struct {
	oauth      *service.OAuthService
	workspaces *service.WorkspaceService
	passkeys   *service.PasskeyService
	auth       *service.AuthService
	cfg        *config.Config
}

func NewOAuthHandler(oauth *service.OAuthService, workspaces *service.WorkspaceService, passkeys *service.PasskeyService, auth *service.AuthService, cfg *config.Config) *OAuthHandler {
	return &OAuthHandler{oauth: oauth, workspaces: workspaces, passkeys: passkeys, auth: auth, cfg: cfg}
}

// authorizePath is the authorization endpoint.
const authorizePath = "/oauth/authorize"

// Authorize checks the client's request and shows the approval. Without a
// session it sends the person to sign in first and back here after.
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	authorization, ok := h.check(w, r, service.ParseAuthorizationRequest(r.URL.Query()))
	if !ok {
		return
	}
	user := ctxkeys.User(r.Context())
	if user == nil || user.Name == "" {
		setPendingApproval(w, h.cfg, authorizePath+"?"+authorization.Request.Values().Encode())
		next := "/auth"
		if user != nil {
			next = "/auth/onboarding"
		}
		redirect(w, r, next, http.StatusSeeOther)
		return
	}
	clearPendingApproval(w, h.cfg)
	view, err := h.view(r, authorization, "", model.DefaultRights)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	render(w, r, pages.OAuth(view))
}

// Approve makes the connection, with the person's passkey or a fresh
// sign-in, and sends the person back to the client with the code.
func (h *OAuthHandler) Approve(w http.ResponseWriter, r *http.Request) {
	form := posted(r)
	authorization, ok := h.check(w, r, service.ParseAuthorizationRequest(form))
	if !ok {
		return
	}
	workspaceID := form.Get("workspace_id")
	rights, ok := model.ParseRights(form["rights"]...)
	if !ok {
		h.again(w, r, authorization, workspaceID, model.DefaultRights, "Choose from the rights shown.", false)
		return
	}
	target, connection, err := h.oauth.Approve(service.OAuthApproval{
		Authorization: authorization,
		WorkspaceID:   workspaceID,
		Rights:        rights,
		Presence: service.Presence{
			Session:  ctxkeys.Session(r.Context()),
			Ceremony: takePasskeyCeremony(w, r),
			Answer:   []byte(form.Get("passkey")),
		},
		Source: service.RequestSource(r.Context()),
	})
	if message, workspace, ok := approveError(err); ok {
		h.again(w, r, authorization, workspaceID, rights, message, workspace)
		return
	}
	if err != nil {
		h.failed(w, r, err)
		return
	}
	slog.Info("oauth connection approved", "connection_id", connection.ID, "client_id", authorization.Client.ID)
	sendBack(w, r, authorization, target, true)
}

// Deny sends the person back to the client, which hears access_denied.
func (h *OAuthHandler) Deny(w http.ResponseWriter, r *http.Request) {
	authorization, ok := h.check(w, r, service.ParseAuthorizationRequest(posted(r)))
	if !ok {
		return
	}
	sendBack(w, r, authorization, h.oauth.Deny(authorization), false)
}

// Reauth signs the person out and sends them to sign in again and back
// here after: an account without a passkey allows only with a sign-in less
// than five minutes old.
func (h *OAuthHandler) Reauth(w http.ResponseWriter, r *http.Request) {
	authorization, ok := h.check(w, r, service.ParseAuthorizationRequest(posted(r)))
	if !ok {
		return
	}
	setPendingApproval(w, h.cfg, authorizePath+"?"+authorization.Request.Values().Encode())
	if err := h.auth.EndSession(r); err != nil {
		h.failed(w, r, err)
		return
	}
	h.auth.ClearSessionCookie(w)
	redirect(w, r, "/auth", http.StatusSeeOther)
}

// posted is the posted form. One that cannot be read is empty, which check
// refuses.
func posted(r *http.Request) url.Values {
	if err := r.ParseForm(); err != nil {
		return url.Values{}
	}
	return r.PostForm
}

// check checks the client's request. When the client or its redirect URI
// is wrong, the page says so and sends nobody anywhere. Any other mistake
// goes back to the client, as OAuth asks.
func (h *OAuthHandler) check(w http.ResponseWriter, r *http.Request, request service.AuthorizationRequest) (*service.Authorization, bool) {
	authorization, err := h.oauth.Check(request)
	var refusal *service.OAuthError
	if errors.As(err, &refusal) {
		sendBack(w, r, authorization, h.oauth.Refuse(authorization, refusal), false)
		return nil, false
	}
	if err != nil {
		h.failed(w, r, err)
		return nil, false
	}
	return authorization, true
}

// again shows the approval again with what was wrong.
func (h *OAuthHandler) again(w http.ResponseWriter, r *http.Request, authorization *service.Authorization, workspaceID string, rights model.Rights, message string, workspace bool) {
	view, err := h.view(r, authorization, workspaceID, rights)
	if err != nil {
		h.failed(w, r, err)
		return
	}
	if workspace {
		view.WorkspaceError = message
	} else {
		view.AllowError = message
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	render(w, r, pages.OAuth(view))
}

func (h *OAuthHandler) view(r *http.Request, authorization *service.Authorization, workspaceID string, rights model.Rights) (pages.OAuthView, error) {
	approval, err := approvalView(r, h.workspaces, h.passkeys, workspaceID, rights)
	if err != nil {
		return pages.OAuthView{}, err
	}
	return pages.OAuthView{
		ClientName: authorization.Client.ClientName,
		Host:       authorization.RedirectHost(),
		Local:      authorization.Local(),
		Request:    authorization.Request.Values(),
		Approval:   approval,
	}, nil
}

// failed says why the request cannot go on, without sending the person
// anywhere.
func (h *OAuthHandler) failed(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	var message string
	switch {
	case errors.Is(err, service.ErrOAuthClient):
		message = "This app is not registered with this hub. Add the hub to the app again."
	case errors.Is(err, service.ErrOAuthRedirect):
		message = "This app asked to send you to a place it did not register. Nothing got access."
	default:
		slog.Error("oauth authorization failed", "error", err)
		status = http.StatusInternalServerError
		message = "Something went wrong. Please try again."
	}
	w.WriteHeader(status)
	render(w, r, pages.OAuthFailed(message))
}

// sendBack sends the person to the client's redirect URI. The request to
// the page is a plain redirect. A form's answer is a page that goes on by
// itself: the browser would not follow a form's redirect to another site,
// which the page's form-action rules out.
func sendBack(w http.ResponseWriter, r *http.Request, authorization *service.Authorization, target string, approved bool) {
	if r.Method == http.MethodGet {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	// The page carries the code: no cache keeps it.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Refresh", "0; url="+target)
	render(w, r, pages.OAuthDone(authorization.RedirectHost(), target, approved))
}
