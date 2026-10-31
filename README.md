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
ops                    # live TUI: tab servers/apps, d deploy, l logs, r rollback
```

Run the printed install command on the server as root. It downloads `ops`,
trades the one-time install token for an agent token and starts the
`ops-agent` systemd unit.

`OPS_URL` and `OPS_TOKEN` override the saved login. `OPS_STATE_DIR` moves the
agent state away from `/var/lib/ops`. Colors and status symbols show only on a
terminal without `NO_COLOR`; piped output is plain text with the same columns.

## Deploy

Run `ops deploy` in a repo. ops packs the files git knows (uncommitted
changes too, ignored files not), the agent builds and starts them with Docker
Compose on the server, checks them and keeps the last 5 releases.

| The repo has | ops does |
| --- | --- |
| `compose.yml` | uses it |
| only a `Dockerfile` | runs it as service `web` with `./data` mounted at `/data` |
| a Go module | also writes the Dockerfile; `PORT` and `DATA_DIR=/data` are set |

The first deploy creates the app on your only server, or on `--server`. A
failed start brings the previous release back. Paths the repo does not bring,
like `./data`, live outside the releases, so data survives every deploy.

Optional config lives in `compose.yml`:

```yaml
x-ops:
  app: blog            # default: the directory name
  server: web-1        # for the first deploy
  service: web         # gets the traffic; default: the only built service
  port: 8080
  health: /healthz     # must answer below 500 after the start
  database: /data/app.db
```

```bash
ops env set SECRET=… # stored encrypted, the next deploy gets it
ops env pull         # writes .env
ops run -- go run .  # runs locally with the app's env
ops logs             # last lines of every container
ops rollback         # the release before the live one, or: ops rollback v3
```

A rollback starts the old images with the env they had. The server needs
Docker with the Compose plugin.

## Build in CI

Build and push the image in CI, then let ops deploy it. The server keeps the
compose file of the running release and only pulls the new image.

```bash
ops registry set ghcr.io <user>   # once, for private images; token on stdin
ops hook                          # a secret URL for CI, shown once
curl -fsS -X POST "$OPS_DEPLOY_HOOK?image=ghcr.io/me/blog:$GITHUB_SHA"
ops deploy --image ghcr.io/me/blog:3f2a9c1   # the same by hand
```

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
