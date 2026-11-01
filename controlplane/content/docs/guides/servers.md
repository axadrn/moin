---
title: "Servers"
description: "Add, watch and remove servers"
order: 1
---

A server is a Linux machine with the ops agent. The agent dials out to the control plane, so the server needs no open port for ops.

| Action | Web | CLI | TUI | MCP |
| --- | --- | --- | --- | --- |
| List with live status | **Servers** | `ops servers` | `ops`, tab **servers** | `list_servers` |
| Add | **Add server** | `ops servers add <name>` | `a` | `add_server` |
| Remove | **Remove** | `ops servers rm <name>` | `x`, then `y` | `remove_server` |

Adding a server gives one install command. It works once, within one hour. Run it as root on the machine.

A server shows **waiting** until its agent connects, **online** while it holds its session and **offline** when the session drops. Lists update live everywhere.

Removing a server disconnects its agent; its agent token stops working. A server with apps cannot be removed: remove the apps first.

## On the server

```bash
systemctl status ops-agent
journalctl -u ops-agent -f
```

The agent keeps its state in `/var/lib/ops`: its token in `agent.json` and every app under `apps/<app>`.
