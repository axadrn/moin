package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// The settings tab holds what belongs to the workspace rather than to one
// app: the GitHub App, the bucket, the Cloudflare token, the Tailscale key
// and the registry logins.

// settings is what the settings tab shows. A part that failed to load
// keeps its error and the rest still shows.
type settings struct {
	loaded          bool
	github          client.GitHub
	githubErr       error
	storage         *client.BackupStorage
	storageErr      error
	integrations    client.Integrations
	integrationsErr error
	registries      []client.Registry
	registriesErr   error
}

// settingRow is one line of the settings tab.
type settingRow struct {
	kind, name, value string
	// set says there is something to remove with x.
	set  bool
	host string
}

// loadSettings fetches every part of the settings tab.
func (m model) loadSettings() tea.Cmd {
	return func() tea.Msg {
		loaded := settings{loaded: true}
		loaded.github, loaded.githubErr = m.api.GitHub(m.ctx)
		storage, err := m.api.BackupStorage(m.ctx)
		var apiErr *client.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Code == "no_backups":
		case err != nil:
			loaded.storageErr = err
		default:
			loaded.storage = &storage
		}
		loaded.integrations, loaded.integrationsErr = m.api.Integrations(m.ctx)
		loaded.registries, loaded.registriesErr = m.api.Registries(m.ctx)
		return settingsMsg(loaded)
	}
}

func (m model) settingRows() []settingRow {
	s := m.settings
	failed := func(err error) string { return style.Danger.Render(sentence(err.Error())) }
	off := func(text string) string { return style.Muted.Render(text) }

	github := off("not connected · enter shows the install link")
	switch {
	case s.githubErr != nil:
		github = failed(s.githubErr)
	case !s.github.Enabled:
		github = off("not set up on this control plane")
	case len(s.github.Installations) > 0:
		accounts := make([]string, 0, len(s.github.Installations))
		for _, installation := range s.github.Installations {
			accounts = append(accounts, installation.Account)
		}
		github = "connected to " + strings.Join(accounts, ", ")
	}

	bucket := off("not set · enter sets it")
	switch {
	case s.storageErr != nil:
		bucket = failed(s.storageErr)
	case s.storage != nil:
		endpoint := s.storage.Endpoint
		if endpoint == "" {
			endpoint = "AWS S3"
		}
		bucket = strings.TrimSuffix(s.storage.Bucket+"/"+s.storage.Path, "/") + off(" at "+host(endpoint)+" · key "+s.storage.AccessKey)
	}

	cloudflare, tailscale := off("not set · enter sets it"), off("not set · enter sets it")
	switch {
	case s.integrationsErr != nil:
		cloudflare, tailscale = failed(s.integrationsErr), failed(s.integrationsErr)
	default:
		if s.integrations.Cloudflare {
			cloudflare = "stored" + off(" · account "+dash(s.integrations.CloudflareAccount))
		}
		if s.integrations.Tailscale {
			tailscale = "stored"
		}
	}

	rows := []settingRow{
		{kind: "github", name: "GitHub", value: github},
		{kind: "bucket", name: "Bucket", value: bucket, set: s.storage != nil},
		{kind: "cloudflare", name: "Cloudflare token", value: cloudflare, set: s.integrations.Cloudflare},
		{kind: "tailscale", name: "Tailscale key", value: tailscale, set: s.integrations.Tailscale},
	}
	if s.registriesErr != nil {
		rows = append(rows, settingRow{kind: "registries", name: "Registries", value: failed(s.registriesErr)})
	}
	for _, registry := range s.registries {
		rows = append(rows, settingRow{kind: "registry", name: "Registry " + registry.Host, value: registry.Username, set: true, host: registry.Host})
	}
	return rows
}

func (m model) settingKey(key string) (tea.Model, tea.Cmd) {
	rows := m.settingRows()
	switch key {
	case "up", "k":
		m.settingSelected = max(m.settingSelected-1, 0)
		return m, nil
	case "down", "j":
		m.settingSelected = min(m.settingSelected+1, len(rows)-1)
		return m, nil
	case "a":
		return m.ask("New registry login", func(values []string) tea.Cmd { return m.setRegistry(values[0], values[1], values[2]) },
			formField{label: "Host", hint: "like ghcr.io"},
			formField{label: "Username"},
			formField{label: "Password", hint: "a token that may read the images", secret: true},
		), nil
	}
	if m.settingSelected >= len(rows) {
		return m, nil
	}
	row := rows[m.settingSelected]
	switch key {
	case "enter":
		return m.editSetting(row)
	case "x":
		if row.set {
			return m.removeSetting(row), nil
		}
	}
	return m, nil
}

// editSetting opens the form of a row, or says how to connect GitHub.
func (m model) editSetting(row settingRow) (tea.Model, tea.Cmd) {
	switch row.kind {
	case "github":
		if !m.settings.github.Enabled {
			m.err = errors.New("the GitHub App is not set up on this control plane")
			return m, nil
		}
		m.notice = "Install the " + build.Name + " GitHub App on your account or organization: " + m.settings.github.InstallURL
		return m, nil
	case "bucket":
		var current client.BackupStorage
		if m.settings.storage != nil {
			current = *m.settings.storage
		}
		return m.ask("Bucket", func(values []string) tea.Cmd {
			return m.setBucket(client.BackupStorage{Endpoint: values[0], Region: values[1], Bucket: values[2], Path: values[3], AccessKey: values[4], SecretKey: values[5]})
		},
			formField{label: "Endpoint", value: current.Endpoint, optional: true, hint: placeholder(current.Endpoint, "https://… of an S3-compatible store, empty for AWS S3")},
			formField{label: "Region", value: current.Region, optional: true, hint: placeholder(current.Region, "auto for Cloudflare R2")},
			formField{label: "Bucket", value: current.Bucket, hint: placeholder(current.Bucket, "like ops-backups")},
			formField{label: "Path", value: current.Path, optional: true, hint: placeholder(current.Path, "a folder inside the bucket, may stay empty")},
			formField{label: "Access key", value: current.AccessKey, hint: placeholder(current.AccessKey, "the access key ID")},
			formField{label: "Secret key", secret: true, hint: build.Name + " checks that it can write before it stores the bucket"},
		), nil
	case "cloudflare":
		return m.ask("Cloudflare token", func(values []string) tea.Cmd { return m.setKeys(&values[0], nil) },
			formField{label: "API token", secret: true, hint: "Tunnel: Edit, DNS: Edit, Zone Settings: Edit"},
		), nil
	case "tailscale":
		return m.ask("Tailscale key", func(values []string) tea.Cmd { return m.setKeys(nil, &values[0]) },
			formField{label: "Auth key", secret: true, hint: "a reusable key, tskey-…"},
		), nil
	case "registry":
		host := row.host
		return m.ask("Registry "+host, func(values []string) tea.Cmd { return m.setRegistry(host, values[0], values[1]) },
			formField{label: "Username", value: row.value, hint: placeholder(row.value, "")},
			formField{label: "Password", secret: true},
		), nil
	}
	return m, nil
}

// removeSetting asks before it removes the bucket, a key or a login.
func (m model) removeSetting(row settingRow) model {
	blank := ""
	switch row.kind {
	case "bucket":
		return m.confirmFirst("Stop backups?", "The bucket keeps what is in it.", "stop", true, m.done(func() error { return m.api.RemoveBackupStorage(m.ctx) }, "Backups stopped; the bucket keeps what is in it"))
	case "cloudflare":
		return m.confirmFirst("Remove the Cloudflare token?", "Tunnels need it again to change.", "remove", true, m.setKeys(&blank, nil))
	case "tailscale":
		return m.confirmFirst("Remove the Tailscale key?", "Servers already on the tailnet stay on it.", "remove", true, m.setKeys(nil, &blank))
	case "registry":
		host := row.host
		return m.confirmFirst("Remove the login for "+host+"?", "Private images from there no longer pull.", "remove", true, m.done(func() error { return m.api.RemoveRegistry(m.ctx, host) }, "Removed the login for "+host))
	}
	return m
}

// done runs a change, says notice and loads the settings again.
func (m model) done(change func() error, notice string) tea.Cmd {
	return func() tea.Msg {
		if err := change(); err != nil {
			return errMsg{err}
		}
		m.shared.program.Send(noticeMsg(notice))
		return m.loadSettings()()
	}
}

func (m model) setBucket(storage client.BackupStorage) tea.Cmd {
	return m.done(func() error {
		_, err := m.api.SetBackupStorage(m.ctx, storage)
		return err
	}, "Bucket saved; backups go there now")
}

// setKeys stores or, with "", removes the Cloudflare token or the
// Tailscale key; nil leaves one as it is.
func (m model) setKeys(cloudflareToken, tailscaleKey *string) tea.Cmd {
	notice := ""
	switch {
	case cloudflareToken != nil && *cloudflareToken == "":
		notice = "Cloudflare token removed"
	case cloudflareToken != nil:
		notice = "Cloudflare token checked and stored"
	case tailscaleKey != nil && *tailscaleKey == "":
		notice = "Tailscale key removed"
	default:
		notice = "Tailscale key stored"
	}
	return m.done(func() error {
		_, err := m.api.SetIntegrations(m.ctx, cloudflareToken, tailscaleKey)
		return err
	}, notice)
}

func (m model) setRegistry(host, username, password string) tea.Cmd {
	return m.done(func() error { return m.api.SetRegistry(m.ctx, host, username, password) }, "Saved the login for "+host)
}

// settingsView lists the rows of the settings tab.
func (m model) settingsView(room int) []string {
	if !m.settings.loaded {
		return []string{pad + style.Muted.Render("Loading the settings…")}
	}
	rows := m.settingRows()
	cells := make([][]string, 0, len(rows))
	for index, row := range rows {
		cells = append(cells, []string{name(row.name, index == m.settingSelected), row.value})
	}
	lines := table([]string{"SETTING", "VALUE"}, cells, m.settingSelected, max(room-2, -1))
	if len(m.settings.registries) == 0 && m.settings.registriesErr == nil {
		lines = append(lines, "", pad+style.Muted.Render("No registry logins. Press a to add one for private images."))
	}
	return lines
}
