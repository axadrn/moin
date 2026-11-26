package handler

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
	"github.com/axadrn/moin/hub/internal/validation"
)

// githubStateCookie holds the state of a trip to GitHub, so only this
// browser can finish it, and the githubTrip.
const githubStateCookie = "oauth_state"

// githubTrip is what a trip to GitHub is for. Empty is a sign-in. With
// userID alone it connects the GitHub account to that user ("Connect
// GitHub" in the settings); with next too it confirms it is them and goes
// back there (ConfirmWithGitHub).
type githubTrip struct {
	userID string
	next   string
}

// cookie is the value of the state cookie: the state, the user and, to
// confirm, the way back, by dots. Neither the ID nor base64 has a dot.
func (t githubTrip) cookie(state string) string {
	value := state + "." + t.userID
	if t.next != "" {
		value += "." + base64.RawURLEncoding.EncodeToString([]byte(t.next))
	}
	return value
}

// readGitHubTrip is the state and the trip of a state cookie.
func readGitHubTrip(value string) (string, githubTrip) {
	state, rest, _ := strings.Cut(value, ".")
	userID, next, _ := strings.Cut(rest, ".")
	raw, err := base64.RawURLEncoding.DecodeString(next)
	if err != nil {
		return state, githubTrip{userID: userID}
	}
	return state, githubTrip{userID: userID, next: string(raw)}
}

// GitHubAuth sends a guest to GitHub to sign in.
func (h *authHandler) GitHubAuth(w http.ResponseWriter, r *http.Request) {
	h.startGitHub(w, r, githubTrip{}, http.StatusTemporaryRedirect)
}

// ConnectGitHub sends a signed-in user without a GitHub account to GitHub,
// to connect the account they will sign in with. It is a POST with the
// CSRF token, so no other site can start it.
func (h *authHandler) ConnectGitHub(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if session := ctxkeys.Session(r.Context()); session == nil || session.IsImpersonating() || user.GitHubID != nil {
		redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	h.startGitHub(w, r, githubTrip{userID: user.ID}, http.StatusSeeOther)
}

// startGitHub redirects to GitHub with status: a 303 after a POST, so the
// browser goes there with a GET.
func (h *authHandler) startGitHub(w http.ResponseWriter, r *http.Request, trip githubTrip, status int) {
	if !h.githubApp.SignsIn() {
		redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		panic("generate the GitHub state: " + err.Error())
	}
	state := base64.RawURLEncoding.EncodeToString(random)
	http.SetCookie(w, &http.Cookie{
		Name:     githubStateCookie,
		Value:    trip.cookie(state),
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.IsProduction(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	redirect(w, r, h.githubApp.GitHub().AuthorizeURL(state, h.githubCallbackURL()), status)
}

// githubCallbackURL is where GitHub sends people back, one of the app's
// callback URLs.
func (h *authHandler) githubCallbackURL() string {
	return strings.TrimRight(h.cfg.AppURL, "/") + "/auth/github/callback"
}

// GitHubCallback takes people back from GitHub: it signs them in,
// connects the GitHub account to the signed-in user who asked for it, or
// confirms it is them.
func (h *authHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(githubStateCookie)
	var expected string
	var trip githubTrip
	if err == nil {
		expected, trip = readGitHubTrip(cookie.Value)
	}
	if !h.githubApp.SignsIn() || state == "" || expected != state {
		slog.Warn("github sign-in state does not match", "error", err)
		// A sign-in that came back after another one finished, from a
		// second tab or the back button: this browser is signed in already.
		if ctxkeys.User(r.Context()) != nil {
			redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		render(w, r, pages.AuthGitHubFailed("This GitHub sign-in is no longer valid. Please try again."))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: githubStateCookie, Value: "", Path: "/", MaxAge: -1})

	account, err := h.githubApp.GitHub().Account(r.Context(), r.URL.Query().Get("code"), h.githubCallbackURL())
	if err != nil {
		slog.Error("github sign-in failed", "error", err)
		render(w, r, pages.AuthGitHubFailed("GitHub did not sign you in. Please try again."))
		return
	}
	if trip.next != "" {
		h.confirmGitHub(w, r, trip, account)
		return
	}
	if trip.userID != "" {
		h.connectGitHub(w, r, trip.userID, account)
		return
	}

	user, err := h.authService.SignInWithGitHub(account, pendingInvitation(r))
	switch {
	case errors.Is(err, service.ErrPrivate):
		render(w, r, pages.AuthPrivate())
		return
	case errors.Is(err, service.ErrGitHubEmailTaken):
		render(w, r, pages.AuthGitHubFailed("Your GitHub email belongs to another account. Sign in to it with its GitHub account, or ask your admin for a recovery key."))
		return
	case errors.Is(err, service.ErrGitHubNoEmail):
		render(w, r, pages.AuthGitHubFailed("GitHub shared no verified primary email. Verify your email on GitHub and try again."))
		return
	case err != nil:
		slog.Error("github sign-in failed", "error", err)
		render(w, r, pages.AuthGitHubFailed("Something went wrong. Please try again."))
		return
	}

	if _, err := h.authService.StartSession(w, r, user.ID); err != nil {
		slog.Error("failed to create session", "error", err, "user_id", user.ID)
		render(w, r, pages.AuthGitHubFailed("Something went wrong. Please try again."))
		return
	}
	slog.Info("user signed in with github", "user_id", user.ID)
	if user.Name == "" {
		setGitHubNameCookie(w, r, user.ID, githubDisplayName(account))
		redirect(w, r, "/auth/onboarding", http.StatusSeeOther)
		return
	}
	h.redirectAfterAuth(w, r, user)
}

// connectGitHub gives the signed-in user who asked for it the GitHub
// account, and takes them back to the security settings.
func (h *authHandler) connectGitHub(w http.ResponseWriter, r *http.Request, userID string, account service.GitHubAccount) {
	user := ctxkeys.User(r.Context())
	if user == nil || user.ID != userID {
		render(w, r, pages.AuthGitHubFailed("This GitHub sign-in is no longer valid. Please try again."))
		return
	}
	err := h.authService.ConnectGitHub(user.ID, account)
	if errors.Is(err, service.ErrGitHubTaken) {
		w.WriteHeader(http.StatusConflict)
		render(w, r, pages.ErrorPage(http.StatusConflict, "GitHub not connected", "This GitHub account is connected to another account."))
		return
	}
	if err != nil {
		renderError(w, "connect github failed", err)
		return
	}
	slog.Info("github connected", "user_id", user.ID)
	members, err := h.workspaceService.ListForUser(user.ID)
	if err != nil || len(members) == 0 {
		redirect(w, r, "/app", http.StatusSeeOther)
		return
	}
	redirect(w, r, members[0].Path()+"/settings/account?tab=security", http.StatusSeeOther)
}

// confirmGitHub notes that the signed-in user who asked for it confirmed
// it is them with their own GitHub account, and goes back. The session
// stays, and so do the others.
func (h *authHandler) confirmGitHub(w http.ResponseWriter, r *http.Request, trip githubTrip, account service.GitHubAccount) {
	user := ctxkeys.User(r.Context())
	if user == nil || user.ID != trip.userID {
		render(w, r, pages.AuthGitHubFailed("This GitHub sign-in is no longer valid. Please try again."))
		return
	}
	err := h.authService.ConfirmWithGitHub(ctxkeys.Session(r.Context()), account)
	if errors.Is(err, service.ErrGitHubOtherAccount) {
		w.WriteHeader(http.StatusForbidden)
		render(w, r, pages.ErrorPage(http.StatusForbidden, "Not confirmed", "This is not the GitHub account you sign in with. Sign in to GitHub with yours and try again."))
		return
	}
	if err != nil {
		renderError(w, "confirm with github failed", err)
		return
	}
	redirect(w, r, localPath(trip.next), http.StatusSeeOther)
}

// pendingInvitation is the token of the invitation this browser opened
// before it signed in, or "".
func pendingInvitation(r *http.Request) string {
	cookie, err := r.Cookie("pending_invitation")
	if err != nil {
		return ""
	}
	return cookie.Value
}

// githubDisplayName is the profile name of a GitHub account, or its login
// without one.
func githubDisplayName(account service.GitHubAccount) string {
	if account.Name != "" && validation.ValidateName(account.Name) == nil {
		return account.Name
	}
	return account.Login
}

// githubNameCookie carries the GitHub name from the sign-in to the first
// sign-in page, where it fills in the fields. It is no more than a
// suggestion the person sees and may change; the name is saved only when
// they continue. It belongs to one user, so another account signing in on
// the same browser never sees it.
const githubNameCookie = "github_name"

func setGitHubNameCookie(w http.ResponseWriter, r *http.Request, userID, name string) {
	if name == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     githubNameCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(userID + "\n" + name)),
		Path:     "/auth",
		MaxAge:   30 * 60,
		HttpOnly: true,
		Secure:   ctxkeys.RenderConfig(r.Context()).IsProduction(),
		SameSite: http.SameSiteLaxMode,
	})
}

// githubName is the name of the cookie when it belongs to user.
func githubName(r *http.Request, user *model.User) string {
	cookie, err := r.Cookie(githubNameCookie)
	if err != nil || user == nil {
		return ""
	}
	value, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return ""
	}
	userID, name, ok := strings.Cut(string(value), "\n")
	if !ok || userID != user.ID || validation.ValidateName(name) != nil {
		return ""
	}
	return name
}

func clearGitHubNameCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     githubNameCookie,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   ctxkeys.RenderConfig(r.Context()).IsProduction(),
		SameSite: http.SameSiteLaxMode,
	})
}
