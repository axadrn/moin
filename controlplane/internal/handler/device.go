package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// pendingDeviceCookie keeps the code of `moin login` while the user signs
// in, so every way to sign in comes back to the approval.
const pendingDeviceCookie = "pending_device"

// DeviceHandler is the page where `moin login` is approved in the browser.
type DeviceHandler struct {
	logins     *service.DeviceLoginService
	workspaces *service.WorkspaceService
	cfg        *config.Config
}

func NewDeviceHandler(logins *service.DeviceLoginService, workspaces *service.WorkspaceService, cfg *config.Config) *DeviceHandler {
	return &DeviceHandler{logins: logins, workspaces: workspaces, cfg: cfg}
}

// Show asks for the code, or with ?code= shows the login to approve. Without
// a session it sends the user to sign in first and back here after.
func (h *DeviceHandler) Show(w http.ResponseWriter, r *http.Request) {
	code := service.FormatUserCode(r.URL.Query().Get("code"))
	user := ctxkeys.User(r.Context())
	if user == nil || user.Name == "" {
		h.setPending(w, code)
		next := "/auth"
		if user != nil {
			next = "/auth/onboarding"
		}
		redirect(w, r, next, http.StatusSeeOther)
		return
	}
	clearPendingDevice(w, h.cfg)
	if code == "" {
		render(w, r, pages.Device(pages.DeviceView{}))
		return
	}
	login, err := h.logins.Pending(code)
	if err != nil {
		h.codeError(w, r, code, err)
		return
	}
	view, err := h.view(r, code, "")
	if err != nil {
		slog.Error("failed to list workspaces for device login", "error", err, "user_id", user.ID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Login unavailable", "Your workspaces could not be loaded. Please try again."))
		return
	}
	view.Login = login
	render(w, r, pages.Device(view))
}

// Approve lets the computer in. Its next poll gets the token.
func (h *DeviceHandler) Approve(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	code := service.FormatUserCode(r.FormValue("code"))
	workspaceID := r.FormValue("workspace_id")
	login, err := h.logins.Approve(user.ID, code, workspaceID)
	if errors.Is(err, service.ErrAPITokenWorkspace) {
		view, viewErr := h.view(r, code, workspaceID)
		pending, pendingErr := h.logins.Pending(code)
		if viewErr == nil && pendingErr == nil {
			view.Login = pending
			view.WorkspaceError = "Choose one of your workspaces."
			w.WriteHeader(http.StatusUnprocessableEntity)
			render(w, r, pages.Device(view))
			return
		}
	}
	if err != nil {
		h.codeError(w, r, code, err)
		return
	}
	slog.Info("device login approved", "login_id", login.ID, "user_id", user.ID)
	render(w, r, pages.DeviceDone(login, true))
}

// Deny turns the login down. Its next poll hears so and stops.
func (h *DeviceHandler) Deny(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	code := service.FormatUserCode(r.FormValue("code"))
	login, err := h.logins.Deny(user.ID, code)
	if err != nil {
		h.codeError(w, r, code, err)
		return
	}
	slog.Info("device login denied", "login_id", login.ID, "user_id", user.ID)
	render(w, r, pages.DeviceDone(login, false))
}

// codeError shows the code form again with why the code was not taken.
func (h *DeviceHandler) codeError(w http.ResponseWriter, r *http.Request, code string, err error) {
	message := "No login waits for this code. It may have expired: run moin login again."
	if !errors.Is(err, service.ErrUserCodeNotFound) {
		slog.Error("device login failed", "error", err)
		message = "Something went wrong. Please try again."
	}
	w.WriteHeader(http.StatusUnprocessableEntity)
	render(w, r, pages.Device(pages.DeviceView{Code: code, CodeError: message}))
}

// view lists the workspaces the computer can be let into, the chosen one or
// the first selected.
func (h *DeviceHandler) view(r *http.Request, code, workspaceID string) (pages.DeviceView, error) {
	user := ctxkeys.User(r.Context())
	memberships, err := h.workspaces.ListForUser(user.ID)
	if err != nil {
		return pages.DeviceView{}, err
	}
	view := pages.DeviceView{Code: code, WorkspaceID: workspaceID}
	for _, membership := range memberships {
		view.Workspaces = append(view.Workspaces, pages.APITokenWorkspace{ID: membership.ID, Name: membership.Name})
	}
	if view.WorkspaceID == "" && len(view.Workspaces) > 0 {
		view.WorkspaceID = view.Workspaces[0].ID
	}
	return view, nil
}

func (h *DeviceHandler) setPending(w http.ResponseWriter, code string) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingDeviceCookie,
		Value:    service.NormalizeUserCode(code),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.IsProduction(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(service.DeviceLoginLifetime.Seconds()),
	})
}

func clearPendingDevice(w http.ResponseWriter, cfg *config.Config) {
	secure := cfg != nil && cfg.IsProduction()
	http.SetCookie(w, &http.Cookie{Name: pendingDeviceCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// pendingDevice is where to go after signing in when `moin login` sent the
// user: the approval page, with the code when there is one.
func pendingDevice(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(pendingDeviceCookie)
	if err != nil {
		return "", false
	}
	code := service.NormalizeUserCode(cookie.Value)
	if code == "" {
		return "/device", true
	}
	return "/device?code=" + url.QueryEscape(service.FormatUserCode(code)), true
}
