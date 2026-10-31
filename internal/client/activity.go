package client

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Event is one thing that happened in the workspace. Actor and Summary read
// as a sentence: "ada@example.com deployed blog v12".
type Event struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Summary   string    `json:"summary"`
	Via       string    `json:"via"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

// Activity returns up to limit events of the token's workspace, newest
// first.
func (c *Client) Activity(ctx context.Context, limit int) ([]Event, error) {
	var answer struct {
		Items []Event `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/activity?limit="+strconv.Itoa(limit), nil, &answer)
	return answer.Items, err
}
