package handler

import (
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axadrn/moin/cloud/internal/github"
	"github.com/axadrn/moin/cloud/internal/mailer"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/workspace"
	"github.com/axadrn/moin/internal/build"
)

// Login is the sign-in page: GitHub, or a link by mail. next is where it
// goes after.
func (c *Cloud) Login(w http.ResponseWriter, r *http.Request) {
	next := localPath(r.URL.Query().Get("next"))
	if _, ok := c.signedIn(r); ok {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	render(w, r, pages.CloudLogin(pages.CloudLoginProps{Next: next, GitHub: c.github.On()}))
}

// SendLink mails a sign-in link. It says the same whether the mail has a
// workspace or not.
func (c *Cloud) SendLink(w http.ResponseWriter, r *http.Request) {
	if !c.sameOrigin(r) {
		http.Error(w, "this form belongs to another site", http.StatusForbidden)
		return
	}
	next := localPath(r.PostFormValue("next"))
	mail, err := workspace.CleanMail(r.PostFormValue("mail"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, pages.CloudLogin(pages.CloudLoginProps{Next: next, GitHub: c.github.On(), Mail: r.PostFormValue("mail"), Problem: "That is not an email address."}))
		return
	}
	now := time.Now()
	if !c.ipLimit.allow(clientIP(r), now) || !c.mailLimit.allow(mail, now) {
		w.WriteHeader(http.StatusTooManyRequests)
		render(w, r, pages.CloudLogin(pages.CloudLoginProps{Next: next, GitHub: c.github.On(), Mail: mail, Problem: "Too many links in a short time. Wait 15 minutes, or use the newest link."}))
		return
	}
	token, err := c.store.NewMailLink(mail)
	if err != nil {
		renderError(w, "make a sign-in link", err)
		return
	}
	link := c.appURL + "/login/link?" + url.Values{"token": {token}, "next": {next}}.Encode()
	message := mailer.Message{
		To:      mail,
		Subject: "Sign in to " + build.Name + " cloud",
		Text:    "Open this link to sign in to " + build.Name + " cloud:\n\n" + link + "\n\nIt works once, for 15 minutes. If you did not ask for it, ignore this mail.",
	}
	if err := c.mailer.Send(r.Context(), message); err != nil {
		renderError(w, "send the sign-in link", err)
		return
	}
	render(w, r, pages.CloudLinkSent(mail))
}

// LinkPage is where a sign-in link opens: one button signs in. A GET
// never does, so a mail scanner that opens the link does not use it up.
func (c *Cloud) LinkPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	render(w, r, pages.CloudLink(query.Get("token"), localPath(query.Get("next"))))
}

// UseLink signs in with the token of a sign-in link.
func (c *Cloud) UseLink(w http.ResponseWriter, r *http.Request) {
	if !c.sameOrigin(r) {
		http.Error(w, "this form belongs to another site", http.StatusForbidden)
		return
	}
	mail, err := c.store.UseMailLink(r.PostFormValue("token"))
	if notFound(err) {
		w.WriteHeader(http.StatusGone)
		render(w, r, pages.CloudLoginFailed("This link was used or is older than 15 minutes. Ask for a new one."))
		return
	}
	if err != nil {
		renderError(w, "use a sign-in link", err)
		return
	}
	c.startSession(w, r, mail, r.PostFormValue("next"))
}

// githubStateCookie holds the state of a trip to GitHub, so only this
// browser can finish it, and where it goes after: state.next.
const githubStateCookie = "github_state"

// GitHubStart sends the browser to GitHub to sign in.
func (c *Cloud) GitHubStart(w http.ResponseWriter, r *http.Request) {
	if !c.github.On() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	state := rand.Text()
	http.SetCookie(w, &http.Cookie{
		Name: githubStateCookie, Value: state + "." + url.QueryEscape(localPath(r.URL.Query().Get("next"))), Path: "/auth/github",
		MaxAge: 600, HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, c.github.AuthorizeURL(state, c.githubBack()), http.StatusSeeOther)
}

// githubBack is where GitHub sends the browser back, a callback URL of
// the app.
func (c *Cloud) githubBack() string {
	return c.appURL + "/auth/github/callback"
}

// GitHubBack signs in with the verified primary mail of the GitHub
// account, into the workspace of that mail.
func (c *Cloud) GitHubBack(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(githubStateCookie)
	var state, next string
	if err == nil {
		state, next, _ = strings.Cut(cookie.Value, ".")
		next, _ = url.QueryUnescape(next)
	}
	if !c.github.On() || state == "" || r.URL.Query().Get("state") != state {
		if _, ok := c.signedIn(r); ok {
			http.Redirect(w, r, "/cloud", http.StatusSeeOther)
			return
		}
		render(w, r, pages.CloudLoginFailed("This GitHub sign-in is no longer valid. Please try again."))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: githubStateCookie, Value: "", Path: "/auth/github", MaxAge: -1})
	mail, err := c.github.Mail(r.Context(), r.URL.Query().Get("code"), c.githubBack())
	if errors.Is(err, github.ErrNoMail) {
		render(w, r, pages.CloudLoginFailed(err.Error()+"."))
		return
	}
	if err != nil {
		slog.Error("github sign-in", "error", err)
		render(w, r, pages.CloudLoginFailed("GitHub did not sign you in. Please try again."))
		return
	}
	c.startSession(w, r, mail, next)
}
