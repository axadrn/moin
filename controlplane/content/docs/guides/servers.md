---
title: "Servers"
description: "Add, watch and remove servers"
order: 1
---

A server is a Linux machine with the ops agent on it. The agent is a small program that dials out to ops and does the work there, so the server needs no open port for ops.

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

## Add a server

Gives you one install command for a new server. Run it as root on the machine within one hour; it works once. Leave the name empty and ops picks one like `brave-otter`. The server needs Linux with systemd, such as Ubuntu or Debian. If the install stops halfway, remove the server and add it again for a new command.

```surfaces
Web: **Servers**, **Add server**
CLI: ops servers add web-1
TUI: on the servers tab, `a`
API: POST /api/v1/servers
MCP: add_server
```

## Rename a server

Gives a server a new name. The rules are those of adding one: up to 64 characters, unique in the workspace. Its apps stay on it.

```surfaces
Web: **Servers**, open the server, **Name**, **Save**
CLI: ops servers rename web-1 shop-1
TUI: on the server, `n`
API: PATCH /api/v1/servers/{id}
MCP: rename_server
```

## Update the agent

Puts the agent version of your control plane on a server. Use it when ops says a newer agent is there.

```surfaces
Web: **Servers**, **Update agent** on the server
CLI: ops servers update web-1
TUI: on the server, `u`
API: POST /api/v1/servers/{id}/update
MCP: update_agent
```

`ops servers update` without a name updates every online server with an older agent.

## Remove a server

Disconnects the agent and makes its token stop working. A server with apps cannot be removed: remove the apps first.

You cannot undo it, so ops asks you to type the server's name first: in the web dialog (the copy button next to the name helps), in the CLI and in the TUI. `--yes` skips it in scripts. An agent passes the name as `confirm`.

```surfaces
Web: **Servers**, open the server, **Remove server**
CLI: ops servers rm web-1
TUI: on the server, `x`, then type its name
API: DELETE /api/v1/servers/{id}
MCP: remove_server
```

The agent stays installed on the machine. Every surface answers the command that removes it; run it on the server:

```bash
sudo ops agent uninstall
```

It stops and removes the `ops-agent` service, the `ops` binary and the agent's state in `/var/lib/ops`. What stays: the apps keep running with their data in `/var/lib/ops/apps`, and so do the proxy, the tunnel and the firewall that serve them, your `/etc/ops/rules` and Docker.

If you want the apps gone too, add `--apps`. It takes every app down with its containers, images and volumes, removes the proxy, the tunnel and the firewall, and deletes `/var/lib/ops`. Their data is gone for good.

```bash
sudo ops agent uninstall --apps
```

## On the server

See whether the agent runs, and follow its log:

```bash
systemctl status ops-agent
journalctl -u ops-agent -f
```

The agent keeps its state in `/var/lib/ops`: its token in `agent.json` and every app under `apps/<app>`.
