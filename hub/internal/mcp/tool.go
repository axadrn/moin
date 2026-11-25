package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/axadrn/moin/hub/internal/service"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Caller is who called a tool: the user and the connection or CI token the
// request authenticated with.
type Caller struct {
	User   *model.User
	Bearer *model.Bearer
	// Source is how the call came in, for the activity: MCP, the
	// connection or CI token by name, the client IP and user agent.
	Source service.Source
}

// ToolFunc is the body of a tool. It gets the caller and the decoded input
// and returns the structured output. Errors from services are reported with
// the JSON API codes.
type ToolFunc[In, Out any] func(ctx context.Context, caller Caller, input In) (Out, error)

// Add registers a tool that needs right, one right or two that must both
// be on. That right is the one place that says it: Add ends the
// description with "Needs the deploy right.", puts the rights in the tool's
// _meta, and checks them before the body runs. A connection without them
// gets missing_right.
func Add[In, Out any](server *mcpsdk.Server, tool *mcpsdk.Tool, right model.Rights, body ToolFunc[In, Out]) {
	tool.Description = strings.TrimSpace(tool.Description + " " + needs(right))
	if tool.Meta == nil {
		tool.Meta = mcpsdk.Meta{}
	}
	tool.Meta["rights"] = right.List()
	mcpsdk.AddTool(server, tool, handler(right, body))
}

// needs is the sentence of a tool's description that names its right.
func needs(right model.Rights) string {
	names := right.List()
	if len(names) == 1 {
		return "Needs the " + names[0] + " right."
	}
	return "Needs the " + strings.Join(names, " and ") + " rights."
}

func handler[In, Out any](right model.Rights, tool ToolFunc[In, Out]) mcpsdk.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, input In) (*mcpsdk.CallToolResult, Out, error) {
		var empty Out
		caller := Caller{User: ctxkeys.User(ctx), Bearer: ctxkeys.Bearer(ctx), Source: service.RequestSource(ctx)}
		caller.Source.Via = service.ViaMCP
		if caller.User == nil || caller.Bearer == nil {
			return nil, empty, &ToolError{Code: "unauthenticated", Message: unauthenticated}
		}
		if caller.Bearer.AppID != "" {
			return nil, empty, &ToolError{Code: "app_only", Message: appOnly}
		}
		if missing := caller.Bearer.Rights.Missing(right); missing != 0 {
			detail := api.MissingRight(*caller.Bearer, missing)
			return nil, empty, &ToolError{Code: detail.Code, Message: detail.Message, Right: detail.Right}
		}
		output, err := tool(ctx, caller, input)
		if err != nil {
			return nil, empty, toolError(err)
		}
		return result(output), output, nil
	}
}

const (
	unauthenticated = "Log in with OAuth, or send a CI token, which starts with pat_, as Authorization: Bearer <token>."
	appOnly         = "This CI token works only for its app, through the API. Make a CI token for every app, or connect in the browser."
)

// Summarizer is implemented by tool outputs that describe themselves in one
// sentence. The sentence comes before the JSON in the text content, so
// clients that only read text still get both.
type Summarizer interface {
	Summary() string
}

// result adds the summary of output to the text content. Without a summary
// the SDK fills the text content with the JSON of output.
func result(output any) *mcpsdk.CallToolResult {
	summarizer, ok := output.(Summarizer)
	if !ok {
		return nil
	}
	content := []mcpsdk.Content{&mcpsdk.TextContent{Text: summarizer.Summary()}}
	data, err := json.Marshal(output)
	if err == nil {
		content = append(content, &mcpsdk.TextContent{Text: string(data)})
	}
	return &mcpsdk.CallToolResult{Content: content}
}

// ToolError is a failed tool call. Its text is the error envelope of the JSON
// API, so AI agents see the same codes and field messages.
type ToolError struct {
	Code    string
	Message string
	Fields  map[string]string
	// Right is the right a missing_right error names.
	Right string
}

func (e *ToolError) Error() string {
	data, err := json.Marshal(api.ErrorBody{Error: api.ErrorDetail{Code: e.Code, Message: e.Message, Fields: e.Fields, Right: e.Right}})
	if err != nil {
		return e.Message
	}
	return string(data)
}

// InvalidField returns a validation_failed error for one input field.
func InvalidField(field, message string) error {
	return &ToolError{Code: "validation_failed", Message: api.ValidationMessage, Fields: map[string]string{field: message}}
}

// toolError maps a service or repository error to the code the JSON API
// answers for it. Unknown errors are logged and reported as internal.
func toolError(err error) error {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr
	}
	_, detail, known := api.ErrorFor(err)
	if !known {
		slog.Error("mcp tool failed", "error", err)
	}
	return &ToolError{Code: detail.Code, Message: detail.Message, Fields: detail.Fields}
}

// actor is the caller's user in the workspace of the connection or CI
// token, through MCP.
func (c Caller) actor() service.Actor {
	return service.Actor{WorkspaceID: c.Bearer.WorkspaceID, UserID: c.User.ID, Source: c.Source}
}
