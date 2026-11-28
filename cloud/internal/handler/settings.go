package handler

import (
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// Settings is /cloud/settings: the account, its sessions, and the button
// that deletes it.
func (c *Cloud) Settings(w http.ResponseWriter, r *http.Request) {
	space, frame, ok := c.page(w, r)
	if !ok {
		return
	}
	sessions, ok := c.sessions(w, r, space)
	if !ok {
		return
	}
	render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Cloud: frame, Sessions: sessions}))
}

// sessions are the browsers and CLIs signed in to space, this browser
// marked.
func (c *Cloud) sessions(w http.ResponseWriter, r *http.Request, space workspace.Workspace) ([]workspace.Session, bool) {
	current := ""
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		current = cookie.Value
	}
	sessions, err := c.store.Sessions(space.ID, current)
	if err != nil {
		renderError(w, "list the sessions", err)
		return nil, false
	}
	return sessions, true
}

// EndSessions signs out the session the form names, or with "all" every
// browser and CLI of the workspace. Signing out this browser goes home.
func (c *Cloud) EndSessions(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok || !c.sameOrigin(r) {
		http.Error(w, "sign in first", http.StatusForbidden)
		return
	}
	sessions, ok := c.sessions(w, r, space)
	if !ok {
		return
	}
	id := r.PostFormValue("session")
	this := id == "all"
	var err error
	if this {
		err = c.store.EndSessions(space.ID)
	} else {
		this = slices.ContainsFunc(sessions, func(s workspace.Session) bool { return s.ID == id && s.This })
		err = c.store.EndSessionOf(space.ID, id)
	}
	if err != nil {
		renderError(w, "sign a session out", err)
		return
	}
	slog.Info("sessions signed out", "workspace", space.ID, "all", id == "all")
	if this {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/cloud/settings", http.StatusSeeOther)
}

// DeleteAccount deletes the workspace when the typed mail is its mail:
// it cancels the subscription, then deletes its servers' tokens, its
// sessions and its logins. Copies under the lock expire by themselves.
func (c *Cloud) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok || !c.sameOrigin(r) {
		http.Error(w, "sign in first", http.StatusForbidden)
		return
	}
	frame := pages.Cloud{Mail: space.Mail, Plan: pages.PlanOf(space, time.Now(), c.Stripe != nil)}
	typed, _ := workspace.CleanMail(r.PostFormValue("mail"))
	if typed != space.Mail {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Cloud: frame, Problem: "Type " + space.Mail + " to delete the account."}))
		return
	}
	// A running subscription ends first, so nobody pays for an account
	// that is gone.
	if err := c.cancelSubscription(r.Context(), space); err != nil {
		slog.Error("subscription not cancelled, account kept", "workspace", space.ID, "error", err)
		w.WriteHeader(http.StatusBadGateway)
		render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Cloud: frame, Problem: "Your subscription did not cancel, so the account is still here. Try again in a minute."}))
		return
	}
	if err := c.store.DeleteWorkspace(space.ID); err != nil {
		renderError(w, "delete an account", err)
		return
	}
	c.watchers.Forget(space.ID)
	slog.Info("account deleted", "workspace", space.ID)
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode})
	render(w, r, pages.CloudDeleted())
}
