package mcp

import (
	"context"
	"strings"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/model"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerDomainTools registers the tools of how visitors reach an app:
// its domains, its instant address and its one password.
func registerDomainTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	Add(server, newTool("check_domains", "Check domains",
		"Looks up every domain of an app and says whether it points at the address of its server, which the DNS record must point at.",
		readOnly("")), model.RightRead, member(services, tools.checkDomains))
	Add(server, newTool("set_domains", "Set domains",
		"Replaces the domains of an app and the canonical one the others redirect to (empty for none). Saving never restarts the app: the change waits for restart_app, a start or a deploy. With service, replaces the domains of that service of compose.yml instead of the main one's; canonical is for the main service. HTTPS is always on.",
		change(false, true)), model.RightDeploy, changes(services, tools.setDomains))
	Add(server, newTool("get_instant", "Get the instant address",
		"Returns the instant address of an app, https://<app>.<server IPv4 with dashes>.<provider>, which works without a domain, or why it has none, such as a server behind a Cloudflare Tunnel, which needs ports 80 and 443 open instead. The app's password guards it like the app's domains while password is true (get_app).",
		readOnly("")), model.RightRead, member(services, tools.getInstant))
	Add(server, newTool("set_instant", "Set the instant address",
		"Picks the provider of the instant address of an app, sslip.io or nip.io, or off. The change waits for restart_app, a start or a deploy.",
		change(false, true)), model.RightDeploy, changes(services, tools.setInstant))
	Add(server, newTool("set_password", "Turn the password on",
		"Turns on the app's one password: its domains and its instant address ask for it with basic auth. With a password of 8 to 72 characters it sets that one; without one moin makes one and returns it in password: the one time moin shows it, so give it to the user right away. username is moin when empty. The change waits for restart_app, a start or a deploy.",
		change(false, true)), model.RightEnv, changes(services, tools.setPassword))
	Add(server, newTool("remove_password", "Turn the password off",
		"Turns off the app's password: no domain of the app asks for one after restart_app, a start or a deploy, so anyone with the link opens it. Ask the user first.",
		change(true, true)), model.RightEnv, changes(services, tools.removePassword))
}

// DomainChecks is the output of check_domains.
type DomainChecks struct {
	api.DomainChecks
}

func (d DomainChecks) Summary() string {
	parts := []string{}
	for _, check := range d.Items {
		state := "does not point at the server yet: set its " + check.Advice
		if check.Points {
			state = "points at the server"
		}
		parts = append(parts, check.Domain+" "+state)
	}
	if len(parts) == 0 {
		return "The app has no domains."
	}
	return strings.Join(parts, "; ") + "."
}

func (t moinTools) checkDomains(ctx context.Context, caller Caller, input AppInput) (DomainChecks, error) {
	checks, err := t.services.Apps.CheckDomains(ctx, caller.Bearer.WorkspaceID, input.App)
	if err != nil {
		return DomainChecks{}, err
	}
	return DomainChecks{api.NewDomainChecks(checks)}, nil
}

// DomainsInput is the input of set_domains.
type DomainsInput struct {
	App       string   `json:"app" jsonschema:"the app name or ID"`
	Domains   []string `json:"domains" jsonschema:"every domain the app answers on, like my-app.example.com and www.my-app.example.com"`
	Canonical string   `json:"canonical,omitempty" jsonschema:"the domain the others redirect to; empty for none"`
	Service   string   `json:"service,omitempty" jsonschema:"a service of compose.yml other than the main one, to set its domains; empty for the main one"`
}

// Change is the output of changes of the domains.
type Change struct {
	api.Change
}

func (c Change) Summary() string {
	if c.Unchanged {
		return "Nothing changed."
	}
	return "Saved. The app takes it with its next restart, start or deploy (restart_app)."
}

func (t moinTools) change(caller Caller, app string, changed bool) (Change, error) {
	answer, err := api.NewChange(t.services.Apps, caller.Bearer.WorkspaceID, app, changed)
	return Change{answer}, err
}

func (t moinTools) setDomains(_ context.Context, caller Caller, input DomainsInput) (Change, error) {
	changed, err := t.services.Apps.SetDomains(caller.actor(), input.App, input.Service, input.Domains, input.Canonical)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, changed)
}

// InstantInput is the input of get_instant.
type InstantInput struct {
	App string `json:"app" jsonschema:"the app name or ID"`
}

// Instant is the output of get_instant.
type Instant struct {
	api.Instant
}

func (i Instant) Summary() string {
	if i.URL == "" {
		return "The app has no instant address. " + i.Reason
	}
	return i.URL + " via " + i.Provider + "."
}

func (t moinTools) getInstant(_ context.Context, caller Caller, input InstantInput) (Instant, error) {
	instant, err := t.services.Apps.Instant(caller.Bearer.WorkspaceID, input.App)
	if err != nil {
		return Instant{}, err
	}
	return Instant{api.NewInstant(instant)}, nil
}

// SetInstantInput is the input of set_instant.
type SetInstantInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Provider string `json:"provider" jsonschema:"sslip.io, nip.io or off"`
}

func (t moinTools) setInstant(_ context.Context, caller Caller, input SetInstantInput) (Change, error) {
	changed, err := t.services.Apps.SetInstant(caller.actor(), input.App, input.Provider)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, changed)
}

// PasswordInput is the input of set_password.
type PasswordInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Username string `json:"username,omitempty" jsonschema:"who signs in; moin when empty"`
	Password string `json:"password,omitempty" jsonschema:"8 to 72 characters; empty lets moin make one"`
}

// NewPassword is the output of set_password.
type NewPassword struct {
	api.NewPassword
}

func (p NewPassword) Summary() string {
	summary := Change{p.Change}.Summary()
	if p.Password != "" {
		summary += " The password moin made is in password: shown this once, give it to the user now."
	}
	return summary
}

func (t moinTools) setPassword(_ context.Context, caller Caller, input PasswordInput) (NewPassword, error) {
	made, changed, err := t.services.Apps.SetPassword(caller.actor(), input.App, input.Username, input.Password)
	if err != nil {
		return NewPassword{}, err
	}
	answer, err := t.change(caller, input.App, changed)
	return NewPassword{api.NewPassword{Change: answer.Change, Password: made}}, err
}

func (t moinTools) removePassword(_ context.Context, caller Caller, input AppInput) (Change, error) {
	changed, err := t.services.Apps.RemovePassword(caller.actor(), input.App)
	if err != nil {
		return Change{}, err
	}
	return t.change(caller, input.App, changed)
}
