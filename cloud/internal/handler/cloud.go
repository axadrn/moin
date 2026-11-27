package handler

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/stripe"
	"github.com/axadrn/moin/cloud/internal/watchers"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// Cloud answers the pages and the API of moin cloud: sign-in, the
// workspace page, `moin cloud` in the CLI, and the reports of servers.
type Cloud struct {
	store    *workspace.Store
	watchers *watchers.Watchers
	mailer   mailer.Mailer
	github   *github.GitHub
	appURL   string
	// secure marks the cookies Secure: everywhere but in development.
	secure bool
	// limits count the sign-in mails per address and per IP.
	mailLimit *limit
	ipLimit   *limit
	// Stripe sells moin cloud. Without it nobody can subscribe.
	Stripe *stripe.Stripe
}

// NewCloud is moin cloud on the site at appURL.
func NewCloud(store *workspace.Store, watchers *watchers.Watchers, mailer mailer.Mailer, github *github.GitHub, appURL string, secure bool) *Cloud {
	return &Cloud{
		store: store, watchers: watchers, mailer: mailer, github: github,
		appURL: strings.TrimRight(appURL, "/"), secure: secure,
		mailLimit: newLimit(3, 15*time.Minute),
		ipLimit:   newLimit(10, 15*time.Minute),
	}
}

// sessionCookie holds the session of a browser.
const sessionCookie = "session"

// signedIn is the workspace the browser is signed in to.
func (c *Cloud) signedIn(r *http.Request) (workspace.Workspace, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return workspace.Workspace{}, false
	}
	space, err := c.store.SessionWorkspace(cookie.Value)
	return space, err == nil
}

// startSession signs the browser in to a workspace and sends it to next.
func (c *Cloud) startSession(w http.ResponseWriter, r *http.Request, mail, next string) {
	space, err := c.store.SignIn(mail)
	if err != nil {
		renderError(w, "sign in", err)
		return
	}
	token, err := c.store.StartSession(space.ID, "browser")
	if err != nil {
		renderError(w, "start a session", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		MaxAge: int((30 * 24 * time.Hour).Seconds()), HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
	slog.Info("signed in", "workspace", space.ID)
	http.Redirect(w, r, localPath(next), http.StatusSeeOther)
}

// SignOut ends the browser's session.
func (c *Cloud) SignOut(w http.ResponseWriter, r *http.Request) {
	if !c.sameOrigin(r) {
		http.Error(w, "this form belongs to another site", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if err := c.store.EndSession(cookie.Value); err != nil {
			slog.Error("end session", "error", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// sameOrigin says whether a form was sent from this site. Browsers send
// Origin with every POST; a request without it is no browser's form.
func (c *Cloud) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == c.appURL
}

// localPath is next when it is a path of this site, else /cloud. A link
// never sends anyone to another site.
func localPath(next string) string {
	parsed, err := url.Parse(next)
	if next == "" || err != nil || parsed.Scheme != "" || parsed.Host != "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, `\`) {
		return "/cloud"
	}
	return next
}

// clientIP is the address of whoever sent r. Behind Cloudflare it is
// CF-Connecting-IP: only the tunnel reaches the site.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// bearer is the token of an Authorization header.
func bearer(r *http.Request) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token, ok && token != ""
}

// notFound says whether err is a token, code or server that is not there.
func notFound(err error) bool {
	return errors.Is(err, workspace.ErrNotFound)
}
