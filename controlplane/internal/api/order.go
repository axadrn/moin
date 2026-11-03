package api

import "net/http"

// OrderRequest is the body of PUT /api/v1/apps/order and PUT
// /api/v1/servers/order: every app or server of the workspace once, by name
// or ID, in the new order.
type OrderRequest struct {
	Order []string `json:"order"`
}

// Reorder puts the apps in a new order and answers them in it.
func (h *AppsHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var request OrderRequest
	if !Decode(w, r, &request) {
		return
	}
	apps, err := h.apps.Reorder(actor(r), request.Order)
	if err != nil {
		ServiceError(w, err)
		return
	}
	list := AppList{Items: make([]App, 0, len(apps))}
	for _, app := range apps {
		list.Items = append(list.Items, NewApp(app))
	}
	JSON(w, http.StatusOK, list)
}

// Reorder puts the servers in a new order and answers them in it.
func (h *ServersHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var request OrderRequest
	if !Decode(w, r, &request) {
		return
	}
	servers, err := h.servers.Reorder(actor(r), request.Order)
	if err != nil {
		ServiceError(w, err)
		return
	}
	list := ServerList{Items: make([]Server, 0, len(servers))}
	for _, server := range servers {
		list.Items = append(list.Items, NewServer(server))
	}
	JSON(w, http.StatusOK, list)
}
