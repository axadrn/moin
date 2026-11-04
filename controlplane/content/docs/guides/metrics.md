---
title: "Metrics"
description: "CPU, memory, disk and network of every server and every app, live and with history, built in"
order: 11
---

From the moment its agent connects, every server sends its numbers to the control plane, and from the first deploy on, every app's too. There is nothing to install and nothing to set up: no Prometheus, no Grafana, no exporter. A self-hosted control plane does the same.

Every 15 seconds the agent reads what the server already knows and sends a few hundred bytes over the connection it holds anyway:

- **The server**, from `/proc`: CPU, memory, the disk that holds the apps' data, network in and out, and the load.
- **Each app**, from the Docker API: CPU, memory and network of its running containers, added up, and their memory limit.

Numbers only: never a request, a log line or anything from your data.

## See the metrics

The newest numbers, and a chart per number over the last hour, 24 hours, 7 or 30 days. The last hour has every sample and updates itself every 15 seconds; longer ranges show averages. A server lists its apps by the memory they take, so you see at once who eats the RAM.

```surfaces
Web: the server's **Metrics** tab, from **Metrics** on its card; the app's **Metrics** tab
CLI: moin metrics <app-name>, or moin metrics --server web-1; --range 24h
TUI: on a server or an app, `m` or `:metrics`; the panel stays live
API: GET /api/v1/apps/{app}/metrics?range=1h, GET /api/v1/servers/{id}/metrics?range=24h
MCP: get_metrics
```

`moin metrics` takes an app or a server: a name that is no app is looked up as a server. The CLI and the TUI draw each number as a small sparkline.

## Deploys on the charts

Every deploy that went live is a dot on the app's charts, with its number in the tooltip: "this got slow after v12" is something you see, not guess. The CLI, the TUI, the API and `get_metrics` list the same deploys with their times, so an agent asked "why is shop slow?" gets the numbers and the deploy that changed them.

## What each number means

| Number | What it is | When it is red |
| --- | --- | --- |
| **CPU** of a server | the busy share of all cores, 0 to 100 % | above 90 % for long: an app is busy or stuck. Find it in the server's app list, then look at its logs, or move it to a bigger server |
| **CPU** of an app | its share of the whole server, so the apps add up to at most the server's CPU | a jump after a deploy points at that deploy: roll back with `moin rollback` |
| **Memory** of a server | what is in use, without the page cache the kernel gives back on demand | close to the total: the kernel starts to kill processes. Find the app that grows in the list |
| **Memory** of an app | what its containers use without the page cache, against their limit; no limit means the server's memory | at its limit, Docker kills the container and it restarts. Raise the limit in `compose.yml` or find the leak |
| **Disk** | the disk that holds the apps' data, `/var/lib/moin`, like `df` | above 90 %: SQLite cannot write when it is full. Old images go with every deploy; a database that grows needs a bigger disk |
| **Network** | bytes per second in and out: of a server over its own interfaces, without Docker's bridges; of an app over its containers | a jump you did not expect: a crawler, an attack, or a backup |
| **Load** | the load average over one minute | above the number of cores: work waits for the CPU or the disk |

Requests, errors and response times per app are not measured yet: Traefik does not count them on your servers today. [Uptime](/docs/guides/uptime) checks every domain from the outside once a minute, with its response time, in the meantime.

## What moin keeps

The last hour of samples lives in the control plane's memory, about 7 KB per server or app. Once a minute the finished minutes go to `history.db`, a file next to the control plane's database, in one write, one row per server or app and minute, never a row per sample; once an hour they add up into hours. Minutes stay for 24 hours, hours for 30 days. After a restart of the control plane, the last hour comes back from the minutes.

A server with five apps writes six rows a minute, so 1,000 such servers are 6,000 rows a minute in one transaction: one small control plane keeps up with thousands of servers.

`history.db` has no backup: metrics grow back every 15 seconds, so losing it costs old charts, never data. See [what is kept where](/docs/self-hosting#database-backup).

An agent older than metrics sends none until it has [updated itself](/docs/self-hosting#updates), on its next connect.
