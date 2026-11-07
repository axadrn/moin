# AGENTS.md

All of moin in one repo, open source under AGPL-3.0: the `moin` binary (daemon,
CLI, TUI) at the root and the hub in `hub/`, with its own
`AGENTS.md`. Never commit
secrets, customer data, `.env` files or anything private: names of companies,
addresses, personal details.

## Rules

- One binary. `moin` is the TUI, `moin <command>` the CLI, `moin daemon` the daemon.
- The daemon only dials out. Never add a listener for moin.
- Clients (CLI, TUI) only call the hub API (`/api/v1`) and its SSE streams. They never talk to a daemon.
- Every feature works in the web UI, the hub API, the CLI and an MCP tool; it is done when each one works end to end. The TUI covers the daily work and opens the web for setup. The docs page "Where things work" (`hub/content/docs/reference/surfaces.md`) lists what each surface does; update it with every feature.
- Every feature is documented in the hub docs (`hub/content/docs`), the only source of truth. Update them in the same change, and `usage` in `internal/cli/cli.go` for commands. `README.md` stays short and punchy, like shadcn/ui's: what moin is, the first commands, a pointer to the docs, the license. It never repeats the docs.
- `protocol` is the contract with the hub. Change it in a way old daemons keep working, or bump `protocol.Version`.
- The product name lives only in `internal/build.Name`.
- Words: one thing, one word, in every layer, from the list in `hub/DESIGN.md` (Words): hub, daemon, server, app, deploy, backup, check, uptime, AI agent. Not "node", "host" or "control plane".
- Standard library first. Current dependencies: Bubble Tea v2, Lip Gloss v2, `coder/websocket`, `x/term`. Add one only when the standard library does not solve it well.
- Keep it small and direct. No interfaces with one implementation, no frameworks.
- `gofmt` clean, `go vet ./...` clean.
- Commits follow Conventional Commits: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, with an optional scope like `feat(cli):`. A short first line, then a sentence or two of why when it helps.
- Commits and pull requests carry no AI attribution: no `Co-Authored-By` for a tool, no "Generated with" line.
- Push, deploy, release. A push to `main` runs no workflow; test locally first. The Deploy workflow ships: tests, the box, for a tag the GitHub release from `hub/content/changelog.md`, then the hosted hub updates itself through Tailscale. Deploy for typos, docs and design: Actions, Deploy, Run workflow. Release for a feature or a fix: its notes in the changelog, then the tag `v…`.
- Release notes follow one format, the same in moin, goilerplate and shadcn-templ, so the eye finds things fast. The same text is the GitHub release and the newest section of `hub/content/changelog.md` (`## v…`, the date like `November 3, 2026`, then the notes), which `/changelog` shows and a test checks:
  - First line: `**In short:**` and one sentence, what is new and why to update.
  - Then these sections in this order, empty ones left out: `### New` (what you can do now), `### Changed` (what behaves differently, everything that could surprise), `### Fixed` (what was broken and works now), `### Removed` (what is gone, and what to use instead), `### Upgrade` (always there: how to update, the exact command, one line).
  - One bullet per thing: `- **Name**: one sentence of what the user gets.` Commands in backticks. What the user sees, never internals or commit hashes.
  - A big turning point may add a short `### Why` under the first line. Longer stories go in a blog post or the docs, linked.

## Test

```bash
go build ./... && go vet ./... && go test ./...
```

Then check what you changed, and only that:

- A small change: the Go tests, plus a quick look at the changed page in a
  browser, or the changed command in the CLI or TUI, against a local hub.
- A release with many changes, or a change to the layout every page uses: the
  full end-to-end run in Chromium and WebKit, at phone and desktop width.
