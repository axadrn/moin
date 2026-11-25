---
title: "AI agents (MCP)"
description: "Let Claude, Cursor and other AI agents run moin for you"
group: "Team"
order: 4
---

MCP lets an AI agent such as Claude or Cursor use moin for you: *"roll back my app and show me its logs"*. The hub is an MCP server at `/mcp`. Almost every action is a tool. [Where things work](/docs/reference/surfaces) lists what stays in the web.

## Connect an AI agent

Add the URL of your hub's MCP server, `https://moin.example.com/mcp`, to your AI agent. It opens the browser. You see the same page as for `moin login`: pick the workspace and the rights, then allow with your passkey. The agent is now a connection under **Settings**, **Connections**, where you change its rights or remove it.

- **Claude Code**: `claude mcp add --transport http moin https://moin.example.com/mcp`, then `/mcp` in Claude Code, pick moin and sign in.
- **Claude and Claude Desktop**: **Customize**, **Connectors**, **Add custom connector**, the URL, then **Connect**.
- **Cursor**: `{"mcpServers": {"moin": {"url": "https://moin.example.com/mcp"}}}` in `.cursor/mcp.json`, then **Connect** next to moin in the MCP settings.
- **ChatGPT**: add a connector in developer mode with the URL and OAuth to sign in.
- **Any other client**: the URL is enough when it signs in with OAuth, as the MCP spec asks.

Claude and ChatGPT connect from their own cloud, so they need a hub on the public internet. Claude Code and Cursor run on your computer and reach any hub you reach.

An agent registers itself with the hub when you add it. Allow it within a day: an agent nobody let in after 24 hours is forgotten, and you add it again.

### Without the browser

A client that cannot sign in with OAuth, or one that runs in CI, uses a CI token. [Make one](/docs/guides/account#make-a-ci-token) with the rights the agent needs and send it as a header:

```bash
claude mcp add --transport http moin https://moin.example.com/mcp \
  --header "Authorization: Bearer pat_…"
```

## Good to know

- An agent you let in works only at `/mcp` of your hub, never in the JSON API. Its access lasts an hour and renews by itself; unused for 90 days, the connection ends. `whoami` tells it its rights.
- The approval page shows the agent's name as the agent gave it, and where it sends you back, like claude.ai or localhost. Nobody checked the name, so allow only an agent you just added. Settings, Connections shows both too: "Claude · claude.ai".
- Each tool needs its right, or all of several, named in the table below and at the end of its description. Without it the tool answers `missing_right` with the right, and the agent says: "This connection may not deploy and restart. Change it in Settings, Connections." [Rights](/docs/reference/rights)
- Rights never add to your role: a member's agent cannot invite, whatever its rights.
- A CI token for one app does not reach MCP.
- A connection or CI token sends at most 121 requests a minute, MCP and API together; one more answers `rate_limited` with the seconds to wait. [API](/docs/reference/api#basics)
- Tools that remove, replace or stop something are marked destructive, so clients ask first.
- Four cannot be undone: `remove_server`, `remove_app`, `remove_bucket` and `restore_app`. They also take `confirm`, the name of the server, app or bucket, and refuse without it or with another name: `confirm_mismatch`, the same check as on the web and in the API.
- Tools take the app as `app` and the server as `server`.

## Tools

| Tool | Does | Right |
| --- | --- | --- |
| `whoami` | the account and the connection or CI token with its rights, with `email_alerts` | read |
| `set_email_alerts` | turns your [email alerts](/docs/guides/alerts) on or off | servers |
| `list_alert_destinations` | where [alerts](/docs/guides/alerts#slack-discord-telegram-and-webhooks) go besides email, without URLs or tokens, and which fail | read |
| `add_alert_destination` | adds Slack, Discord, Telegram or a webhook; a webhook returns its `signing_secret` once | servers |
| `test_alert_destination` | sends a test alert | servers |
| `remove_alert_destination` | removes a destination | servers, destructive |
| `get_status` | whether everything is OK: what needs you, catalog updates and counts | read |
| `list_servers` | servers with live status | read |
| `add_server` | adds a server, returns its install command | servers |
| `remove_server` | removes a server; `confirm` is its name; returns the `uninstall_command` for the server, empty when no daemon ever connected, and the `apps` that went with an offline server | delete, servers, destructive |
| `new_install_command` | a new one-time install command for a server | servers, destructive |
| `rename_server` | renames a server | servers |
| `update_daemon` | puts the hub's daemon version on a server | servers |
| `set_daemon_updates` | turns automatic daemon updates on or off | servers |
| `get_version` | the hub's version, the newest version of moin and whether daemons update by themselves; for the admins of the hub also the hub backup | read |
| `update_hub` | updates a self-hosted hub to the newest version, for its admins: first it returns the release notes, then it updates with `confirm` | servers |
| `list_apps` | apps with the live and the newest deploy | read |
| `get_app` | an app with its deploys and what was detected | read |
| `create_app` | creates an app on a server with its password on; returns the password, this once | deploy |
| `remove_app` | stops and removes an app, keeps its data; `confirm` is its name | delete, destructive |
| `rename_app` | renames an app; links, data and backups stay | deploy |
| `pin_app` | pins an app, or unpins it with `pinned` false | deploy |
| `get_deploy` | one deploy with its log | read |
| `redeploy_app` | runs the newest source or image again | deploy |
| `deploy_image` | deploys an image built in CI, with the compose file of `ref` in the linked repo | deploy |
| `create_deploy_hook` | a new deploy hook URL for CI, shown once; the old one stops working and pushes no longer deploy | deploy, destructive |
| `remove_deploy_hook` | turns the deploy hook off; CI can no longer deploy | deploy, destructive |
| `list_registries` | registry logins, without passwords, with the servers that keep each and the ones that do not | read |
| `set_registry` | hands a server a registry login, which only it keeps | servers |
| `remove_registry` | removes a registry login, also from the servers | servers, destructive |
| `rollback_app` | starts an earlier deploy again | deploy, destructive |
| `list_catalog` | the catalog with each app's newest version | read |
| `get_catalog_app` | a catalog app with every version | read |
| `install_catalog_app` | installs a catalog app on a server; returns the first sign-in password and the app's password, this once | deploy |
| `update_app` | sets a catalog app to a version and deploys it | deploy |
| `get_logs` | the last container lines | read |
| `get_compose_file` | the [compose file](/docs/guides/compose#the-compose-file-moin-runs) moin runs for an app, without secret values, to run it elsewhere | read |
| `exec_command` | [runs a command](/docs/guides/exec) in a container, no shell; the exit code and the last 64 KB of output (owner or admin). It can do anything in the app | deploy, env, delete, destructive |
| `list_tables` | tables, row counts, schema and migration of the database | read |
| `query_database` | runs read-only SQL | env |
| `execute_sql` | runs SQL that changes data | env, delete, destructive |
| `get_backups` | how each database is backed up: how often, how long, the newest copy, the daily check, and the moments to go back to | read |
| `set_backups` | how often (`every_minutes`: 15, 60 or 1440), how long (`keep_days`: 1, 3, 7 or 30) | deploy, destructive |
| `restore_app` | goes back to a moment (`moment`, its id or label: a copy, `before-v12`, `undo-…`) or a time (`at`), the newest copy at or before it; keeps the database before, so it can be undone; `confirm` is the app's name. It runs on its own and answers its state; with `id` it tells how it stands | delete, destructive |
| `download_backup` | a link to a copy in the app's own bucket, the newest or `moment`, signed by the app's server; it works for 15 minutes | env |
| `list_buckets` | the buckets, without their secrets, with the apps that use each and the servers that keep its key | read |
| `add_bucket` | adds a bucket; `server` tests it and keeps the secret key; the first is the default | servers |
| `edit_bucket` | changes a bucket; `server` tests a new place or key, an empty `secret_key` keeps the server's | servers |
| `set_bucket_key` | hands one more server the bucket's secret key | servers |
| `set_default_bucket` | makes a bucket the default | servers |
| `remove_bucket` | removes a bucket nothing uses; `confirm` is the bucket's name | delete, destructive |
| `set_app_bucket` | moves an app's backups to a bucket; the copies start again there | deploy |
| `check_domains` | whether each domain points at the server | read |
| `get_uptime` | uptime per domain over 24 hours, 7 and 30 days, the last visit and the last outages | read |
| `set_uptime` | the path uptime visits on a domain, or pauses and resumes it | deploy |
| `get_metrics` | CPU, memory, disk and network of an app or a server, now and over 1h, 24h, 7d or 30d; for an app the deploys that went live, for a server its apps by memory | read |
| `set_domains` | domains and the canonical one, or with `service` the domains of another service | deploy |
| `get_instant` | the instant address, or why the app has none | read |
| `set_instant` | the provider of the instant address or off | deploy |
| `set_protection` | turns the app's password on for every domain: your own, or one moin makes and returns this once | env |
| `remove_protection` | turns the app's password off: every domain is open | env, destructive |
| `restart_app` | starts the live deploy again with the current settings | deploy |
| `stop_app` | takes the app offline; its data stays | deploy, destructive |
| `start_app` | runs a stopped app again | deploy |
| `list_env` | the env vars of an app, by name, with when each changed; never a value | read |
| `set_env` | sets and removes env vars | env |
| `replace_env` | replaces all env vars at once, like one `.env` file; keys not sent are removed; returns the keys added, replaced and removed | env, destructive |
| `get_github` | GitHub installations and the install link | read |
| `list_branches` | the branches of a repo, the default first | read |
| `list_repos` | repos moin may read, and where on GitHub to give it more | read |
| `link_repo` | follows a repo and branch; with `auto_deploy` the deploy hook turns off | deploy, destructive |
| `unlink_repo` | stops following it | deploy, destructive |
| `deploy_ref` | deploys a branch, tag or commit from GitHub | deploy |
| `get_integrations` | whether Cloudflare is connected and which servers keep a Tailscale key | read |
| `set_integrations` | stores the Cloudflare token in the hub; hands `server` the Tailscale key, which only it keeps | servers |
| `set_server_security` | visitors through a Cloudflare Tunnel, SSH for everyone, only through Tailscale or off, the firewall | servers, destructive |
| `get_billing` | the subscription; on a self-hosted hub it says billing is off | read |
| `list_workspaces` · `get_workspace` | the workspace of the connection or CI token, with its team | read |
| `list_activity` | who did what, newest first, to the second; filter by `app`, `server` or `user` | read |
| `invite_member` | invites someone by email, as member; an admin is invited in the web | servers |
| `resend_invitation` | sends an invitation again | servers |
| `revoke_invitation` | takes an invitation back | servers, destructive |
| `rename_workspace` | renames the workspace | servers |
