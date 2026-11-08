---
title: "Servers"
description: "Add, watch, update and remove servers"
group: "Servers and data"
order: 1
---

A server is your computer on the internet. The daemon is the little helper on it. It only calls out, so the server needs no open door for moin.

Any Linux server with systemd works, amd64 or arm64, at any provider. Ubuntu and Debian are the easy choice.

```text
1 moin servers add web-1
  ─▶ an install command
2 run it on web-1, as root
  ─▶ Docker and the daemon
3 the daemon calls moin
  ─▶ web-1 is online
```

## Add a server

You get one install command. Run it on the server as root within one hour.

```surfaces
Web: **Servers**, **Add server**
CLI: moin servers add web-1
TUI: opens the web: `w`, then `a`
API: POST /api/v1/servers
MCP: add_server
```

```bash
curl -fsSL https://moin.example.com/install.sh | sh -s -- ins_…
```

It installs Docker if it is missing. It installs the daemon as the `moin-daemon` service and connects it.

- The name: 1 to 63 lowercase letters, digits or dashes, unique in the workspace. Leave it empty and moin names it `web-1`, `web-2` …
- Not root? Put `sudo` before `sh`. The command works once. If the install stops halfway, get [a new install command](#a-new-install-command).
- `MOIN_URL=http://127.0.0.1:8090` before `sh` makes the daemon dial that address instead of the hub's `APP_URL`, as on a server that runs the hub too ([one server](/docs/self-hosting#one-server)).

## See your servers

Every server with its live status.

```surfaces
Web: **Servers**
CLI: moin servers
TUI: `s` on the overview
API: GET /api/v1/servers
MCP: list_servers
```

| Status | Means |
| --- | --- |
| **waiting** | the daemon never connected: the install command did not finish |
| **online** | the daemon is connected |
| **offline** | the connection dropped: the daemon cannot reach moin |

A server offline for two minutes sends an [alert](/docs/guides/alerts). So does a server that comes back. The API streams changes from `GET /api/v1/servers/events`.

## A new install command

The install command shows once. Lost it, or moving the daemon to another server? Get a new one. It works once, within one hour. The old command and the old daemon's token stop working.

```surfaces
Web: **Servers**, open the server, **New install command**
CLI: moin servers install-command web-1
TUI: opens the web: `w`, then `a`
API: POST /api/v1/servers/{id}/install
MCP: new_install_command
```

## Rename a server

The rules of a new name apply. Its apps stay on it, and its links keep working ([Names and links](/docs/guides/names)).

```surfaces
Web: **Servers**, open the server, **Name**, **Save**
CLI: moin servers rename web-1 web-2
TUI: opens the web: `w`, then `a`
API: PATCH /api/v1/servers/{id}
MCP: rename_server
```

## Updates

The daemons update themselves to the version of the hub. One server at a time, never during a deploy. Your apps keep running while the daemon restarts.

If a daemon does not come back, moin stops there and says so in the activity. The hub itself: we update the hosted one. For your own, see [Self-hosting](/docs/self-hosting#updates).

```surfaces
Web: **Servers**, **Update daemons automatically**
CLI: moin servers auto-update on|off
TUI: opens the web: `w`, then `a`
API: PUT /api/v1/daemon-updates
MCP: set_daemon_updates
```

Update by hand. Without a name the CLI updates every online server with an older daemon:

```surfaces
Web: **Servers**, open the server, **Update daemon**
CLI: moin servers update web-1
TUI: on the server, `u`, then `y`
API: POST /api/v1/servers/{id}/update
MCP: update_daemon
```

A server with rules in `/etc/moin/rules` never updates from moin. Update it on the server ([CLI reference](/docs/reference/cli#daemon-updates)).

## Remove a server

Disconnects the daemon and stops its token. It cannot be undone, so you type the server's name first. The hub checks it. A script passes it as `--confirm <name>`, the API as `?confirm=<name>`, an AI agent as `confirm`.

```surfaces
Web: **Servers**, open the server, **Remove server**
CLI: moin servers rm web-1
TUI: opens the web: `w`, then `a`
API: DELETE /api/v1/servers/{id}
MCP: remove_server
```

- **Online with apps**: refused. [Remove the apps](/docs/guides/deploy#remove-an-app) first.
- **Offline**: it goes with its apps. The apps stay on the server with their data if it comes back.

The daemon stays on the server. Every surface gives you the command that removes it. Run it on the server as root:

```bash
moin daemon uninstall          # the daemon goes, the apps keep running
moin daemon uninstall --apps   # the apps and their data go too, for good
```

| | `uninstall` | `uninstall --apps` |
| --- | --- | --- |
| `moin-daemon` service, `moin` binary, daemon state in `/var/lib/moin` | removed | removed |
| apps and their data in `/var/lib/moin/apps` | keep running | removed with containers, images and volumes |
| proxy, tunnel, firewall | stay | removed |
| `/etc/moin/rules`, Docker | stay | stay |

## Good to know

- **Disk space**: after every deploy and once a day the daemon removes images nothing uses and old build cache. What a rollback needs, your volumes and anything moin did not put there stay.
- Next: close every port with the [Cloudflare Tunnel and the firewall](/docs/guides/security).

## Under the hood

The daemon is the `moin-daemon` service. On the server:

```bash
systemctl status moin-daemon    # does the daemon run?
journalctl -u moin-daemon -f     # its log
```

It keeps its state in `/var/lib/moin`: its token in `daemon.json` and every app under `apps/<app>`.
