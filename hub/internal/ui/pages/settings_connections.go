package pages

import (
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/ui/components"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/link"
)

// ConnectionsView is Settings, Connections: the clients you let in from the
// browser, the CI tokens of this workspace you see, and the form for a new
// CI token.
type ConnectionsView struct {
	Connections []model.Connection
	// WorkspaceNames names the workspace of each connection, shown when you
	// are in more than one.
	WorkspaceNames map[string]string
	CITokens       []model.CIToken
	// Apps are the apps a new CI token can be limited to.
	Apps []model.App
	Form CITokenForm
	Now  time.Time
	// Confirm asks the person to confirm before more rights or a new CI
	// token.
	Confirm ConfirmView
}

// ConfirmView is the card that asks the person to prove they are at the
// browser before giving more rights or making a CI token: with their
// passkey, or without one by signing in again. Needed is false while a
// confirmation or a sign-in from the last five minutes counts.
type ConfirmView struct {
	Needed  bool
	Passkey bool
	// Next is the page to come back to after the passkey.
	Next string
	// Error says why the confirmation did not work.
	Error string
}

// CITokenForm is the state of the form for a new CI token, with one message
// per invalid field in Errors.
type CITokenForm struct {
	Name   string
	App    string
	Expiry string
	Rights model.Rights
	Errors map[string]string
}

// CITokenExpiryNever is the form value for a token that never expires.
const CITokenExpiryNever = "never"

// NewCITokenForm is an empty form: the default rights, every app, 90 days.
func NewCITokenForm() CITokenForm {
	return CITokenForm{Rights: model.DefaultRights, Expiry: "90", Errors: map[string]string{}}
}

var ciTokenExpiryOptions = []components.Option{
	{Value: "7", Label: "7 days"},
	{Value: "30", Label: "30 days"},
	{Value: "90", Label: "90 days"},
	{Value: "365", Label: "1 year"},
	{Value: CITokenExpiryNever, Label: "Never"},
}

func (v ConnectionsView) appOptions() []components.Option {
	options := []components.Option{{Value: "", Label: "Every app"}}
	for _, app := range v.Apps {
		options = append(options, components.Option{Value: app.Name, Label: app.Name})
	}
	return options
}

// connectionNote is the line under a connection: what it may do, when it
// was last used, and its workspace when you have several.
func (v ConnectionsView) connectionNote(connection model.Connection) string {
	parts := []string{rightsNote(connection.Rights), usedNote(connection.LastUsedAt, v.Now)}
	if len(v.WorkspaceNames) > 1 {
		parts = append(parts, v.WorkspaceNames[connection.WorkspaceID])
	}
	return strings.Join(parts, " · ")
}

// ciTokenNote is the line under a CI token: what it may do, the app it is
// for, when it was last used, and who made it when it is not yours.
func ciTokenNote(token model.CIToken, userID string, now time.Time) string {
	parts := []string{rightsNote(token.Rights)}
	if token.AppName != nil {
		parts = append(parts, "only "+*token.AppName)
	}
	parts = append(parts, usedNote(token.LastUsedAt, now))
	if token.IsExpired(now) {
		parts = append(parts, "expired")
	}
	if token.UserID != userID {
		parts = append(parts, "by "+token.MakerName)
	}
	return strings.Join(parts, " · ")
}

// rightsNote names rights in a list: "read, deploy, env".
func rightsNote(rights model.Rights) string {
	return strings.Join(rights.List(), ", ")
}

func usedNote(lastUsed *time.Time, now time.Time) string {
	if lastUsed == nil {
		return "never used"
	}
	return "used " + human.Ago(*lastUsed, now)
}

// ConnectionView is the page of one connection.
type ConnectionView struct {
	Connection    model.Connection
	WorkspaceName string
	Now           time.Time
	Confirm       ConfirmView
}

// CITokenView is the page of one CI token. Mine says you made it, so you
// may change its rights.
type CITokenView struct {
	Token   model.CIToken
	Mine    bool
	Now     time.Time
	Confirm ConfirmView
}

// linkID is the ID of a connection or CI token in its link.
func linkID(id string) string { return link.ID(id) }

func expiresNote(expiresAt *time.Time, now time.Time) string {
	if expiresAt == nil {
		return "Never"
	}
	if !expiresAt.After(now) {
		return "Expired " + expiresAt.Local().Format("Jan 2, 2006")
	}
	return expiresAt.Local().Format("Jan 2, 2006")
}

func agoNote(at time.Time, now time.Time) string {
	return human.Ago(at, now)
}

// rightSwitchID is the ID of a right's switch on a page.
func rightSwitchID(prefix string, right model.Rights) string {
	return prefix + "-" + right.Name()
}

// editableRights are the rights a switch turns on and off: all but read,
// which is always on.
var editableRights = model.AllRights[1:]

// confirmAction is where the card of ConfirmView sends its form: the
// passkey's answer, or signing in again.
func confirmAction(base string, passkey bool) string {
	if passkey {
		return base + "/settings/connections/confirm"
	}
	return base + "/settings/connections/reauth"
}
