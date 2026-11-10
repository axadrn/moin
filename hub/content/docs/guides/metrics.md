---
title: "Metrics"
description: "CPU, memory, disk and network of every server and app. Live and with history."
group: "Every day"
order: 5
---

Metrics show how hard your servers and apps work. CPU, memory, disk and network. Live and over the last 30 days. Nothing to install or set up.

```surfaces
Web: **Servers**, the server, **Metrics**; the app, **Metrics**
CLI: moin metrics <app-name>, or moin metrics --server web-1; --range 24h
TUI: on the app, `m`, or on a server; `[` `]` change the range
API: GET /api/v1/apps/{app}/metrics?range=1h, GET /api/v1/servers/{id}/metrics?range=24h
MCP: get_metrics
```

```bash
moin metrics my-app --range 24h
```

Ranges are the last hour, 24 hours, 7 or 30 days. The last hour updates every 15 seconds.

## Deploys on the charts

Every deploy that went live is a dot on the app's charts. So "slow since v12" is something you can see. A server lists its apps by the memory they take.

## What each number means

| Number | What it is | When it is red |
| --- | --- | --- |
| **CPU** of a server | the busy share of all cores | above 90 % for long: find the busy app, or move it to a bigger server |
| **CPU** of an app | its share of the whole server | a jump after a deploy points at that deploy: `moin rollback` |
| **Memory** of a server | in use, without the page cache | close to the total: the kernel starts to kill processes |
| **Memory** of an app | its containers, against their limit | at its limit, Docker kills the container and it restarts |
| **Disk** | the disk that holds the apps' data, `/var/lib/moin` | above 90 %: SQLite cannot write when it is full |
| **Network** | bytes per second in and out | a jump you did not expect: a crawler, an attack, or a backup |
| **Load** | the load average over one minute | above the number of cores: work waits |

## Good to know

- Requests and errors per app are not measured yet. [Uptime](/docs/guides/uptime) visits every domain once a minute.
- Numbers only: never a request, a log line or anything from your data.
- Minutes stay 24 hours, hours 30 days. `history.db` has no backup: losing it costs old charts, never data. See [what is kept where](/docs/self-hosting#database-backup).
- A daemon older than metrics sends none until it has [updated itself](/docs/self-hosting#updates).
