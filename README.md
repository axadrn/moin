<p align="center">
  <img src="controlplane/assets/img/og.png" alt="moin. Deploy anything. Lose nothing." width="100%">
</p>

Deploy anything. Lose nothing. Your apps on your servers. Backups on your schedule, checked every day. Open source, for SQLite and Postgres, with web, CLI, terminal UI and MCP.

```bash
moin login              # log in through the browser
moin servers add web-1  # prints the one-line install for your server
moin deploy             # ✓ moin. your app is live
moin                    # the live terminal UI
```

The agent on your server only dials out. Nothing listens for moin.

## Documentation

Everything else is in the docs: [moin.run/docs](https://moin.run/docs), or `/docs` on your own control plane. They are the source of truth.

## Build

```bash
go install github.com/axadrn/moin/cmd/moin@latest
```

The control plane is in [`controlplane`](controlplane).

## License

AGPL-3.0, all of it. See [LICENSE](LICENSE) and [CONTRIBUTING.md](CONTRIBUTING.md). The license covers the code, not the name or the logo: see [TRADEMARKS.md](TRADEMARKS.md).
