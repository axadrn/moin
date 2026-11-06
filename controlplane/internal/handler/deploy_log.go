package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// DeployLog streams the log of a deploy as HTML for the web (hx-sse):
// every message is the lines to append, sorted into steps and folds; at
// the end the state and the event "done". The API streams the same log as
// JSON for the CLI, the TUI and MCP.
func (h *AppsHandler) DeployLog(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	appName := r.PathValue("app")
	deploy, lines, changes, stop, err := h.apps.Follow(workspace.ID, appName, number)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer stop()
	stream := startStream(w)
	var folder pages.LogFolder
	sent := 0
	send := func() bool {
		all := lines()
		if len(all) == sent {
			return true
		}
		chunk := folder.Add(all[sent:])
		sent = len(all)
		return stream.html(r, pages.DeployLogChunk(chunk))
	}
	if !send() {
		return
	}
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for changes != nil {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-changes:
			if !send() {
				return
			}
			if !open {
				changes = nil
			}
		case <-heartbeat.C:
			if !stream.comment() {
				return
			}
		}
	}
	finished, err := h.apps.DeployByNumber(workspace.ID, appName, deploy.Number)
	if err != nil {
		return
	}
	end := folder.End(finished.Number, finished.Status, finished.Error)
	state := finished.Status
	if state == model.DeployRunning {
		// The log stream ended while the deploy still runs.
		state = "stopped"
	}
	if !stream.html(r, pages.DeployLogChunk(end), pages.DeployLogStateOOB(state)) {
		return
	}
	stream.event("done")
}

// sseStream writes server-sent events.
type sseStream struct {
	w          http.ResponseWriter
	controller *http.ResponseController
}

func startStream(w http.ResponseWriter) sseStream {
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return sseStream{w: w, controller: controller}
}

// html sends components as one message for htmx to swap.
func (s sseStream) html(r *http.Request, components ...templ.Component) bool {
	var body bytes.Buffer
	for _, component := range components {
		if err := component.Render(r.Context(), &body); err != nil {
			return false
		}
	}
	if strings.TrimSpace(body.String()) == "" {
		return true
	}
	var message strings.Builder
	for line := range strings.SplitSeq(body.String(), "\n") {
		message.WriteString("data: " + line + "\n")
	}
	message.WriteString("\n")
	return s.write(message.String())
}

func (s sseStream) event(name string) bool {
	return s.write(fmt.Sprintf("event: %s\ndata: \n\n", name))
}

func (s sseStream) comment() bool {
	return s.write(": heartbeat\n\n")
}

func (s sseStream) write(text string) bool {
	_, err := fmt.Fprint(s.w, text)
	return err == nil && s.controller.Flush() == nil
}
