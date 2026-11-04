package service

import (
	"slices"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
)

// The order of apps and servers is per workspace and shows everywhere: the
// web lists and sidebar, the CLI, the TUI, the API and MCP. A new order names
// every app or server of the workspace once, by name or ID.

// ErrOrder means an order does not name every app or server of the
// workspace exactly once.
var ErrOrder = repository.ErrOrder

// orderIDs turns an order of names or IDs into the IDs and names of items,
// and says whether it differs from the order items are in now.
func orderIDs[T any](items []T, key func(T) (id, name string), order []string) (ids, names []string, changed bool, err error) {
	if len(order) != len(items) {
		return nil, nil, false, ErrOrder
	}
	for index, wanted := range order {
		wanted = strings.TrimSpace(wanted)
		at := slices.IndexFunc(items, func(item T) bool {
			id, name := key(item)
			return id == wanted || name == wanted
		})
		if at < 0 {
			return nil, nil, false, ErrOrder
		}
		id, name := key(items[at])
		if slices.Contains(ids, id) {
			return nil, nil, false, ErrOrder
		}
		ids, names = append(ids, id), append(names, name)
		changed = changed || at != index
	}
	return ids, names, changed, nil
}

// Moved is order with name moved to index to, which stays within the
// order. An order without name comes back as it is.
func Moved(order []string, name string, to int) []string {
	from := slices.Index(order, name)
	if from < 0 {
		return order
	}
	moved := slices.Delete(slices.Clone(order), from, from+1)
	to = min(max(to, 0), len(moved))
	return slices.Insert(moved, to, name)
}

// Reorder puts the apps of the workspace in the order of names or IDs,
// which names every app once, and returns them in it.
func (s *AppService) Reorder(actor Actor, order []string) ([]model.App, error) {
	apps, err := s.apps.ListByWorkspaceID(actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ids, names, changed, err := orderIDs(apps, func(app model.App) (string, string) { return app.ID, app.Name }, order)
	if err != nil || !changed {
		return apps, err
	}
	err = s.apps.SetOrder(actor.WorkspaceID, ids)
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "apps.reordered", "workspace", "", map[string]string{"order": strings.Join(names, ", ")})
	s.notify(actor.WorkspaceID)
	return s.List(actor.WorkspaceID)
}

// Reorder puts the servers of the workspace in the order of names or IDs,
// which names every server once, and returns them in it.
func (s *ServerService) Reorder(actor Actor, order []string) ([]model.Server, error) {
	servers, err := s.List(actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	ids, names, changed, err := orderIDs(servers, func(server model.Server) (string, string) { return server.ID, server.Name }, order)
	if err != nil || !changed {
		return servers, err
	}
	err = s.servers.SetOrder(actor.WorkspaceID, ids)
	if err != nil {
		return nil, err
	}
	s.record(actor, "servers.reordered", "workspace", "", map[string]string{"order": strings.Join(names, ", ")})
	s.notify(actor.WorkspaceID)
	return s.List(actor.WorkspaceID)
}
