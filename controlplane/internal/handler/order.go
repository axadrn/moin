package handler

import (
	"net/http"
	"slices"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
)

// formOrder is the order the list form sends: the names in their order on
// the page, after a drag, with the one an arrow button named moved up or
// down by one.
func formOrder(r *http.Request) []string {
	_ = r.ParseForm()
	order := r.Form["order"]
	if name := r.PostForm.Get("up"); name != "" {
		order = service.Moved(order, name, slices.Index(order, name)-1)
	}
	if name := r.PostForm.Get("down"); name != "" {
		order = service.Moved(order, name, slices.Index(order, name)+1)
	}
	return order
}

// withView remembers how the member sees page and answers the request as
// the member who sees it so now.
func withView(w http.ResponseWriter, r *http.Request, users *service.UserService, page string) (*http.Request, bool) {
	user := ctxkeys.User(r.Context())
	view := r.FormValue("view")
	if err := users.SetView(user.ID, page, view); err != nil {
		failed(w, r, "View not changed", err, nil)
		return r, false
	}
	updated := *user
	if page == "servers" {
		updated.ServersView = view
	} else {
		updated.AppsView = view
	}
	return r.WithContext(ctxkeys.WithUser(r.Context(), &updated)), true
}

// Reorder saves the order of the apps after a drag or an arrow button and
// answers the list in it. A failed save puts the list back as it was.
func (h *AppsHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	if _, err := h.apps.Reorder(actor(r), formOrder(r)); err != nil {
		toastError(w, r, "Order not saved", errorMessage(err))
	}
	h.List(w, r)
}

// SetView shows the apps as a list or a grid, for this member from now on.
func (h *AppsHandler) SetView(w http.ResponseWriter, r *http.Request) {
	r, ok := withView(w, r, h.users, "apps")
	if ok {
		h.List(w, r)
	}
}

// Reorder saves the order of the servers after a drag or an arrow button
// and answers the list in it. A failed save puts the list back as it was.
func (h *ServersHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	if _, err := h.servers.Reorder(actor(r), formOrder(r)); err != nil {
		toastError(w, r, "Order not saved", errorMessage(err))
	}
	h.List(w, r)
}

// SetView shows the servers as a list or a grid, for this member from now
// on.
func (h *ServersHandler) SetView(w http.ResponseWriter, r *http.Request) {
	r, ok := withView(w, r, h.users, "servers")
	if ok {
		h.List(w, r)
	}
}
