package api

import (
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/protocol"
)

// ExecRequest is the body of POST /api/v1/apps/{app}/exec: the command as
// a list of words, or as one line split the way a shell quotes words,
// never read by a shell; the service, the app's main one when empty; and
// the timeout in seconds.
type ExecRequest struct {
	Service string   `json:"service"`
	Command []string `json:"command"`
	Line    string   `json:"line"`
	Timeout int      `json:"timeout"`
}

// Exec runs a command in a container of the app and answers with server-
// sent events: "output" with each piece the command writes, then "done"
// with how it ended, or "error". A request refused before it runs answers
// with the usual error envelope instead.
func (h *AppsHandler) Exec(w http.ResponseWriter, r *http.Request) {
	var request ExecRequest
	if !Decode(w, r, &request) {
		return
	}
	if len(request.Command) == 0 && request.Line != "" {
		argv, err := protocol.SplitCommand(request.Line)
		if err != nil {
			ValidationError(w, map[string]string{"line": "The command: " + err.Error() + "."})
			return
		}
		request.Command = argv
	}
	var events *eventStream
	open := func() *eventStream {
		if events == nil {
			stream := startEvents(w)
			events = &stream
		}
		return events
	}
	result, err := h.apps.Exec(r.Context(), ctxkeys.WorkspaceMember(r.Context()), source(r), r.PathValue("app"), service.ExecInput{
		Service: request.Service,
		Argv:    request.Command,
		Timeout: request.Timeout,
	}, func(data string) {
		open().send("output", map[string]string{"data": data})
	})
	if err != nil {
		if events == nil {
			ServiceError(w, err)
			return
		}
		_, detail, known := ErrorFor(err)
		if !known {
			detail = ErrorDetail{Code: "internal", Message: "Something went wrong. Please try again."}
		}
		events.send("error", ErrorBody{Error: detail})
		return
	}
	open().send("done", result)
}
