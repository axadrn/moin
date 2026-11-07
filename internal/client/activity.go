package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// Event is one thing that happened in the workspace. Actor and Summary read
// as a sentence: "ada@example.com deployed blog v12". Via is web, cli, tui,
// api, mcp, hook, github or system, Token the API token by name. IP and
// UserAgent are there for admins only.
type Event struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Summary   string    `json:"summary"`
	Via       string    `json:"via"`
	Token     string    `json:"token"`
	App       string    `json:"app"`
	Server    string    `json:"server"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

// ViaLabel names how a change came in, with the API token it came with:
// "CLI, token ci". The web shows the same label as the CLI.
func ViaLabel(via, token string) string {
	label := ""
	switch via {
	case "web":
		label = "Web"
	case "cli":
		label = "CLI"
	case "tui":
		label = "TUI"
	case "api":
		label = "API"
	case "mcp":
		label = "MCP"
	case "hook":
		label = "Deploy hook"
	case "github":
		label = "GitHub"
	case "system":
		label = build.Name
	}
	if token != "" {
		if label == "" {
			return "token " + token
		}
		return label + ", token " + token
	}
	return label
}

// ActivityQuery picks a page of the activity: up to Limit events, after the
// cursor of the page before, of one app, server or member (an email).
type ActivityQuery struct {
	App, Server, User, Cursor string
	Limit                     int
}

// ActivityPage is a page of the activity, newest first. Next continues with
// older events and is empty on the last page.
type ActivityPage struct {
	Items []Event
	Next  string
}

// Activity returns a page of the token's workspace activity, newest first.
func (c *Client) Activity(ctx context.Context, query ActivityQuery) (ActivityPage, error) {
	values := url.Values{}
	for key, value := range map[string]string{"app": query.App, "server": query.Server, "user": query.User, "cursor": query.Cursor} {
		if value != "" {
			values.Set(key, value)
		}
	}
	if query.Limit > 0 {
		values.Set("limit", strconv.Itoa(query.Limit))
	}
	var answer struct {
		Items      []Event `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/activity?"+values.Encode(), nil, &answer)
	page := ActivityPage{Items: answer.Items}
	if answer.NextCursor != nil {
		page.Next = *answer.NextCursor
	}
	return page, err
}

// Team is the workspace of the token with its members and the invitations
// still open.
type Team struct {
	Workspace struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"workspace"`
	Role        string `json:"role"`
	Permissions struct {
		ManageWorkspace bool `json:"manage_workspace"`
		ManageMembers   bool `json:"manage_members"`
	} `json:"permissions"`
	Members []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	} `json:"members"`
	Invitations []Invitation `json:"invitations"`
}

// Invitation is an open invitation to the workspace.
type Invitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
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

func workspacePath(slug string, rest ...string) string {
	path := "/api/v1/workspaces/" + url.PathEscape(slug)
	for _, part := range rest {
		path += "/" + url.PathEscape(part)
	}
	return path
}

// ResendInvitation emails an open invitation again.
func (c *Client) ResendInvitation(ctx context.Context, slug, id string) error {
	return c.do(ctx, http.MethodPost, workspacePath(slug, "invitations", id, "resend"), nil, nil)
}

// RevokeInvitation takes an open invitation back.
func (c *Client) RevokeInvitation(ctx context.Context, slug, id string) error {
	return c.do(ctx, http.MethodDelete, workspacePath(slug, "invitations", id), nil, nil)
}

// RenameWorkspace gives the workspace a new name.
func (c *Client) RenameWorkspace(ctx context.Context, slug, name string) error {
	return c.do(ctx, http.MethodPatch, workspacePath(slug, "name"), map[string]string{"name": name}, nil)
}
