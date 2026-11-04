# ops

Deploy SQLite apps to servers you own. Deploys, backups, data and logs in one binary: web, CLI, terminal UI and MCP.

```bash
ops login              # sign in with an API token
ops servers add web-1  # prints the one-line install for your server
ops deploy             # ship the current directory
ops                    # the live terminal UI
```

The agent on your server only dials out. Nothing listens for ops.

"ops" is a working name and may change soon. Servers, the CLI and agents will move over by themselves; see the [FAQ](https://ops.goilerplate.com/docs/faq).

## Documentation

Everything else is in the docs, under `/docs` in the control plane: [ops.goilerplate.com/docs](https://ops.goilerplate.com/docs). They are the source of truth.

## Build

```bash
go install github.com/axadrn/moin/cmd/ops@latest
```

The control plane is in [`controlplane`](controlplane).

## License

AGPL-3.0, all of it. See [LICENSE](LICENSE) and [CONTRIBUTING.md](CONTRIBUTING.md). The license covers the code, not the name or the logo: see [TRADEMARKS.md](TRADEMARKS.md).
