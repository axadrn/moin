# moin hub

The hosted side of moin: accounts, billing, servers, apps, deploys, backups and data. Daemons on the users' servers dial in over one WebSocket. The daemon, CLI, TUI and protocol are at the root of this repo.

Read `DESIGN.md` for how it looks. Product docs are in `content/docs`, served at `/docs`: the only source of truth for how moin works.

## Develop

```bash
cp .env.example .env   # then fill in your own development bucket
task dev               # the app on a free port
```

Development uses a real bucket of its own, such as `moin-development` on R2, so
it runs the same storage code as production.

Keep your settings in 1Password? Copy `.env.example` to `.env.tpl` and put
`op://vault/item/field` references in place of the secrets. `task dev` then
reads them fresh on every start, and no `.env` lies on disk.

Without Cloudflare credentials, emails go to the log. The hub and the `moin` binary are one Go module: a change to the daemon, the CLI or the protocol shows up here right away.

The image builds from the repo root: `docker build .`

## License

Open source: [what the licenses cover](https://moin.run/docs/faq#is-moin-open-source).
