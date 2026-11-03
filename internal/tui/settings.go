package tui

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// The settings tab holds what belongs to the workspace rather than to one
// app: the version of the control plane and its update, automatic agent
// updates, the user's own email alerts, the alert destinations of the
// workspace, the GitHub App, the bucket, the Cloudflare token, the
// Tailscale key and the registry logins.

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
	version         client.Version
	versionErr      error
	autoUpdate      bool
	autoUpdateErr   error
	account         client.Account
	accountErr      error
	destinations    []client.AlertDestination
	destinationsErr error
}

// settingRow is one line of the settings tab.
type settingRow struct {
	kind, name, value string
	// set says there is something to remove with x.
	set  bool
	host string
	// destination is the alert destination of the row, by name.
	destination string
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
		loaded.version, loaded.versionErr = m.api.Version(m.ctx)
		loaded.autoUpdate, loaded.autoUpdateErr = m.api.AgentUpdates(m.ctx)
		loaded.account, loaded.accountErr = m.api.Account(m.ctx)
		loaded.destinations, loaded.destinationsErr = m.api.AlertDestinations(m.ctx)
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
		bucket = s.storage.Bucket + "/" + s.storage.Folder + off(" at "+host(endpoint)+" · key "+s.storage.AccessKey)
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

	controlPlane := s.version.Version
	switch {
	case s.versionErr != nil:
		controlPlane = failed(s.versionErr)
	case s.version.Update.Running():
		controlPlane += style.Warning.Render(" · updating to " + s.version.Update.Version)
	case s.version.UpdateAvailable && s.version.CanUpdate:
		controlPlane += style.Warning.Render(" · "+s.version.Latest+" is out") + off(" · u updates · "+s.version.Summary)
	case s.version.UpdateAvailable:
		controlPlane += style.Warning.Render(" · "+s.version.Latest+" is out") + off(" · update it on the server with sh update.sh")
	case !s.version.SelfHosted:
		controlPlane += off(" · updates itself with every release")
	}
	agentUpdates := "on" + off(" · agents update to the control plane's version · enter turns it off")
	switch {
	case s.autoUpdateErr != nil:
		agentUpdates = failed(s.autoUpdateErr)
	case !s.autoUpdate:
		agentUpdates = "off" + off(" · enter turns it on; u on a server updates its agent")
	}

	alerts := "on" + off(" · a mail to "+s.account.Email+" when a site or server goes down or a deploy fails · enter turns it off")
	switch {
	case s.accountErr != nil:
		alerts = failed(s.accountErr)
	case !s.account.EmailAlerts:
		alerts = "off" + off(" · enter turns it on")
	}

	rows := []settingRow{
		{kind: "controlplane", name: "Control plane", value: controlPlane},
		{kind: "agentupdates", name: "Agent updates", value: agentUpdates},
		{kind: "alerts", name: "Email me alerts", value: alerts},
	}
	if s.destinationsErr != nil {
		rows = append(rows, settingRow{kind: "destinations", name: "Alert destinations", value: failed(s.destinationsErr)})
	}
	for _, destination := range s.destinations {
		state := off(" · enter or T sends a test")
		if destination.Failing {
			state = style.Danger.Render(" · failing: " + destination.LastError)
		}
		events := "every alert"
		if len(destination.Events) < 5 {
			events = strings.Join(destination.Events, ", ")
		}
		rows = append(rows, settingRow{kind: "destination", name: "Alerts to " + destination.Name, value: destination.Kind + off(" "+destination.Hint+" · "+events) + state, set: true, destination: destination.Name})
	}
	rows = append(rows, []settingRow{
		{kind: "github", name: "GitHub", value: github},
		{kind: "bucket", name: "Bucket", value: bucket, set: s.storage != nil},
		{kind: "cloudflare", name: "Cloudflare token", value: cloudflare, set: s.integrations.Cloudflare},
		{kind: "tailscale", name: "Tailscale key", value: tailscale, set: s.integrations.Tailscale},
	}...)
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
	case "a":
		return m.ask("New registry login", func(values []string) tea.Cmd { return m.setRegistry(values[0], values[1], values[2]) },
			formField{label: "Host", hint: "like ghcr.io"},
			formField{label: "Username"},
			formField{label: "Password", hint: "a token that may read the images", secret: true},
		), nil
	case "u":
		return m.updateControlPlane()
	case "A":
		return m, m.setAgentUpdates(!m.settings.autoUpdate)
	case "E":
		return m, m.setEmailAlerts(!m.settings.account.EmailAlerts)
	case "C":
		return m.askDestination(), nil
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
	case "T":
		if row.kind == "destination" {
			return m, m.testDestination(row.destination)
		}
	}
	return m, nil
}

// editSetting opens the form of a row, or says how to connect GitHub.
func (m model) editSetting(row settingRow) (tea.Model, tea.Cmd) {
	switch row.kind {
	case "controlplane":
		return m.updateControlPlane()
	case "agentupdates":
		return m, m.setAgentUpdates(!m.settings.autoUpdate)
	case "alerts":
		return m, m.setEmailAlerts(!m.settings.account.EmailAlerts)
	case "destination":
		return m, m.testDestination(row.destination)
	case "github":
		if !m.settings.github.Enabled {
			m.err = errors.New("the GitHub App is not set up on this control plane")
			return m, nil
		}
		m.notice = "Connect GitHub, also when the " + build.Name + " GitHub App is already installed: " + cmp.Or(m.settings.github.ConnectURL, m.settings.github.InstallURL)
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
			formField{label: "Bucket", value: current.Bucket, hint: placeholder(current.Bucket, "your bucket's name")},
			formField{label: "Folder", value: current.Path, optional: true, hint: placeholder(current.Path, "only if the bucket holds other files too")},
			formField{label: "Access key", value: current.AccessKey, hint: placeholder(current.AccessKey, "the access key ID")},
			formField{label: "Secret key", secret: true, hint: build.Name + " checks that it can write before it stores the bucket"},
		), nil
	case "cloudflare":
		return m.ask("Cloudflare token", func(values []string) tea.Cmd { return m.setKeys(&values[0], nil) },
			formField{label: "API token", secret: true, hint: "Tunnel: Edit, DNS: Edit (Zone Settings: Edit optional, for Always Use HTTPS)"},
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
		return m.confirmName("Stop backups?", "The bucket keeps what is in it.", m.settings.storage.Bucket, m.done(func() error { return m.api.RemoveBackupStorage(m.ctx) }, "Backups stopped; the bucket keeps what is in it"))
	case "cloudflare":
		return m.confirmFirst("Remove the Cloudflare token?", "Turn tunnels off first: without it ops cannot delete them in Cloudflare.", "remove", true, m.setKeys(&blank, nil))
	case "tailscale":
		return m.confirmFirst("Remove the Tailscale key?", "The next security change of a server opens SSH on every interface again.", "remove", true, m.setKeys(nil, &blank))
	case "destination":
		name := row.destination
		return m.confirmFirst("Remove the alert destination "+name+"?", "Its alerts stop.", "remove", true, m.done(func() error { return m.api.RemoveAlertDestination(m.ctx, name) }, "Removed "+name))
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

// updateControlPlane asks before it updates a self-hosted control plane to
// the newest release.
func (m model) updateControlPlane() (tea.Model, tea.Cmd) {
	version := m.settings.version
	switch {
	case !version.SelfHosted:
		m.notice = "This control plane updates itself with every release"
		return m, nil
	case version.Update.Running():
		m.notice = "The control plane is updating to " + version.Update.Version
		return m, nil
	case !version.UpdateAvailable:
		m.notice = "The control plane runs " + version.Version + ", the newest release"
		return m, nil
	case !version.CanUpdate:
		m.err = errors.New("only an admin of this control plane updates it, here or on the server with sh update.sh")
		return m, nil
	}
	return m.confirmFirst("Update the control plane to "+version.Latest+"?", "It is away for a minute; your apps keep running. "+version.Summary, "update", false,
		m.done(func() error {
			_, err := m.api.UpdateControlPlane(m.ctx)
			return err
		}, "Updating the control plane to "+version.Latest+"; this tab shows when it is back")), nil
}

// setAgentUpdates turns automatic agent updates on or off.
func (m model) setAgentUpdates(on bool) tea.Cmd {
	notice := "Agents update to the control plane's version by themselves now"
	if !on {
		notice = "Agents no longer update by themselves; u on a server updates its agent"
	}
	return m.done(func() error { return m.api.SetAgentUpdates(m.ctx, on) }, notice)
}

// setEmailAlerts turns the user's email alerts on or off.
func (m model) setEmailAlerts(on bool) tea.Cmd {
	notice := "Email alerts on: a mail when a site or server goes down or a deploy fails"
	if !on {
		notice = "Email alerts off"
	}
	return m.done(func() error { return m.api.SetEmailAlerts(m.ctx, on) }, notice)
}

// askDestination asks for a new alert destination: Slack, Discord or a
// webhook take the URL, Telegram the bot token and the chat.
func (m model) askDestination() model {
	kinds := []string{"slack", "discord", "telegram", "webhook"}
	return m.ask("New alert destination", func(values []string) tea.Cmd {
		destination := client.NewAlertDestination{Kind: strings.ToLower(values[0]), Name: values[1], ChatID: values[3]}
		if destination.Kind == "telegram" {
			destination.BotToken = values[2]
		} else {
			destination.URL = values[2]
		}
		if values[4] != "" {
			for _, event := range strings.Split(values[4], ",") {
				destination.Events = append(destination.Events, strings.TrimSpace(event))
			}
		}
		return m.addDestination(destination)
	},
		formField{label: "Kind", hint: "slack, discord, telegram or webhook", check: func(value string) error {
			if !slices.Contains(kinds, strings.ToLower(value)) {
				return errors.New("choose slack, discord, telegram or webhook")
			}
			return nil
		}},
		formField{label: "Name", hint: "like team-ops"},
		formField{label: "URL or bot token", secret: true, hint: "the incoming webhook URL; for Telegram the bot token from @BotFather"},
		formField{label: "Chat ID", optional: true, hint: "Telegram only: like -1001234567890, or @channel"},
		formField{label: "Events", optional: true, hint: "empty for all; or uptime.down,uptime.up,server.down,server.up,deploy.failed"},
	)
}

// addDestination adds an alert destination; a webhook's signing secret
// shows this once.
func (m model) addDestination(destination client.NewAlertDestination) tea.Cmd {
	return func() tea.Msg {
		added, err := m.api.AddAlertDestination(m.ctx, destination)
		if err != nil {
			return errMsg{err}
		}
		notice := "Added " + added.Name + "; T sends it a test"
		if added.SigningSecret != "" {
			notice = "Added " + added.Name + ". Signing secret, shown this once: " + added.SigningSecret
		}
		m.shared.program.Send(noticeMsg(notice))
		return m.loadSettings()()
	}
}

// testDestination sends a test alert to a destination.
func (m model) testDestination(name string) tea.Cmd {
	return m.done(func() error {
		_, err := m.api.TestAlertDestination(m.ctx, name)
		return err
	}, "Sent a test to "+name+"; it took it")
}
