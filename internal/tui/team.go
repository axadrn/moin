package tui

import (
	"fmt"
	"net/mail"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// The team tab lists the members and the open invitations of the token's
// workspace. It does what the API allows a personal token: invite, send an
// invitation again, revoke it and rename the workspace. Roles, removing
// members, leaving and deleting the workspace take the web, signed in.

// teamMsg is the workspace the team tab shows.
type teamMsg struct {
	team client.Team
	err  error
}

// teamRow is a member or an open invitation; invitation is its ID.
type teamRow struct {
	cells      []string
	invitation string
	email      string
}

func (m model) loadTeam() tea.Cmd {
	return func() tea.Msg {
		team, err := m.api.Team(m.ctx)
		return teamMsg{team: team, err: err}
	}
}

func (m model) teamRows() []teamRow {
	rows := []teamRow{}
	for _, member := range m.team.Members {
		rows = append(rows, teamRow{cells: []string{member.Name, member.Email, member.Role, "member"}})
	}
	for _, invitation := range m.team.Invitations {
		left := "invited · expires " + invitation.ExpiresAt.Local().Format("Jan 2")
		rows = append(rows, teamRow{cells: []string{"", invitation.Email, invitation.Role, left}, invitation: invitation.ID, email: invitation.Email})
	}
	return rows
}

func (m model) teamKey(key string) (tea.Model, tea.Cmd) {
	rows := m.teamRows()
	slug := m.team.Workspace.Slug
	switch key {
	case "up", "k":
		m.teamSelected = max(m.teamSelected-1, 0)
	case "down", "j":
		m.teamSelected = min(m.teamSelected+1, max(len(rows)-1, 0))
	case "a", "n", "enter", "x":
		if !m.team.Permissions.ManageMembers && !m.team.Permissions.ManageWorkspace {
			m.err = fmt.Errorf("only admins and owners change the team; you are %s", m.team.Role)
			return m, nil
		}
	}
	switch key {
	case "a":
		return m.ask("Invite", func(values []string) tea.Cmd { return m.invite(values[0], values[1]) },
			formField{label: "Email", check: checkEmail},
			formField{label: "Role", value: "member", hint: "member or admin, enter takes member", check: checkRole},
		), nil
	case "n":
		name := m.team.Workspace.Name
		return m.ask("Workspace", func(values []string) tea.Cmd {
			return m.teamChange(func() error { return m.api.RenameWorkspace(m.ctx, slug, values[0]) }, "Renamed the workspace to "+values[0])
		}, formField{label: "Name", value: name, hint: placeholder(name, "")}), nil
	case "enter", "x":
		if m.teamSelected >= len(rows) || rows[m.teamSelected].invitation == "" {
			return m, nil
		}
		row := rows[m.teamSelected]
		if key == "enter" {
			return m, m.teamChange(func() error { return m.api.ResendInvitation(m.ctx, slug, row.invitation) }, "Sent the invitation to "+row.email+" again")
		}
		return m.confirmFirst("Revoke the invitation of "+row.email+"?", "The link in the email stops working.", "revoke", true,
			m.teamChange(func() error { return m.api.RevokeInvitation(m.ctx, slug, row.invitation) }, "Revoked the invitation of "+row.email)), nil
	}
	return m, nil
}

// checkEmail lets the invite form ask the email again when it is no address.
func checkEmail(email string) error {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return fmt.Errorf("%s is not an email address", email)
	}
	return nil
}

// checkRole lets the invite form ask the role again when it is wrong.
func checkRole(role string) error {
	if role != "member" && role != "admin" {
		return fmt.Errorf("the role is member or admin, not %s", role)
	}
	return nil
}

func (m model) invite(email, role string) tea.Cmd {
	return m.teamChange(func() error { return m.api.Invite(m.ctx, email, role) }, "Invited "+email+" as "+role)
}

// teamChange runs a change, says notice and loads the team again.
func (m model) teamChange(change func() error, notice string) tea.Cmd {
	return func() tea.Msg {
		if err := change(); err != nil {
			return errMsg{err}
		}
		m.shared.program.Send(noticeMsg(notice))
		return m.loadTeam()()
	}
}

func (m model) teamView(room int) []string {
	switch {
	case m.teamErr != nil:
		return []string{pad + style.Danger.Render(sentence(m.teamErr.Error()))}
	case m.team.Workspace.Slug == "":
		return []string{pad + style.Muted.Render("Loading the team…")}
	}
	lines := []string{pad + style.Bold.Render(m.team.Workspace.Name) + style.Muted.Render("  ·  you are "+m.team.Role), ""}
	rows := m.teamRows()
	cells := make([][]string, 0, len(rows))
	for index, row := range rows {
		cells = append(cells, []string{name(dash(row.cells[0]), index == m.teamSelected), row.cells[1], row.cells[2], style.Muted.Render(row.cells[3])})
	}
	lines = append(lines, table([]string{"NAME", "EMAIL", "ROLE", "STATE"}, cells, m.teamSelected, max(room-len(lines)-2, -1))...)
	return append(lines, "", pad+style.Muted.Render("Roles, removing members, leaving and deleting the workspace are web only: sign in to "+build.Name+" in the browser."))
}

// teamKeys are the keys of the team tab; the selected row decides some.
func (m model) teamKeys() [][2]string {
	keys := [][2]string{{"tab", "settings"}, {"↑↓", "select"}}
	admin := m.team.Permissions.ManageMembers || m.team.Permissions.ManageWorkspace
	if admin {
		keys = append(keys, [2]string{"a", "invite"}, [2]string{"n", "rename"})
	}
	rows := m.teamRows()
	if admin && m.teamSelected < len(rows) && rows[m.teamSelected].invitation != "" {
		keys = append(keys, [2]string{"enter", "resend"}, [2]string{"x", "revoke"})
	} else if len(rows) > 0 {
		keys = append(keys, [2]string{"role, remove", "web only"})
	}
	return append(keys, [2]string{"q", "quit"})
}
