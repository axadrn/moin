---
title: "Servers"
description: "Add, watch and remove servers"
order: 1
---

A server is a Linux machine with the ops agent on it. The agent is a small program that dials out to ops and does the work there, so the server needs no open port for ops.

Any Linux server you can log in to as root works, at any provider, on amd64 or arm64. Ubuntu and Debian are the easy choice. New to ops? [Getting Started](/docs/getting-started) walks you through your first server.

## See your servers

Lists every server with its live status. Lists update by themselves everywhere.

```surfaces
Web: **Servers**
CLI: ops servers
TUI: `ops`, then the **servers** tab
API: GET /api/v1/servers
MCP: list_servers
```

A server shows **waiting** until its agent connects, **online** while it is connected and **offline** when the connection drops. The API streams changes live from `GET /api/v1/servers/events`.

If a server stays **waiting**, the install command did not finish. If it is **offline**, the agent cannot reach ops. In both cases, look at the agent on the server: see [On the server](#on-the-server).

## Add a server

Gives you one install command for a new server. The name is yours to pick, such as `web-1`: 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit, like an app name. Leave it empty and ops picks one like `brave-otter`.

Log in to the server as root, for example with `ssh root@<server-ip>`, and run the command there within one hour. Not root? Put `sudo` before `sh`. The command works once. The server needs Linux with systemd, the program that starts services; Ubuntu and Debian have it. If the install stops halfway, get [a new install command](#a-new-install-command).

```surfaces
Web: **Servers**, **Add server**
CLI: ops servers add <server-name>
TUI: on the servers tab, `a`
API: POST /api/v1/servers
MCP: add_server
```

## A new install command

The install command shows once: ops stores only a fingerprint of its token. If you lost it, or move the agent to another machine, get a new one. It works once within one hour, like the first. The old command stops working, and the agent that registers with the new one takes over: the old agent's token stops working.

```surfaces
Web: **Servers**, open the server, **New install command**
CLI: ops servers install-command <server-name>
TUI: on the server, `i`
API: POST /api/v1/servers/{id}/install
MCP: new_install_command
```

## Rename a server

Gives a server a new name. The rules are those of adding one: lowercase letters, digits and dashes, unique in the workspace. Its apps stay on it. A server added before this rule keeps its name until you rename it.

```surfaces
Web: **Servers**, open the server, **Name**, **Save**
CLI: ops servers rename <server-name> <new-name>
TUI: on the server, `cw` or `:rename`
API: PATCH /api/v1/servers/{id}
MCP: rename_server
```

## Updates

**The control plane.** On our hosted control plane we update it, you do nothing. On your own, ops tells you when a release is out and updates with one click, see [Self-hosting](/docs/self-hosting#updates).

**The agents** update themselves to the version of the control plane. When an agent is older, ops updates it: one server at a time, never during a deploy. Your apps keep running while the agent restarts. If an agent does not come back, ops stops there and says so in the activity.

Switch it off for your workspace with **Update agents automatically**:

```surfaces
Web: **Servers**, **Agent updates**
CLI: ops servers auto-update on|off
TUI: settings tab, **Agent updates**, `enter`, or `:auto-update`
API: PUT /api/v1/agent-updates
MCP: set_agent_updates
```

Update an agent by hand:

```surfaces
Web: **Servers**, **Update agent** on the server
CLI: ops servers update <server-name>
TUI: on the server, `u`
API: POST /api/v1/servers/{id}/update
MCP: update_agent
```

`ops servers update` without a name updates every online server with an older agent. A server with rules in `/etc/ops/rules` never updates from ops; update it on the server, see the [CLI reference](/docs/reference/cli#agent-updates).

## Remove a server

Disconnects the agent and makes its token stop working.

- **An online server with apps** cannot be removed: [remove the apps](/docs/guides/deploy#remove-an-app) first, so nothing keeps running unseen.
- **An offline server** goes with its apps: a server that died, or whose agent moved elsewhere, can't remove them itself. Every surface names the apps that went, and the activity notes each one. The apps stay on the machine with their data if it comes back; remove them there with `ops agent uninstall --apps`.

You cannot undo it, so ops asks you to type the server's name first: in the web dialog (the copy button next to the name helps), in the CLI and in the TUI. `--yes` skips it in scripts. An agent passes the name as `confirm`.

```surfaces
Web: **Servers**, open the server, **Remove server**
CLI: ops servers rm <server-name>
TUI: on the server, `x`, then type its name
API: DELETE /api/v1/servers/{id}
MCP: remove_server
```

The agent stays installed on the machine. Every surface answers the command that removes it; run it on the server as root, or put `sudo` in front. If the agent never connected, there is nothing to remove and no command shows.

```bash
ops agent uninstall
```

It stops and removes the `ops-agent` service, the `ops` binary and the agent's state in `/var/lib/ops`. What stays: the apps keep running with their data in `/var/lib/ops/apps`, and so do the proxy, the tunnel and the firewall that serve them, your `/etc/ops/rules` and Docker.

If you want the apps gone too, add `--apps`. It takes every app down with its containers, images and volumes, removes the proxy, the tunnel and the firewall, and deletes `/var/lib/ops`. Their data is gone for good.

```bash
ops agent uninstall --apps
```

## On the server

See whether the agent runs, and follow its log:

```bash
systemctl status ops-agent
journalctl -u ops-agent -f
```

The agent keeps its state in `/var/lib/ops`: its token in `agent.json` and every app under `apps/<app>`.

Next: put the server behind a [Cloudflare Tunnel and a firewall](/docs/guides/security), so no port stays open.
