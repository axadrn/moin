---
title: "Servers"
description: "Add, watch and remove servers"
order: 1
---

A server is a Linux machine with the moin agent on it. The agent is a small program that dials out to moin and does the work there, so the server needs no open port for moin.

Any Linux server you can log in to as root works, at any provider, on amd64 or arm64. Ubuntu and Debian are the easy choice. New to moin? [Getting Started](/docs/getting-started) walks you through your first server.

## See your servers

Lists every server with its live status. Lists update by themselves everywhere.

```surfaces
Web: **Servers**
CLI: moin servers
TUI: `s` on the dashboard
API: GET /api/v1/servers
MCP: list_servers
```

A server shows **waiting** until its agent connects, **online** while it is connected and **offline** when the connection drops. The API streams changes live from `GET /api/v1/servers/events`.

If a server stays **waiting**, the install command did not finish. If it is **offline**, the agent cannot reach moin. In both cases, look at the agent on the server: see [On the server](#on-the-server).

A server offline for two minutes, and back online, sends the owners and admins of the workspace a mail, see [Alerts](/docs/guides/alerts).

## Add a server

Gives you one install command for a new server. The name is yours to pick, such as `web-1`: 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit, like an app name. Leave it empty and moin picks one like `brave-otter`.

Log in to the server as root, for example with `ssh root@<server-ip>`, and run the command there within one hour. Not root? Put `sudo` before `sh`. The command works once. The server needs Linux with systemd, the program that starts services; Ubuntu and Debian have it. If the install stops halfway, get [a new install command](#a-new-install-command). The agent dials the control plane at its `APP_URL`; `MOIN_URL=http://127.0.0.1:8090` before `sh` dials another address, as on a server that runs the control plane too ([one server](/docs/self-hosting#one-server)).

```surfaces
Web: **Servers**, **Add server**
CLI: moin servers add <server-name>
TUI: opens the web: `w`, then `a`
API: POST /api/v1/servers
MCP: add_server
```

## A new install command

The install command shows once: moin stores only a fingerprint of its token. If you lost it, or move the agent to another machine, get a new one. It works once within one hour, like the first. The old command stops working, and the agent that registers with the new one takes over: the old agent's token stops working.

```surfaces
Web: **Servers**, open the server, **New install command**
CLI: moin servers install-command <server-name>
TUI: opens the web: `w`, then `a`
API: POST /api/v1/servers/{id}/install
MCP: new_install_command
```

## Rename a server

Gives a server a new name. The rules are those of adding one: lowercase letters, digits and dashes, unique in the workspace. Its apps stay on it. A server added before this rule keeps its name until you rename it.

```surfaces
Web: **Servers**, open the server, **Name**, **Save**
CLI: moin servers rename <server-name> <new-name>
TUI: opens the web: `w`, then `a`
API: PATCH /api/v1/servers/{id}
MCP: rename_server
```

## Order your servers

Servers show in the order you give them, the same for everyone in the workspace and everywhere: the web, the CLI, the TUI, the API and MCP. A new server goes to the end.

```surfaces
Web: **Servers**, drag a server to its place
CLI: moin servers move <server-name> up|down|top|bottom|<position>
TUI: opens the web: `w`, then `a`
API: PUT /api/v1/servers/order
MCP: reorder_servers
```

With a mouse you drag the whole server; on a phone you drag the grip at its left, so the page still scrolls. On the keyboard, Alt and the up or down arrow move the server in focus. The API and MCP take every server once, by name or ID, in the new order: `{"order": ["web-1", "db-1"]}`. The activity records each new order once.

**List** and **Grid** above the servers switch how the page shows them: cards side by side, or one row each. moin remembers your choice for you on every device; the rest of the team keeps theirs.

## Updates

**The control plane.** On our hosted control plane we update it, you do nothing. On your own, moin tells you when a release is out and updates with one click, see [Self-hosting](/docs/self-hosting#updates).

**The agents** update themselves to the version of the control plane. When an agent is older, moin updates it: one server at a time, never during a deploy. Your apps keep running while the agent restarts. If an agent does not come back, moin stops there and says so in the activity. Until an agent has updated, the servers list shows **update pending** next to its version; the control plane's own version is at the bottom of the sidebar.

Switch it off for your workspace with **Update agents automatically**:

```surfaces
Web: **Servers**, **Agent updates**
CLI: moin servers auto-update on|off
TUI: opens the web: `w`, then `a`
API: PUT /api/v1/agent-updates
MCP: set_agent_updates
```

Update an agent by hand:

```surfaces
Web: **Servers**, **Update agent** on the server
CLI: moin servers update <server-name>
TUI: on the server, `u`, then `y`
API: POST /api/v1/servers/{id}/update
MCP: update_agent
```

`moin servers update` without a name updates every online server with an older agent. A server with rules in `/etc/moin/rules` never updates from moin; update it on the server, see the [CLI reference](/docs/reference/cli#agent-updates).

## Disk space

The agent keeps the disk clean by itself, after every deploy and once a day. It removes the images that no release moin keeps and no container uses, images without a name, and build cache older than a week. Everything a rollback needs stays, and so do your volumes and anything moin did not put on the server. The agent's log says what it freed. When the disk is over 90% anyway, moin [alerts](/docs/guides/alerts).

## Remove a server

Disconnects the agent and makes its token stop working.

- **An online server with apps** cannot be removed: [remove the apps](/docs/guides/deploy#remove-an-app) first, so nothing keeps running unseen.
- **An offline server** goes with its apps: a server that died, or whose agent moved elsewhere, can't remove them itself. Every surface names the apps that went, and the activity notes each one. The apps stay on the machine with their data if it comes back; remove them there with `moin agent uninstall --apps`.

You cannot undo it, so moin asks you to type the server's name first: in the web dialog (the copy button next to the name helps) and in the CLI. `--yes` skips it in scripts. An agent passes the name as `confirm`.

```surfaces
Web: **Servers**, open the server, **Remove server**
CLI: moin servers rm <server-name>
TUI: opens the web: `w`, then `a`
API: DELETE /api/v1/servers/{id}
MCP: remove_server
```

The agent stays installed on the machine. Every surface answers the command that removes it; run it on the server as root, or put `sudo` in front. If the agent never connected, there is nothing to remove and no command shows.

```bash
moin agent uninstall
```

It stops and removes the `moin-agent` service, the `moin` binary and the agent's state in `/var/lib/moin`. What stays: the apps keep running with their data in `/var/lib/moin/apps`, and so do the proxy, the tunnel and the firewall that serve them, your `/etc/moin/rules` and Docker.

If you want the apps gone too, add `--apps`. It takes every app down with its containers, images and volumes, removes the proxy, the tunnel and the firewall, and deletes `/var/lib/moin`. Their data is gone for good.

```bash
moin agent uninstall --apps
```

## A new address

Agents and the CLI that still talk to an old address of the control plane move to the new one by themselves, once it answers.

## On the server

See whether the agent runs, and follow its log:

```bash
systemctl status moin-agent
journalctl -u moin-agent -f
```

The agent keeps its state in `/var/lib/moin`: its token in `agent.json` and every app under `apps/<app>`.

Next: put the server behind a [Cloudflare Tunnel and a firewall](/docs/guides/security), so no port stays open.
