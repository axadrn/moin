package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/axadrn/ops/controlplane/internal/service"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
	"github.com/axadrn/ops/controlplane/internal/validation"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

func (h *authHandler) githubOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     h.cfg.GitHubClientID,
		ClientSecret: h.cfg.GitHubClientSecret,
		RedirectURL:  h.cfg.APIURL + "/auth/github/callback",
		Scopes:       []string{"user:email"},
		Endpoint:     h.githubEndpoint(),
	}
}

// githubEndpoint is github.com, or GITHUB_URL when set, like the GitHub
// App uses.
func (h *authHandler) githubEndpoint() oauth2.Endpoint {
	base := strings.TrimRight(h.cfg.GitHubURL, "/")
	if base == "" || base == "https://github.com" {
		return github.Endpoint
	}
	return oauth2.Endpoint{AuthURL: base + "/login/oauth/authorize", TokenURL: base + "/login/oauth/access_token"}
}

// githubAPI is api.github.com, or GITHUB_API_URL when set.
func (h *authHandler) githubAPI() string {
	if base := strings.TrimRight(h.cfg.GitHubAPIURL, "/"); base != "" {
		return base
	}
	return "https://api.github.com"
}

// GitHubAuth redirects user to GitHub OAuth consent screen
func (h *authHandler) GitHubAuth(w http.ResponseWriter, r *http.Request) {
	if h.cfg.GitHubClientID == "" || h.cfg.GitHubClientSecret == "" {
		redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}
	// Generate secure state token for CSRF protection
	state := generateOAuthState()

	// Get config from context to determine if we're in production
	cfg := ctxkeys.RenderConfig(r.Context())
	isProduction := cfg.IsProduction()

	// Store state in secure cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProduction, // Secure flag based on APP_ENV (safer than r.TLS behind load balancers)
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	url := h.githubOAuthConfig().AuthCodeURL(state, oauth2.AccessTypeOffline)
	redirect(w, r, url, http.StatusTemporaryRedirect)
}

// GitHubCallback handles the OAuth callback from GitHub
func (h *authHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	// Validate state parameter for CSRF protection
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != state || state == "" {
		slog.Warn("github oauth state validation failed", "error", err)
		render(w, r, pages.Auth("OAuth authentication failed. Please try again."))
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		slog.Warn("github oauth callback missing code")
		render(w, r, pages.Auth("OAuth authentication failed. Please try again."))
		return
	}

	// Exchange code for token
	token, err := h.githubOAuthConfig().Exchange(context.Background(), code)
	if err != nil {
		slog.Error("github oauth token exchange failed", "error", err)
		render(w, r, pages.Auth("OAuth authentication failed. Please try again."))
		return
	}

	client := h.githubOAuthConfig().Client(context.Background(), token)
	email, name, err := githubAccount(client, h.githubAPI())
	if errors.Is(err, errOAuthEmailMissing) || errors.Is(err, errOAuthEmailUnverified) {
		slog.Warn("github oauth: no verified email", "error", err)
		render(w, r, pages.Auth("GitHub did not share a verified email address. Verify your email on GitHub and try again."))
		return
	}
	if err != nil {
		slog.Error("failed to get github user info", "error", err)
		render(w, r, pages.Auth("OAuth authentication failed. Please try again."))
		return
	}

	// Authenticate or create user
	user, err := h.authService.AuthenticateOAuth(email, "github")
	if errors.Is(err, service.ErrWaitlisted) {
		render(w, r, pages.AuthWaitlisted(strings.ToLower(email)))
		return
	}
	if errors.Is(err, service.ErrPrivate) {
		render(w, r, pages.AuthPrivate())
		return
	}
	if err != nil {
		slog.Error("oauth authentication failed", "error", err)
		render(w, r, pages.Auth("Authentication failed. Please try again."))
		return
	}

	_, err = h.authService.StartSession(w, r, user.ID)
	if err != nil {
		slog.Error("failed to create session", "error", err, "user_id", user.ID)
		render(w, r, pages.Auth("An error occurred. Please try again."))
		return
	}

	slog.Info("user logged in with github oauth", "user_id", user.ID)

	// Check if user needs onboarding
	needsOnboarding, err := h.authService.NeedsOnboarding(user.ID)
	if err != nil {
		slog.Warn("failed to check onboarding status", "error", err, "user_id", user.ID)
	}

	if needsOnboarding {
		setGitHubNameCookie(w, r, user.ID, name)
		redirect(w, r, "/auth/onboarding", http.StatusSeeOther)
	} else {
		h.redirectAfterAuth(w, r, user)
	}
}

// githubAccount returns the email and the name of the GitHub account: the
// profile name, or the login without one.
func githubAccount(client *http.Client, api string) (string, string, error) {
	var userInfo struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		Login string `json:"login"`
	}
	err := getOAuthJSON(client, api+"/user", &userInfo)
	if err != nil {
		return "", "", err
	}
	email, err := githubEmail(client, api, userInfo.Email)
	if err != nil {
		return "", "", err
	}
	name := strings.TrimSpace(userInfo.Name)
	if name == "" || validation.ValidateName(name) != nil {
		name = strings.TrimSpace(userInfo.Login)
	}
	return email, name, nil
}

// githubEmail returns the public email, or the primary address when the
// public one is private. Either must be verified on GitHub, so nobody signs
// in to an account by adding its address to GitHub without proving it.
func githubEmail(client *http.Client, api, public string) (string, error) {
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	err := getOAuthJSON(client, api+"/user/emails", &emails)
	if err != nil {
		return "", err
	}
	primary := ""
	for _, email := range emails {
		if !email.Verified || email.Email == "" {
			continue
		}
		if public != "" && strings.EqualFold(email.Email, public) {
			return email.Email, nil
		}
		if email.Primary {
			primary = email.Email
		}
	}
	if primary != "" {
		return primary, nil
	}
	if len(emails) == 0 && public == "" {
		return "", errOAuthEmailMissing
	}
	return "", errOAuthEmailUnverified
}

func getOAuthJSON(client *http.Client, url string, value any) error {
	response, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		closeErr := response.Body.Close()
		if closeErr != nil {
			slog.Error("failed to close response body", "error", closeErr)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(value)
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
