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
// its servers' tokens, its sessions and its logins. Copies under the lock
// expire by themselves.
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
	// Billing comes with its own step; until then a running subscription
	// is not deleted here by accident.
	if space.StripeSubscription != "" {
		w.WriteHeader(http.StatusConflict)
		render(w, r, pages.CloudSettings(pages.CloudSettingsProps{Mail: space.Mail, Problem: "This account has a subscription. Write to us to cancel it and delete the account."}))
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
