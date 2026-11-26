package daemon

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/axadrn/moin/protocol"
)

// The daemon writes every request of the hub to its log, which systemd keeps
// in the journal: journalctl -u moin-daemon. One line says what the hub
// asked (logAsked), one what came of it (logAnswer), so the owner of the
// server sees what a hub did there, also one that someone broke into.

// asked is what a request of the hub names, for the log. Only these fields
// are read, so an env value, a secret, a password, SQL, the arguments of a
// command or a file never reaches the log. Add a field only when it is safe
// to show; TestJournalShowsOnlyWhatItMay checks every request.
type asked struct {
	App          string                      `json:"app"`
	DeployID     string                      `json:"deploy_id"`
	Number       int                         `json:"number"`
	FromNumber   int                         `json:"from_number"`
	SourceNumber int                         `json:"source_number"`
	SourceURL    string                      `json:"source_url"`
	Image        string                      `json:"image"`
	Catalog      string                      `json:"catalog"`
	Service      string                      `json:"service"`
	Argv         []string                    `json:"argv"`
	Database     string                      `json:"database"`
	Write        bool                        `json:"write"`
	Copy         string                      `json:"copy"`
	Undo         string                      `json:"undo"`
	Name         string                      `json:"name"`
	Value        json.RawMessage             `json:"value"`
	Set          map[string]json.RawMessage  `json:"set"`
	Remove       []string                    `json:"remove"`
	Version      string                      `json:"version"`
	URL          string                      `json:"url"`
	SSH          string                      `json:"ssh"`
	SSHTailscale bool                        `json:"ssh_on_tailscale"`
	Firewall     bool                        `json:"firewall"`
	Bucket       *protocol.Bucket            `json:"bucket"`
	Storage      *protocol.Bucket            `json:"storage"`
	AppStorage   map[string]*protocol.Bucket `json:"app_storage"`
}

// logAsked writes what the hub asked: the type of the request and what it
// names, such as the app, the deploy number or the command.
func logAsked(message protocol.Message) {
	var a asked
	_ = json.Unmarshal(message.Data, &a)
	line := []any{"type", clip(string(message.Type))}
	add := func(key string, value any, ok bool) {
		if ok {
			line = append(line, key, clip(fmt.Sprint(value)))
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
	// Whether a secret goes, never its value.
	add("removed", len(a.Value) == 0 || string(a.Value) == `""`, message.Type == protocol.TypeSecretSet)
	// The env keys, never their values.
	add("set", slices.Sorted(maps.Keys(a.Set)), len(a.Set) > 0)
	add("remove", a.Remove, len(a.Remove) > 0)
	add("version", a.Version, a.Version != "")
	add("url", a.URL, a.URL != "")
	server := message.Type == protocol.TypeServerConfig
	add("ssh", a.SSH, server)
	add("ssh_on_tailscale", a.SSHTailscale, server)
	add("firewall", a.Firewall, server)
	// Where backups go, never the keys.
	if a.Bucket != nil {
		add("bucket", bucketPlace(a.Bucket), true)
	}
	if a.Storage != nil {
		add("bucket", bucketPlace(a.Storage), true)
	}
	var apps []string
	for _, app := range slices.Sorted(maps.Keys(a.AppStorage)) {
		if bucket := a.AppStorage[app]; bucket != nil {
			apps = append(apps, app+"="+bucketPlace(bucket))
		}
	}
	add("app_buckets", apps, len(apps) > 0)
	slog.Info("hub asked", line...)
}

// logAnswer writes what came of a request: done, or not done and why. It
// runs for every message the daemon sends and skips those that answer
// nothing, such as metrics.
func logAnswer(messageType protocol.MessageType, requestID string, data any) {
	requestID = clip(requestID)
	switch answer := data.(type) {
	case protocol.Error:
		slog.Warn("not done", "request", requestID, "why", why(answer.Message))
	case protocol.DeployDone:
		if answer.OK {
			slog.Info("done", "deploy", clip(answer.DeployID))
		} else {
			slog.Warn("not done", "deploy", clip(answer.DeployID), "why", why(answer.Error))
		}
	case protocol.ExecDone:
		if answer.Error != "" {
			slog.Warn("not done", "request", requestID, "why", why(answer.Error))
		} else {
			slog.Info("done", "request", requestID, "exit_code", answer.ExitCode, "timed_out", answer.TimedOut)
		}
	case protocol.ServerConfigResult:
		// The hub sends the config on its own when the daemon connects,
		// without a request: that answer gets no line.
		if len(answer.Problems) == 0 {
			if requestID != "" {
				slog.Info("done", "request", requestID)
			}
			break
		}
		// What each problem is about, such as SSH: the rest may carry
		// what a command printed.
		var parts []string
		for _, problem := range answer.Problems {
			part, _, _ := strings.Cut(problem, ":")
			parts = append(parts, part)
		}
		slog.Warn("not fully done", "request", requestID, "problems", clip(strings.Join(parts, ", ")))
	default:
		if requestID != "" && messageType != protocol.TypeExecOutput {
			slog.Info("done", "request", requestID)
		}
	}
}

// why is the reason a request was not done, for the log: a refusal, or
// another sentence of the daemon's own. Any other error may carry what a
// command printed or a compose setting, an env value among them, so it is
// only "failed": the hub shows the whole error.
func why(text string) string {
	switch {
	case strings.HasPrefix(text, refusedByRules), text == errUpdating.Error(), strings.HasPrefix(text, "this daemon does not know "):
		return clip(text)
	case strings.Contains(text, beyondTheApp):
		return "refused: " + beyondTheApp + ", without server-access: on"
	}
	return "failed"
}

// clip keeps a value in the log at 200 bytes or fewer, whole characters.
func clip(text string) string {
	if len(text) <= 200 {
		return text
	}
	end := 200
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}
