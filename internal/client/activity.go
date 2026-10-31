package client

import (
	"context"
	"errors"
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

// Team is the workspace of the token with its members and the invitations
// still open.
type Team struct {
	Workspace struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"workspace"`
	Role    string `json:"role"`
	Members []struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"members"`
	Invitations []struct {
		Email     string    `json:"email"`
		Role      string    `json:"role"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"invitations"`
}

// Team returns the workspace the token belongs to.
func (c *Client) Team(ctx context.Context) (Team, error) {
	var list struct {
		Items []struct {
			Workspace struct {
				Slug string `json:"slug"`
			} `json:"workspace"`
		} `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/workspaces", nil, &list)
	if err != nil {
		return Team{}, err
	}
	if len(list.Items) == 0 {
		return Team{}, errors.New("this token reaches no workspace: log in with a token of your team")
	}
	var team Team
	err = c.do(ctx, http.MethodGet, "/api/v1/workspaces/"+list.Items[0].Workspace.Slug, nil, &team)
	return team, err
}

// Invite emails an invitation to the token's workspace. role is member or
// admin.
func (c *Client) Invite(ctx context.Context, email, role string) error {
	team, err := c.Team(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/api/v1/workspaces/"+team.Workspace.Slug+"/invitations", map[string]string{"email": email, "role": role}, nil)
}
