package handler

import (
	"errors"
	"github.com/a-h/templ"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
	"github.com/axadrn/moin/hub/internal/validation"
)

type authHandler struct {
	authService      *service.AuthService
	workspaceService *service.WorkspaceService
	// githubApp signs people in; nil, or without an app, nobody signs in
	// with GitHub.
	githubApp *service.GitHubAppService
	passkeys  *service.PasskeyService
	cfg       *config.Config
}

func NewAuthHandler(authService *service.AuthService, workspaceService *service.WorkspaceService, githubApp *service.GitHubAppService, passkeys *service.PasskeyService, cfg *config.Config) *authHandler {
	return &authHandler{
		authService:      authService,
		workspaceService: workspaceService,
		githubApp:        githubApp,
		passkeys:         passkeys,
		cfg:              cfg,
	}
}

func (h *authHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.authService.EndSession(r); err != nil {
		slog.Error("failed to revoke session during logout", "error", err)
		if isAction(r) {
			answerFailed(w, r, "Not signed out", "Something went wrong. Please try again.")
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Sign out failed", "Please try again."))
		}
		return
	}
	h.authService.ClearSessionCookie(w)
	redirect(w, r, "/", http.StatusSeeOther)
}

// VerifyEmailChange takes the link sent to a new address. It changes the
// email and signs the user out everywhere; only a browser signed in as
// that user stays in, with the session it had. Anyone else goes to sign in.
func (h *authHandler) VerifyEmailChange(w http.ResponseWriter, r *http.Request) {
	user, signedIn, err := h.authService.ConfirmEmailChange(r, r.PathValue("token"))
	if user == nil {
		slog.Warn("email change verification failed", "error", err)
		render(w, r, pages.VerifyEmailError("It expired or was used already."))
		return
	}
	if err != nil {
		slog.Error("failed to end the sessions after an email change", "error", err, "user_id", user.ID)
	}
	slog.Info("email changed", "user_id", user.ID)
	if !signedIn {
		redirect(w, r, "/auth?email=changed", http.StatusSeeOther)
		return
	}
	render(w, r, pages.VerifyEmailSuccess())
}

// AuthPage is the way in: GitHub where the GitHub App is set up, else the
// recovery key from the terminal.
// After an email change from a link it says so.
func (h *authHandler) AuthPage(w http.ResponseWriter, r *http.Request) {
	note := ""
	if r.URL.Query().Get("email") == "changed" {
		note = "Your email is changed. Sign in again."
	}
	render(w, r, pages.Auth(note, ""))
}

// KeyPage takes a recovery key.
func (h *authHandler) KeyPage(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.AuthKey(""))
}

// SignInWithKey signs in with a recovery key, once. It ends every other
// session of the user, whose GitHub account the key disconnected and whose
// passkeys it deleted, and leads to the security settings to connect
// GitHub and add a passkey again.
func (h *authHandler) SignInWithKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.FormValue("key"))
	if key == "" {
		authFieldError(w, r, pages.AuthKeyForm("Enter the key."), "Not signed in", "key", "Enter the key.")
		return
	}
	user, err := h.authService.SignInWithRecoveryKey(key)
	if errors.Is(err, service.ErrRecoveryKeyInvalid) {
		message := "This key is wrong, used or expired."
		authFieldError(w, r, pages.AuthKeyForm(message), "Not signed in", "key", message)
		return
	}
	if err != nil {
		slog.Error("recovery key sign-in failed", "error", err)
		answerFailed(w, r, "Not signed in", "Something went wrong. Please try again.")
		return
	}
	if _, err := h.authService.StartFreshSession(w, r, user.ID); err != nil {
		slog.Error("failed to create session", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Not signed in", "Something went wrong. Please try again.")
		return
	}
	slog.Info("user signed in with a recovery key", "user_id", user.ID)
	members, err := h.workspaceService.ListForUser(user.ID)
	if user.Name == "" || err != nil || len(members) == 0 {
		h.redirectAfterAuth(w, r, user)
		return
	}
	redirect(w, r, members[0].Path()+"/settings/account?tab=security", http.StatusSeeOther)
}

func (h *authHandler) OnboardingPage(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if user != nil && user.Name != "" {
		h.redirectAfterAuth(w, r, user)
		return
	}
	_, invited := h.pendingInvitationForUser(w, r, user)
	form := pages.OnboardingForm{AskWorkspace: !invited}
	// After GitHub sign-in both fields come filled in, so it is one click.
	if name := githubName(r, user); name != "" {
		form.Name = name
		form.Workspace = service.DefaultWorkspaceName(name)
	}
	render(w, r, pages.AuthOnboarding(form))
}

func (h *authHandler) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if user == nil {
		redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}

	pendingInvitation, hasPendingInvitation := h.pendingInvitationForUser(w, r, user)
	form := pages.OnboardingForm{
		Name:         r.FormValue("name"),
		Workspace:    r.FormValue("workspace"),
		AskWorkspace: !hasPendingInvitation,
	}
	if validation.ValidateName(form.Name) != nil {
		onboardingFieldError(w, r, form, "name", "Enter your name, up to 100 characters.")
		return
	}
	workspaceName := ""
	if form.AskWorkspace {
		workspaceName = form.Workspace
		if validation.ValidateWorkspaceName(workspaceName) != nil {
			onboardingFieldError(w, r, form, "workspace", "Enter a name of 2 to 80 characters.")
			return
		}
	}

	workspace, err := h.authService.CompleteOnboarding(user.ID, form.Name, workspaceName, !hasPendingInvitation)
	if err != nil {
		slog.Error("onboarding failed", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Not saved", "Something went wrong. Please try again.")
		return
	}
	clearGitHubNameCookie(w, r)

	slog.Info("onboarding completed", "user_id", user.ID)
	if hasPendingInvitation {
		redirect(w, r, "/workspaces/invitations/"+pendingInvitation, http.StatusSeeOther)
		return
	}
	if approval, ok := pendingApproval(r); ok {
		redirect(w, r, approval, http.StatusSeeOther)
		return
	}
	redirect(w, r, workspace.Path()+"/overview", http.StatusSeeOther)
}

func onboardingFieldError(w http.ResponseWriter, r *http.Request, form pages.OnboardingForm, field, message string) {
	form.Field, form.Error = field, message
	authFieldError(w, r, pages.AuthOnboardingForm(form), "Not saved", field, message)
}

func (h *authHandler) redirectAfterAuth(w http.ResponseWriter, r *http.Request, user *model.User) {
	redirect(w, r, h.afterAuth(w, r, user), http.StatusSeeOther)
}

// afterAuth is where a sign-in leads: the invitation, or the approval of
// moin login or an AI agent, this browser opened before, else the user's
// first workspace.
func (h *authHandler) afterAuth(w http.ResponseWriter, r *http.Request, user *model.User) string {
	if invitation, ok := h.pendingInvitationForUser(w, r, user); ok {
		return "/workspaces/invitations/" + invitation
	}
	if approval, ok := pendingApproval(r); ok {
		return approval
	}
	members, err := h.workspaceService.ListForUser(user.ID)
	if err != nil || len(members) == 0 {
		return "/workspaces/new"
	}
	return members[0].Path() + "/overview"
}

func (h *authHandler) pendingInvitationForUser(w http.ResponseWriter, r *http.Request, user *model.User) (string, bool) {
	cookie, err := r.Cookie("pending_invitation")
	if err != nil || cookie.Value == "" {
		return "", false
	}
	invitation, err := h.workspaceService.WorkspaceInvitation(cookie.Value)
	if err != nil || user == nil || !strings.EqualFold(invitation.Email, user.Email) {
		clearPendingInvitationCookie(w, h.cfg)
		return "", false
	}
	return cookie.Value, true
}

// authFieldError answers a sign-in form with a wrong field: the field is
// marked and the message shows in its hint line, and what was typed stays
// (DESIGN.md, Feedback). Without htmx the form renders again with it.
func authFieldError(w http.ResponseWriter, r *http.Request, form templ.Component, title, field, message string) {
	if isAction(r) {
		fieldError(w, title, field, message)
		return
	}
	render(w, r, form)
}
