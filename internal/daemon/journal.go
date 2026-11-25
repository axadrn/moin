package daemon

import (
	"encoding/json"
	"log/slog"
	"net/url"
	"slices"

	"github.com/axadrn/moin/protocol"
)

// The daemon writes every request of the hub to its log, which systemd keeps
// in the journal: journalctl -u moin-daemon. One line says what the hub
// asked (logAsked), one what came of it (logAnswer), so the owner of the
// server sees what a hub did there, also one that someone broke into.

// asked is what a request of the hub names, for the log. It holds only
// these fields, so an env value, a secret, a password, SQL, the arguments
// of a command or a file never reaches the log. Add a field only when it
// is safe to show.
type asked struct {
	App          string                     `json:"app"`
	DeployID     string                     `json:"deploy_id"`
	Number       int                        `json:"number"`
	FromNumber   int                        `json:"from_number"`
	SourceNumber int                        `json:"source_number"`
	SourceURL    string                     `json:"source_url"`
	Image        string                     `json:"image"`
	Catalog      string                     `json:"catalog"`
	Service      string                     `json:"service"`
	Argv         []string                   `json:"argv"`
	Database     string                     `json:"database"`
	Write        bool                       `json:"write"`
	Copy         string                     `json:"copy"`
	Undo         string                     `json:"undo"`
	Name         string                     `json:"name"`
	Set          map[string]json.RawMessage `json:"set"`
	Remove       []string                   `json:"remove"`
	Version      string                     `json:"version"`
	URL          string                     `json:"url"`
}

// logAsked writes what the hub asked: the type of the request and what it
// names, such as the app, the deploy number or the command.
func logAsked(message protocol.Message) {
	var a asked
	_ = json.Unmarshal(message.Data, &a)
	line := []any{"type", message.Type}
	add := func(key string, value any, ok bool) {
		if ok {
			line = append(line, key, value)
		}
	}
	add("request", message.RequestID, message.RequestID != "")
	add("app", a.App, a.App != "")
	add("deploy", a.DeployID, a.DeployID != "")
	add("number", a.Number, a.Number != 0)
	add("from_number", a.FromNumber, a.FromNumber != 0)
	add("source_number", a.SourceNumber, a.SourceNumber != 0)
	// The address without its query: the repo and the commit.
	if link, err := url.Parse(a.SourceURL); err == nil && a.SourceURL != "" {
		add("source", link.Host+link.Path, true)
	}
	add("image", a.Image, a.Image != "")
	add("catalog", a.Catalog, a.Catalog != "")
	add("service", a.Service, a.Service != "")
	// The command's name, never its arguments.
	if len(a.Argv) > 0 {
		add("command", a.Argv[0], true)
	}
	add("database", a.Database, a.Database != "")
	add("write", a.Write, a.Write)
	add("copy", a.Copy, a.Copy != "")
	add("undo", a.Undo, a.Undo != "")
	add("name", a.Name, a.Name != "")
	// The env keys, never their values.
	keys := make([]string, 0, len(a.Set))
	for key := range a.Set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	add("set", keys, len(keys) > 0)
	add("remove", a.Remove, len(a.Remove) > 0)
	add("version", a.Version, a.Version != "")
	add("url", a.URL, a.URL != "")
	slog.Info("hub asked", line...)
}

// logAnswer writes what came of a request: done, or not done and why, such
// as "refused by the rules of this server". It runs for every message the
// daemon sends and skips those that answer nothing, such as metrics.
func logAnswer(messageType protocol.MessageType, requestID string, data any) {
	switch answer := data.(type) {
	case protocol.Error:
		slog.Warn("not done", "request", requestID, "why", answer.Message)
	case protocol.DeployDone:
		if answer.OK {
			slog.Info("done", "deploy", answer.DeployID)
		} else {
			slog.Warn("not done", "deploy", answer.DeployID, "why", answer.Error)
		}
	case protocol.ExecDone:
		if answer.Error != "" {
			slog.Warn("not done", "request", requestID, "why", answer.Error)
		} else {
			slog.Info("done", "request", requestID, "exit_code", answer.ExitCode, "timed_out", answer.TimedOut)
		}
	default:
		if requestID != "" && messageType != protocol.TypeExecOutput {
			slog.Info("done", "request", requestID)
		}
	}
}
