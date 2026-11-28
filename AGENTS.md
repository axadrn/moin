# AGENTS.md

All of moin in one repo, open source: the `moin` binary (CLI,
daemon, watcher) at the root and the website moin.run in `cloud/`, with
its own `AGENTS.md`. Never commit
secrets, customer data, `.env` files or anything private: names of companies,
addresses, personal details.

## Rules

- One binary. `moin <command>` is the CLI, `moin daemon` the daemon on a server, `moin watcher` the watcher.
- Two one-way roads. Commands go from the CLI to a server over SSH, with the user's key. Reports go from a daemon to its watchers. The daemon takes no commands and acts on nothing a watcher answers. The watcher is the only part that listens.
- One exact exception: after a copy, a watcher that takes copies, moin cloud, answers one address to upload it to, and the daemon puts that one encrypted copy there: https to a public address only, one PUT, no redirects. Nothing else a watcher answers is ever used.
- The CLI is the interface: no web app or API that commands a server, no MCP server. moin cloud's pages and API sign in, bill, show and receive. None of it reaches a server. Every reading command takes `--json`. A destructive one asks on a terminal and needs `--yes` otherwise. A feature is done when it works end to end in the CLI.
- Every feature is documented in the docs (`cloud/content/docs`), the only source of truth. Update them in the same change, and `usage` in `internal/cli/cli.go` for commands. `README.md` stays short and punchy, like shadcn/ui's: what moin is, the first commands, a pointer to the docs, the license. It never repeats the docs.
- A server never updates by itself: `moin update <server>` updates it, when the user runs it. That update never needs a step by hand. When a release needs something before or after it, like stopping apps, `moin update <server>` does it.
- `internal/report` is the contract between a daemon and its watchers. Change it in a way old daemons keep working.
- The license is explained once, in the FAQ of the docs. `LICENSE` is the text; everywhere else says "open source" without a license name.
- The product name lives only in `internal/build.Name`. One exception: the site's CSS and JS in `cloud/assets/`, whose `moin-` classes have no Go to ask.
- Words: one thing, one word, in every layer: server, app, deploy, backup, bucket, check, daemon, watcher, moin cloud. Not "hub", "node", "host", "control plane", or "agent" for the daemon.
- Standard library first. Current dependencies of the binary: Bubble Tea v2, Lip Gloss v2, `colorprofile`, `x/term`, the AWS S3 SDK for the bucket, `filippo.io/age` to encrypt the copies, `modernc.org/sqlite` for backups and data, and `yaml.v3`, with which the daemon reads a compose file before Docker does. The site adds templ and goldmark. Add one only when the standard library does not solve it well.
- Keep it small and direct. No interfaces with one implementation, no frameworks.
- Formatted (`cd cloud && task fmt`, a test checks it), `go vet ./...` clean.
- How a change ships, from the pull request to the release, is in `cloud/content/docs/shipping.md`, the one source for it. Change how moin ships and change that page in the same pull request.
- Every change goes through a pull request, from maintainers too. Nobody pushes to `main`; GitHub blocks it. One small thing per branch, CI green, a maintainer reviews it to the best of their knowledge, with AI where it helps, then a squash merge: the pull request's title and description become the one commit on `main`.
- Pull request titles and commits follow Conventional Commits: `feat:`, `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, with an optional scope like `feat(cli):`. A short first line, then a sentence or two of why when it helps. Plain text: `-` lists and backticks, no bold, headings or tables, because the description becomes the commit.
- Commits and pull requests carry no AI attribution: no `Co-Authored-By` for a tool, no "Generated with" line.
- An issue is closed by writing `Fixes #N` once, in the pull request's description. Commits on the branch may mention `#N` without "Fixes".
- Ship as the shipping page says. Deploy: Actions, Deploy, Run workflow. Release: its notes in the changelog, then the tag `v…`. The Deploy workflow runs the tests, builds the box, for a tag makes the GitHub release from `cloud/content/changelog.md` with the signed binaries, then updates moin.run through Tailscale.
- Small releases: one finished thing is one tag. `main` is always releasable; only unfinished work stays on a branch.
- Release notes follow one format, the same in moin, goilerplate and shadcn-templ, so the eye finds things fast. The same text is the GitHub release and the newest section of `cloud/content/changelog.md` (`## v…`, the date like `November 3, 2026`, then the notes), which `/changelog` shows and a test checks:
  - First line: `**In short:**` and one sentence, what is new and why to update.
  - Then these sections in this order, empty ones left out: `### New` (what you can do now), `### Changed` (what behaves differently, everything that could surprise), `### Fixed` (what was broken and works now), `### Removed` (what is gone, and what to use instead), `### Upgrade` (always there: how to update, the exact command; one line, or a short numbered list when it needs several steps).
  - Plain words a developer who has never seen moin's internals knows. Short sentences, one idea each: what the reader gets or has to do. No internal names, no backstory.
  - One bullet per thing: `- **Name**: one sentence of what the user gets.` Commands in backticks. What the user sees, never internals or commit hashes.
  - A bullet that has an issue ends with `(#N)`. No issue is created just for a note. Example: `- **Deploy every push**: The switch shows what is saved. (#49)`
  - A big turning point may add a short `### Why` under the first line. Longer stories go in a blog post or the docs, linked.

## Test

```bash
go build ./... && go vet ./... && go test ./cloud/internal/ui/pages/...
```

Locally, test only the packages you changed, as in the last command. CI runs
every test, with the race detector, on the pull request and again before
every deploy.

Then check what you changed, and only that:

- A small change: the Go tests, plus a quick look at the changed page of the
  site in a browser, or the changed command in the CLI against a throwaway
  server.
- Every release: `scripts/e2e/run.sh`, the whole product on two throwaway
  servers in one command, about 10 minutes. A change to the layout every
  page of the site uses: Chromium and WebKit, at phone and desktop width.

Whole environments and end-to-end tests run on throwaway servers
(`scripts/test-servers.sh`), not on the dev machine. Remove what you started.
Commands that hit every session (`go clean -cache`, `docker system prune`,
`docker volume prune`) only after asking.
