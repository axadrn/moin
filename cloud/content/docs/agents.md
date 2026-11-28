---
title: "moin for agents"
description: "Claude Code, Codex and Cursor drive moin through the CLI. --json to read, -y to delete, exit codes to know. No MCP server: nothing on your server listens."
group: "Reference"
order: 4
---

AI coding agents like Claude Code, Codex and Cursor use moin the way you do. They type commands. That is all.

## What an agent needs

- **`--json`**: every reading command takes it and prints JSON, not a table. `moin --json`, `moin apps --json`, `moin backups shop --json`.
- **`-y`**: a command that deletes or replaces something asks first. An agent has no terminal, so without `-y` it stops. With `-y`, the step shows in the transcript, and you can see it.
- **Exit codes**: `0` is good. Anything else is a failure, and the command says why. `moin exec` exits with the code of the command it ran.
- **Quiet stdout**: with `--json`, `moin deploy`, `moin rollback`, `moin backup` and `moin restore` send their log to stderr. stdout holds only the JSON.

## An example session

```bash
moin deploy prod --json
moin --json
moin logs shop --json
moin rollback shop --to v3 --json
```

- `moin deploy prod --json` deploys the folder. The answer says which app and which deploy.
- `moin --json` reads every server and app, and what needs you.
- `moin logs shop --json` prints the last lines of the app. It takes no `-f`.
- `moin rollback shop --to v3 --json` goes back to v3. With no `--to`, it goes to the one before the live one.

A deploy that fails brings the last good one back. The agent sees the exit code and the log, and tries again.

## Tell the agent

Put this in your project's `AGENTS.md` or `CLAUDE.md`:

```markdown
Deploy with `moin deploy prod --json`. Check with `moin --json`.
Logs: `moin logs shop --json`. Go back: `moin rollback shop`.
Every command shows its flags with `--help`.
```

The site has an [llms.txt](/llms.txt) too: every docs page, in one list an agent can read.

## Why no MCP server

The CLI is the interface. An MCP server would be one more thing to run, to sign in to, and to keep safe. And nothing on your server listens: the daemon takes no commands. Every command goes over SSH, with a key.

So an agent has what its SSH key has. Give it what you would give a colleague. See [Teams](/docs/teams).
