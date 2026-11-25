package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/internal/link"
)

// ConnectionsHandler lists, edits and removes the connections of the
// signed-in person. A connection lists and removes; only a signed-in
// browser changes rights, so no client gives itself more.
type ConnectionsHandler struct {
	connections *service.ConnectionService
}

func NewConnectionsHandler(connections *service.ConnectionService) *ConnectionsHandler {
	return &ConnectionsHandler{connections: connections}
}

// Connection is the public JSON shape of a connection. It never carries a
// token.
type Connection struct {
	ID string `json:"id"`
	// ClientName is what the client called itself, such as "moin CLI on
	// laptop-1".
	ClientName  string     `json:"client_name"`
	WorkspaceID string     `json:"workspace_id"`
	Rights      []string   `json:"rights"`
	Current     bool       `json:"current"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ConnectionList is the answer of GET /api/v1/account/connections.
type ConnectionList struct {
	Items []Connection `json:"items"`
}

// RightsRequest changes the rights of a connection or CI token. Read is
// always in.
type RightsRequest struct {
	Rights []string `json:"rights"`
}

// NewConnection converts a connection into its public JSON shape. current
// marks the connection of the request.
func NewConnection(connection model.Connection, current *model.Bearer) Connection {
	return Connection{
		ID:          link.ID(connection.ID),
		ClientName:  connection.ClientName,
		WorkspaceID: link.ID(connection.WorkspaceID),
		Rights:      connection.Rights.List(),
		Current:     current != nil && current.ConnectionID == connection.ID,
		LastUsedAt:  connection.LastUsedAt,
		CreatedAt:   connection.CreatedAt,
	}
}

// List returns the live connections, newest first. A connection sees those
// of its own workspace.
func (h *ConnectionsHandler) List(w http.ResponseWriter, r *http.Request) {
	connections, err := h.connections.List(ctxkeys.User(r.Context()).ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	bearer := ctxkeys.Bearer(r.Context())
	items := make([]Connection, 0, len(connections))
	for _, connection := range connections {
		if bearer != nil && connection.WorkspaceID != bearer.WorkspaceID {
			continue
		}
		items = append(items, NewConnection(connection, bearer))
	}
	JSON(w, http.StatusOK, ConnectionList{Items: items})
}

// SetRights changes what a connection may do: {"rights": ["deploy"]}. It
// counts from the connection's next request.
func (h *ConnectionsHandler) SetRights(w http.ResponseWriter, r *http.Request) {
	var request RightsRequest
	if !Decode(w, r, &request) {
		return
	}
	rights, ok := model.ParseRights(request.Rights...)
	if !ok {
		ServiceError(w, service.ErrRightsUnknown)
		return
	}
	connection, err := h.connections.SetRights(ctxkeys.User(r.Context()).ID, r.PathValue("id"), rights, source(r))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewConnection(*connection, nil))
}

// Remove ends a connection and every token of it, at once. A connection
// removes those of its own workspace.
func (h *ConnectionsHandler) Remove(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if bearer := ctxkeys.Bearer(r.Context()); bearer != nil {
		connections, err := h.connections.List(user.ID)
		if err != nil {
			ServiceError(w, err)
			return
		}
		mine := false
		for _, connection := range connections {
			if link.ID(connection.ID) == link.ID(r.PathValue("id")) && connection.WorkspaceID == bearer.WorkspaceID {
				mine = true
			}
		}
		if !mine {
			ServiceError(w, repository.ErrConnectionNotFound)
			return
		}
	}
	err := h.connections.Remove(user.ID, r.PathValue("id"), source(r))
	if err != nil {
		ServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
