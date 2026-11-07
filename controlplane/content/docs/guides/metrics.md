---
title: "Metrics"
description: "CPU, memory, disk and network of every server and app, live and with history"
group: "Every day"
order: 5
---

Metrics show how hard your servers and apps work: CPU, memory, disk and network, live and over the last 30 days. Nothing to install or set up: no Prometheus, no Grafana. A self-hosted control plane does the same.

```surfaces
Web: **Servers**, the server, **Metrics**; the app, **Metrics**
CLI: moin metrics <app-name>, or moin metrics --server web-1; --range 24h
TUI: on the app, `m`, or on a server; `[` `]` change the range
API: GET /api/v1/apps/{app}/metrics?range=1h, GET /api/v1/servers/{id}/metrics?range=24h
MCP: get_metrics
```

```bash
moin metrics shop --range 24h
```

Ranges are the last hour, 24 hours, 7 or 30 days. The last hour has every sample and updates itself every 15 seconds; longer ranges show averages. `moin metrics` takes an app or a server: a name that is no app is looked up as a server.

## Deploys on the charts

Every deploy that went live is a dot on the app's charts, with its number. The CLI, the TUI, the API and `get_metrics` list the same deploys with their times, so "slow since v12" is something you see. A server lists its apps by the memory they take.

## What each number means

| Number | What it is | When it is red |
| --- | --- | --- |
| **CPU** of a server | the busy share of all cores, 0 to 100 % | above 90 % for long: find the busy app in the server's list, read its logs, or move it to a bigger server |
| **CPU** of an app | its share of the whole server; the apps add up to at most the server's CPU | a jump after a deploy points at that deploy: `moin rollback` |
| **Memory** of a server | in use, without the page cache the kernel gives back | close to the total: the kernel starts to kill processes |
| **Memory** of an app | its containers without the page cache, against their limit; no limit means the server's memory | at its limit, Docker kills the container and it restarts: raise the limit in `compose.yml` or find the leak |
| **Disk** | the disk that holds the apps' data, `/var/lib/moin`, like `df` | above 90 %: SQLite cannot write when it is full. Old images go with every deploy |
| **Network** | bytes per second in and out: of a server over its own interfaces, without Docker's bridges; of an app over its containers | a jump you did not expect: a crawler, an attack, or a backup |
| **Load** | the load average over one minute | above the number of cores: work waits for the CPU or the disk |

## Good to know

- Requests, errors and response times per app are not measured yet. [Uptime](/docs/guides/uptime) checks every domain once a minute, with its response time.
- Numbers only: never a request, a log line or anything from your data.
- Samples of the last hour stay in the control plane's memory, finished minutes go to `history.db` once a minute, and add up into hours. Minutes stay 24 hours, hours 30 days. After a restart the last hour comes back from the minutes.
- `history.db` has no backup: losing it costs old charts, never data. See [what is kept where](/docs/self-hosting#database-backup).
- An agent older than metrics sends none until it has [updated itself](/docs/self-hosting#updates), on its next connect.

## How it works

Every 15 seconds the agent reads the server from `/proc` (CPU, memory, the data disk, network, load) and each app's running containers from the Docker API (CPU, memory, network, added up, and their memory limit), and sends a few hundred bytes over the connection it already holds. The control plane writes one row per server or app and minute, never one per sample.
