# ops

Deploys, backups and data for SQLite apps on your own servers.

`ops` is one binary:

- `ops` opens the terminal UI.
- `ops <command>` is the CLI.
- `ops agent` runs on a server and holds one outbound connection to the control plane.

The web UI, the CLI and the TUI show the same data, live. All three talk to the
hosted control plane. Nothing on your server listens for ops.

`ops` is a working name and changes before the public beta.

## Install

Not released yet. Build it from source:

```bash
go install github.com/axadrn/ops/cmd/ops@latest
```

## Use

```bash
ops login              # paste a personal API token from the web UI
ops servers add web-1  # prints the install command for the server
ops servers            # NAME, STATUS, SYSTEM, AGENT, LAST SEEN
ops                    # live TUI: a add, x remove, q quit
```

Run the printed install command on the server as root. It downloads `ops`,
trades the one-time install token for an agent token and starts the
`ops-agent` systemd unit.

`OPS_URL` and `OPS_TOKEN` override the saved login. `OPS_STATE_DIR` moves the
agent state away from `/var/lib/ops`.

## Packages

| Path | What |
| --- | --- |
| `cmd/ops` | main |
| `internal/cli` | commands |
| `internal/tui` | terminal UI (Bubble Tea v2) |
| `internal/client` | control plane API client |
| `internal/agent` | the agent |
| `protocol` | messages between agent and control plane, imported by the control plane |

## License

Apache 2.0. See `LICENSE`.
