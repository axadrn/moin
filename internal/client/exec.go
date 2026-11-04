package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ExecResult is how a command ended.
type ExecResult struct {
	Service   string `json:"service"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated"`
	TimedOut  bool   `json:"timed_out"`
}

// Exec runs a command in a container of an app and hands what it writes to
// output as it comes. argv is the command as words; line, used when argv
// is empty, is one line the control plane splits like a shell quotes it.
func (c *Client) Exec(ctx context.Context, app, service string, argv []string, line string, timeout int, output func(string)) (ExecResult, error) {
	body := map[string]any{"service": service, "command": argv, "line": line, "timeout": timeout}
	var result ExecResult
	done := false
	err := c.streamRequest(ctx, http.MethodPost, appPath(app, "exec"), body, func(event string, data []byte) error {
		switch event {
		case "output":
			var piece struct {
				Data string `json:"data"`
			}
			if err := json.Unmarshal(data, &piece); err != nil {
				return err
			}
			output(piece.Data)
		case "done":
			done = true
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			return errStreamDone
		case "error":
			var problem struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(data, &problem)
			return &Error{Code: problem.Error.Code, Message: problem.Error.Message}
		}
		return nil
	})
	if err == nil && !done {
		return result, errors.New("the connection ended before the command did")
	}
	return result, err
}
