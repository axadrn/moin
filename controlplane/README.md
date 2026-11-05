# moin control plane

The hosted side of moin: accounts, billing, servers, apps, deploys, backups and data. Agents on the users' servers dial in over one WebSocket. The agent, CLI, TUI and protocol are at the root of this repo.

Read `DESIGN.md` for how it looks. Product docs are in `content/docs`, served at `/docs`: the only source of truth for how moin works.

## Develop

```bash
cp .env.example .env   # then fill in your own development bucket
task dev               # the app on a free port
```

Development uses a real bucket of its own, such as `moin-development` on R2, so
it runs the same storage code as production.

Without Cloudflare credentials, emails go to the log. The control plane and the `moin` binary are one Go module: a change to the agent, the CLI or the protocol shows up here right away.

The image builds from the repo root: `docker build .`

## License

AGPL-3.0. See [LICENSE](../LICENSE).
