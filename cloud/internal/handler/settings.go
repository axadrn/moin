package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/cloud/internal/workspace"
)

// Settings is /cloud/settings: the account, and the button that deletes it.
func (c *Cloud) Settings(w http.ResponseWriter, r *http.Request) {
	space, ok := c.signedIn(r)
	if !ok {
		http.Redirect(w, r, "/login?next=/cloud/settings", http.StatusSeeOther)
		return
	}
	render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Mail: space.Mail}))
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
	typed, _ := workspace.CleanMail(r.PostFormValue("mail"))
	if typed != space.Mail {
		w.WriteHeader(http.StatusBadRequest)
		render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Mail: space.Mail, Problem: "Type " + space.Mail + " to delete the account."}))
		return
	}
	// A running subscription ends first, so nobody pays for an account
	// that is gone.
	if err := c.cancelSubscription(r.Context(), space); err != nil {
		slog.Error("subscription not cancelled, account kept", "workspace", space.ID, "error", err)
		w.WriteHeader(http.StatusBadGateway)
		render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Mail: space.Mail, Problem: "Your subscription did not cancel, so the account is still here. Try again in a minute."}))
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
