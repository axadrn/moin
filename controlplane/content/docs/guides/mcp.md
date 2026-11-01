---
title: "AI agents (MCP)"
description: "Let Claude and other agents run ops for you"
order: 12
---

The control plane serves an MCP server at `/mcp`. Every action of the web UI, the CLI and the terminal UI is a tool, so you can say *"roll back blog and show me its logs"*.

Agents authenticate with a personal API token. A read-only token can call the read tools; changes need read and write access. Removing a server or an app is marked destructive, so clients ask before they call it.

## Connect Claude Code

```bash
claude mcp add --transport http ops https://ops.example.com/mcp --header "Authorization: Bearer pat_…"
```

## Tools

| Tool | Does | Scope |
| --- | --- | --- |
| `whoami` | the account and token | read |
| `list_servers` | servers with live status | read |
| `add_server` | adds a server, returns its install command | write |
| `remove_server` | removes a server | write, destructive |
| `update_agent` | puts the control plane's agent version on a server | write |
| `list_apps` | apps with live and newest deploy | read |
| `get_app` | an app with its deploys and what was detected | read |
| `create_app` | creates an app on a server | write |
| `remove_app` | stops and removes an app, keeps its data | write, destructive |
| `get_deploy` | one deploy with its log | read |
| `redeploy_app` | runs the newest source or image again | write |
| `deploy_image` | deploys an image built in CI | write |
| `create_deploy_hook` | a new deploy hook URL for CI, shown once | write |
| `list_registries` | registry logins, without passwords | read |
| `set_registry` | stores a registry login | write |
| `remove_registry` | removes a registry login | write, destructive |
| `rollback_app` | starts an earlier release again | write |
| `get_logs` | the last container lines | read |
| `list_tables` | tables, row counts, schema and migration of the database | read |
| `query_database` | runs read-only SQL | write |
| `execute_sql` | runs SQL that changes data | write, destructive |
| `get_backups` | snapshots, point in time window, restore drills | read |
| `back_up_app` | takes a snapshot now | write |
| `restore_app` | restores a snapshot or a point in time | write, destructive |
| `set_backup_settings` | snapshot before every change | write |
| `get_backup_storage` | the bucket, without its secret | read |
| `set_backup_storage` | sets the bucket | write |
| `remove_backup_storage` | stops backups | write, destructive |
| `check_domains` | whether each domain points at the server | read |
| `set_domains` | domains and the canonical one | write |
| `set_protection` | a password in front of the app | write |
| `remove_protection` | makes the app public again | write, destructive |
| `apply_app` | restarts the running release with the current settings | write |
| `get_env` | env vars in plain text | write |
| `set_env` | sets and removes env vars | write |
| `get_github` | GitHub installations and the install link | read |
| `list_repos` | repos ops may read | read |
| `link_repo` | follows a repo and branch | write |
| `unlink_repo` | stops following it | write, destructive |
| `deploy_ref` | deploys a branch, tag or commit from GitHub | write |
| `get_integrations` | whether the Cloudflare token and Tailscale key are stored | read |
| `set_integrations` | stores them | write |
| `set_server_security` | Cloudflare Tunnel, SSH on Tailscale, firewall | write, destructive |
| `get_billing` | the subscription | read |
| `list_workspaces` · `get_workspace` | the token's workspace with its members | read |
| `list_activity` | who did what, newest first | read |
| `invite_member` | invites someone by email | write |
| `rename_workspace` | renames the workspace | write |
