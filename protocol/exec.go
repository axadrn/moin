package protocol

// ExecStart runs Argv in a container of Service of App, as a list: no shell
// reads it. The agent stops it after Timeout seconds.
type ExecStart struct {
	App     string   `json:"app"`
	Service string   `json:"service"`
	Argv    []string `json:"argv"`
	Timeout int      `json:"timeout"`
}

// ExecOutput is a piece of what the command wrote, stdout and stderr as
// they came.
type ExecOutput struct {
	Data string `json:"data"`
}

// ExecDone ends a run: the exit code, or the error that kept the command
// from running. Truncated says the output passed ExecMaxOutput and the rest
// was dropped; TimedOut that the agent stopped it.
type ExecDone struct {
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated,omitempty"`
	TimedOut  bool   `json:"timed_out,omitempty"`
	Error     string `json:"error,omitempty"`
}

const (
	// ExecMaxOutput is how much a run may write before the rest is dropped.
	ExecMaxOutput = 1 << 20
	// ExecTimeout is how long a run may take unless asked otherwise, and
	// ExecMaxTimeout how long it may take at most, in seconds.
	ExecTimeout    = 10 * 60
	ExecMaxTimeout = 60 * 60
)
