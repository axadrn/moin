package client

import (
	"context"
	"net/http"
	"slices"
)

// ReorderApps puts the apps of the workspace in a new order, which names
// every app once, and returns them in it.
func (c *Client) ReorderApps(ctx context.Context, order []string) ([]App, error) {
	var response struct {
		Items []App `json:"items"`
	}
	err := c.do(ctx, http.MethodPut, "/api/v1/apps/order", map[string][]string{"order": order}, &response)
	return response.Items, err
}

// ReorderServers puts the servers of the workspace in a new order, which
// names every server once, and returns them in it.
func (c *Client) ReorderServers(ctx context.Context, order []string) ([]Server, error) {
	var response struct {
		Items []Server `json:"items"`
	}
	err := c.do(ctx, http.MethodPut, "/api/v1/servers/order", map[string][]string{"order": order}, &response)
	return response.Items, err
}

// Moved is names with name moved to index to, kept within the list. A list
// without name comes back as it is.
func Moved(names []string, name string, to int) []string {
	from := slices.Index(names, name)
	if from < 0 {
		return names
	}
	moved := slices.Delete(slices.Clone(names), from, from+1)
	to = min(max(to, 0), len(moved))
	return slices.Insert(moved, to, name)
}
