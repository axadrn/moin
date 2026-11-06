package client

import (
	"context"
	"net/http"
	"net/url"
)

// SetPinned pins an app, or unpins it, and returns it.
func (c *Client) SetPinned(ctx context.Context, app string, pinned bool) (App, error) {
	var out App
	err := c.do(ctx, http.MethodPut, "/api/v1/apps/"+url.PathEscape(app)+"/pin", struct {
		Pinned bool `json:"pinned"`
	}{pinned}, &out)
	return out, err
}

// SetSort sorts the apps another way for the user: smart, recent or
// server. Every list follows it, in the web too.
func (c *Client) SetSort(ctx context.Context, sort string) error {
	return c.do(ctx, http.MethodPut, "/api/v1/account/sort", struct {
		Sort string `json:"sort"`
	}{sort}, nil)
}
