package handler

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// pendingApprovalCookie keeps the approval page, of `moin login` or an AI
// agent, while the person signs in, so every way to sign in comes back to
// it.
const pendingApprovalCookie = "pending_approval"

// DeviceHandler is the page where `moin login` is approved in the browser:
// which workspace, which rights, and the person's passkey or a fresh
// confirmation to allow it.
type DeviceHandler struct {
	logins     *service.DeviceLoginService
	workspaces *service.WorkspaceService
	passkeys   *service.PasskeyService
	cfg        *config.Config
}

func NewDeviceHandler(logins *service.DeviceLoginService, workspaces *service.WorkspaceService, passkeys *service.PasskeyService, cfg *config.Config) *DeviceHandler {
	return &DeviceHandler{logins: logins, workspaces: workspaces, passkeys: passkeys, cfg: cfg}
}

// Show asks for the code, or with ?code= shows the login to approve. Without
// a session it sends the user to sign in first and back here after.
func (h *DeviceHandler) Show(w http.ResponseWriter, r *http.Request) {
	code := service.FormatUserCode(r.URL.Query().Get("code"))
	user := ctxkeys.User(r.Context())
	if user == nil || user.Name == "" {
		setPendingApproval(w, h.cfg, devicePath(code))
		next := "/auth"
		if user != nil {
			next = "/auth/onboarding"
		}
		redirect(w, r, next, http.StatusSeeOther)
		return
	}
	clearPendingApproval(w, h.cfg)
	if code == "" {
		render(w, r, pages.Device(pages.DeviceView{}))
		return
	}
	view, err := h.view(r, code, "", model.DefaultRights)
	if err != nil {
		h.codeError(w, r, code, err)
		return
	}
	render(w, r, pages.Device(view))
}

// PasskeyOptions starts the passkey ceremony that allows a login.
func (h *DeviceHandler) PasskeyOptions(w http.ResponseWriter, r *http.Request) {
	options, ceremony, err := h.passkeys.BeginConfirm(ctxkeys.Session(r.Context()))
	if err != nil {
		passkeyFailed(w, err)
		return
	}
	setPasskeyCeremony(w, r, ceremony)
	api.JSON(w, http.StatusOK, options)
}

// Approve makes the connection, with the person's passkey or a fresh
// confirmation. The computer's next poll gets its tokens.
func (h *DeviceHandler) Approve(w http.ResponseWriter, r *http.Request) {
	code := service.FormatUserCode(r.FormValue("code"))
	workspaceID := r.FormValue("workspace_id")
	rights, ok := model.ParseRights(r.Form["rights"]...)
	if !ok {
		view, err := h.view(r, code, workspaceID, model.DefaultRights)
		if err != nil {
			h.codeError(w, r, code, err)
			return
		}
		view.AllowError = "Choose from the rights shown."
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.Device(view))
		return
	}
	login, connection, err := h.logins.Approve(service.Approval{
		UserCode:    code,
		WorkspaceID: workspaceID,
		Rights:      rights,
		Presence: service.Presence{
			Session:  ctxkeys.Session(r.Context()),
			Ceremony: takePasskeyCeremony(w, r),
			Answer:   []byte(r.FormValue("passkey")),
		},
		Source: service.RequestSource(r.Context()),
	})
	if message, workspace, ok := approveError(err); ok {
		view, viewErr := h.view(r, code, workspaceID, rights)
		if viewErr == nil {
			if workspace {
				view.WorkspaceError = message
			} else {
				view.AllowError = message
			}
			w.WriteHeader(http.StatusUnprocessableEntity)
			render(w, r, pages.Device(view))
			return
		}
		err = viewErr
	}
	if err != nil {
		h.codeError(w, r, code, err)
		return
	}
	slog.Info("device login approved", "login_id", login.ID, "connection_id", connection.ID)
	render(w, r, pages.DeviceDone(login, true))
}

// approveError is what the page says when Allow did not work, and whether
// it is about the workspace. ok is false for errors about the code.
func approveError(err error) (message string, workspace, ok bool) {
	switch {
	case errors.Is(err, service.ErrConnectionWorkspace):
		return "Choose one of your workspaces.", true, true
	case errors.Is(err, service.ErrPresencePasskey):
		return "Allow with your passkey.", false, true
	case errors.Is(err, service.ErrConfirm):
		return "Confirm it's you first.", false, true
	case errors.Is(err, service.ErrPasskeyRefused):
		slog.Warn("passkey refused", "error", err)
		return "This passkey did not work. Try again.", false, true
	case errors.Is(err, service.ErrPasskeyExpired):
		return "This took too long. Please try again.", false, true
	case errors.Is(err, service.ErrImpersonating):
		return "Impersonation is read-only.", false, true
	case errors.Is(err, service.ErrConnectionLimit):
		return "You have 50 connections. Remove one in Settings, Connections.", false, true
	}
	return "", false, false
}

// Deny turns the login down. Its next poll hears so and stops.
func (h *DeviceHandler) Deny(w http.ResponseWriter, r *http.Request) {
	code := service.FormatUserCode(r.FormValue("code"))
	login, err := h.logins.Deny(code)
	if err != nil {
		h.codeError(w, r, code, err)
		return
	}
	slog.Info("device login denied", "login_id", login.ID, "user_id", ctxkeys.User(r.Context()).ID)
	render(w, r, pages.DeviceDone(login, false))
}

// codeError shows the code form again with why the code was not taken.
func (h *DeviceHandler) codeError(w http.ResponseWriter, r *http.Request, code string, err error) {
	message := "No login waits for this code. It may have expired: run " + brand.Name + " login again."
	if !errors.Is(err, service.ErrUserCodeNotFound) {
		slog.Error("device login failed", "error", err)
		message = "Something went wrong. Please try again."
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	render(w, r, pages.Device(pages.DeviceView{Code: code, CodeError: message}))
}

// view is the approval of the pending login of code.
func (h *DeviceHandler) view(r *http.Request, code, workspaceID string, rights model.Rights) (pages.DeviceView, error) {
	login, err := h.logins.Pending(code)
	if err != nil {
		return pages.DeviceView{}, err
	}
	approval, err := approvalView(r, h.workspaces, h.passkeys, workspaceID, rights, devicePath(code))
	if err != nil {
		return pages.DeviceView{}, err
	}
	return pages.DeviceView{Code: code, Login: login, Approval: approval}, nil
}

// approvalView is how the approval page asks this person, for `moin login`
// and an AI agent alike: the workspaces the client can go into, the chosen
// one or the first, the rights, and how Allow works for them. next is the
// page itself, to come back to after confirming.
func approvalView(r *http.Request, workspaces *service.WorkspaceService, passkeys *service.PasskeyService, workspaceID string, rights model.Rights, next string) (pages.Approval, error) {
	user := ctxkeys.User(r.Context())
	memberships, err := workspaces.ListForUser(user.ID)
	if err != nil {
		return pages.Approval{}, err
	}
	passkey, err := passkeys.NeedsPasskey(user.ID)
	if err != nil {
		return pages.Approval{}, err
	}
	view := pages.Approval{WorkspaceID: workspaceID, Rights: rights, Passkey: passkey, Passkeys: passkeys.On(), Next: next}
	view.Confirmed = passkeys.Present(ctxkeys.Session(r.Context())) == nil
	for _, membership := range memberships {
		view.Workspaces = append(view.Workspaces, pages.WorkspaceChoice{ID: membership.ID, Name: membership.Name})
	}
	if view.WorkspaceID == "" && len(view.Workspaces) > 0 {
		view.WorkspaceID = view.Workspaces[0].ID
	}
	return view, nil
}

// devicePath is the approval page of a code of `moin login`.
func devicePath(code string) string {
	code = service.FormatUserCode(code)
	if code == "" {
		return "/device"
	}
	return "/device?code=" + url.QueryEscape(code)
}

// setPendingApproval remembers path, an approval page, while the person
// signs in.
func setPendingApproval(w http.ResponseWriter, cfg *config.Config, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingApprovalCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(path)),
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg != nil && cfg.IsProduction(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(service.DeviceLoginLifetime.Seconds()),
	})
}

func clearPendingApproval(w http.ResponseWriter, cfg *config.Config) {
	secure := cfg != nil && cfg.IsProduction()
	http.SetCookie(w, &http.Cookie{Name: pendingApprovalCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// pendingApproval is where to go after signing in when `moin login` or an
// AI agent sent the person: the approval page. Only the two approval pages
// of this hub count, so the cookie never leads elsewhere.
func pendingApproval(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(pendingApprovalCookie)
	if err != nil {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "", false
	}
	target, err := url.Parse(string(raw))
	if err != nil || target.Scheme != "" || target.Host != "" || target.Opaque != "" || (target.Path != "/device" && target.Path != "/oauth/authorize") {
		return "", false
	}
	return target.String(), true
}
