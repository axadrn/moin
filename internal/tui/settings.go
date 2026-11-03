package tui

import (
	"cmp"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// The settings tab holds what belongs to the workspace rather than to one
// app: the version of the control plane and its update, automatic agent
// updates, the GitHub App, the bucket, the Cloudflare token, the Tailscale
// key and the registry logins.

// settings is what the settings tab shows. A part that failed to load
// keeps its error and the rest still shows.
type settings struct {
	loaded          bool
	github          client.GitHub
	githubErr       error
	buckets         []client.Bucket
	bucketsErr      error
	integrations    client.Integrations
	integrationsErr error
	registries      []client.Registry
	registriesErr   error
	version         client.Version
	versionErr      error
	autoUpdate      bool
	autoUpdateErr   error
}

// settingRow is one line of the settings tab.
type settingRow struct {
	kind, name, value string
	// set says there is something to remove with x.
	set  bool
	host string
	// bucket is the bucket of a bucket row.
	bucket *client.Bucket
}

// loadSettings fetches every part of the settings tab.
func (m model) loadSettings() tea.Cmd {
	return func() tea.Msg {
		loaded := settings{loaded: true}
		loaded.github, loaded.githubErr = m.api.GitHub(m.ctx)
		loaded.buckets, loaded.bucketsErr = m.api.Buckets(m.ctx)
		loaded.integrations, loaded.integrationsErr = m.api.Integrations(m.ctx)
		loaded.registries, loaded.registriesErr = m.api.Registries(m.ctx)
		loaded.version, loaded.versionErr = m.api.Version(m.ctx)
		loaded.autoUpdate, loaded.autoUpdateErr = m.api.AgentUpdates(m.ctx)
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

	var buckets []settingRow
	switch {
	case s.bucketsErr != nil:
		buckets = append(buckets, settingRow{kind: "buckets", name: "Buckets", value: failed(s.bucketsErr)})
	case len(s.buckets) == 0:
		buckets = append(buckets, settingRow{kind: "buckets", name: "Bucket", value: off("not set · enter adds it")})
	}
	for index := range s.buckets {
		bucket := &s.buckets[index]
		endpoint := bucket.Endpoint
		if endpoint == "" {
			endpoint = "AWS S3"
		}
		used := "no app yet"
		if len(bucket.Apps) > 0 {
			used = strings.Join(bucket.Apps, ", ")
		}
		if bucket.Default {
			used = "default · " + used
		}
		if len(bucket.OlderBackups) > 0 {
			used += " · older backups of " + strings.Join(bucket.OlderBackups, ", ")
		}
		value := bucket.Bucket + "/" + bucket.Folder + off(" at "+host(endpoint)+" · key "+bucket.AccessKey+" · "+used)
		buckets = append(buckets, settingRow{kind: "bucket", name: "Bucket " + bucket.Name, value: value, set: true, bucket: bucket})
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

	rows := []settingRow{
		{kind: "controlplane", name: "Control plane", value: controlPlane},
		{kind: "agentupdates", name: "Agent updates", value: agentUpdates},
		{kind: "github", name: "GitHub", value: github},
	}
	rows = append(rows, buckets...)
	rows = append(rows,
		settingRow{kind: "cloudflare", name: "Cloudflare token", value: cloudflare, set: s.integrations.Cloudflare},
		settingRow{kind: "tailscale", name: "Tailscale key", value: tailscale, set: s.integrations.Tailscale},
	)
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
	case "b":
		return m.bucketForm(nil), nil
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
	case "D":
		if row.bucket == nil {
			m.notice = "Pick a bucket row first, then D makes it the default"
			return m, nil
		}
		if row.bucket.Default {
			m.notice = row.bucket.Name + " is the default bucket already"
			return m, nil
		}
		input := row.bucket.Input()
		input.Default = true
		name := row.bucket.Name
		return m.confirmFirst("Make "+name+" the default bucket?", "Apps without a bucket of their own upload and back up there from now on; their older backups stay in the old default.", "make default", false,
			m.done(func() error {
				_, err := m.api.EditBucket(m.ctx, name, input)
				return err
			}, name+" is the default bucket now")), nil
	}
	return m, nil
}

// bucketForm asks for a new bucket, or for the changes of one. A change
// keeps the stored secret key when it is left empty.
func (m model) bucketForm(current *client.Bucket) model {
	var stored client.Bucket
	title, secretHint, name := "New bucket", build.Name+" checks that it can write before it stores the bucket", ""
	if current != nil {
		stored, title, secretHint, name = *current, "Bucket "+current.Name, "empty keeps the stored one", current.Name
	} else if len(m.settings.buckets) == 0 {
		stored.Name = "default"
	}
	return m.ask(title, func(values []string) tea.Cmd {
		input := client.BucketInput{Name: values[0], Endpoint: values[1], Region: values[2], Bucket: values[3], Path: values[4], AccessKey: values[5], SecretKey: values[6], Default: stored.Default}
		if name == "" {
			return m.done(func() error {
				_, err := m.api.AddBucket(m.ctx, input)
				return err
			}, "Bucket "+input.Name+" added; :bucket "+input.Name+" on an app moves it there")
		}
		return m.done(func() error {
			_, err := m.api.EditBucket(m.ctx, name, input)
			return err
		}, "Bucket "+input.Name+" saved; its apps upload and back up there now")
	},
		formField{label: "Name", value: stored.Name, hint: placeholder(stored.Name, "your name for it, like client-a"), check: checkName},
		formField{label: "Endpoint", value: stored.Endpoint, optional: true, hint: placeholder(stored.Endpoint, "https://… of an S3-compatible store, empty for AWS S3")},
		formField{label: "Region", value: stored.Region, optional: true, hint: placeholder(stored.Region, "auto for Cloudflare R2")},
		formField{label: "Bucket", value: stored.Bucket, hint: placeholder(stored.Bucket, "your bucket's name")},
		formField{label: "Folder", value: stored.Path, optional: true, hint: placeholder(stored.Path, "only if the bucket holds other files too")},
		formField{label: "Access key", value: stored.AccessKey, hint: placeholder(stored.AccessKey, "the access key ID")},
		formField{label: "Secret key", secret: true, optional: current != nil, hint: secretHint},
	)
}

// editSetting opens the form of a row, or says how to connect GitHub.
func (m model) editSetting(row settingRow) (tea.Model, tea.Cmd) {
	switch row.kind {
	case "controlplane":
		return m.updateControlPlane()
	case "agentupdates":
		return m, m.setAgentUpdates(!m.settings.autoUpdate)
	case "github":
		if !m.settings.github.Enabled {
			m.err = errors.New("the GitHub App is not set up on this control plane")
			return m, nil
		}
		m.notice = "Connect GitHub, also when the " + build.Name + " GitHub App is already installed: " + cmp.Or(m.settings.github.ConnectURL, m.settings.github.InstallURL)
		return m, nil
	case "buckets":
		return m.bucketForm(nil), nil
	case "bucket":
		return m.bucketForm(row.bucket), nil
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
		name := row.bucket.Name
		return m.confirmName("Remove the bucket "+name+"?", "What is in it stays. A bucket that apps use or that keeps older backups is refused.", name, m.done(func() error { return m.api.RemoveBucket(m.ctx, name) }, "Removed the bucket "+name+"; what is in it stays"))
	case "cloudflare":
		return m.confirmFirst("Remove the Cloudflare token?", "Turn tunnels off first: without it ops cannot delete them in Cloudflare.", "remove", true, m.setKeys(&blank, nil))
	case "tailscale":
		return m.confirmFirst("Remove the Tailscale key?", "The next security change of a server opens SSH on every interface again.", "remove", true, m.setKeys(nil, &blank))
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
