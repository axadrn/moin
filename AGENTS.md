# AGENTS.md

This repo is public (Apache 2.0). The control plane lives in the private repo
`github.com/axadrn/ops-cloud`; its `PLAN.md` is the plan for both. Nothing
closed goes here: no control plane code, no secrets, no customer data.

## Rules

- One binary. `ops` is the TUI, `ops <command>` the CLI, `ops agent` the agent.
- The agent only dials out. Never add a listener for ops.
- Clients (CLI, TUI) only call the control plane API (`/api/v1`) and its SSE streams. They never talk to an agent.
- Every feature works the same in web, CLI and TUI. A slice is done when all three work end to end.
- `protocol` is the contract with the control plane. Change it in a way old agents keep working, or bump `protocol.Version`.
- The product name lives only in `internal/build.Name`.
- Words: control plane, agent, server, app, deploy, backup. Not "node" or "host".
- Standard library first. Current dependencies: Bubble Tea v2, Lip Gloss v2, `coder/websocket`, `x/term`. Add one only when the standard library does not solve it well.
- Keep it small and direct. No interfaces with one implementation, no frameworks.
- `gofmt` clean, `go vet ./...` clean.

## Test

```bash
go build ./... && go vet ./... && go test ./...
```

Test changes end to end against a local control plane: the CLI, the TUI in a
real terminal and the web UI.
