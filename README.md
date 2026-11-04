# moin

Deploy SQLite apps to servers you own. Deploys, backups, data and logs in one binary: web, CLI, terminal UI and MCP.

```bash
moin login              # sign in with an API token
moin servers add web-1  # prints the one-line install for your server
moin deploy             # ship the current directory
moin                    # the live terminal UI
```

The agent on your server only dials out. Nothing listens for moin.

"moin" is a working name and may change soon. Servers, the CLI and agents will move over by themselves; see the [FAQ](https://moin.run/docs/faq).

## Documentation

Everything else is in the docs, under `/docs` in the control plane: [moin.run/docs](https://moin.run/docs). They are the source of truth.

## Build

```bash
go install github.com/axadrn/moin/cmd/ops@latest
```

The control plane is in [`controlplane`](controlplane).

## License

AGPL-3.0, all of it. See [LICENSE](LICENSE) and [CONTRIBUTING.md](CONTRIBUTING.md). The license covers the code, not the name or the logo: see [TRADEMARKS.md](TRADEMARKS.md).
