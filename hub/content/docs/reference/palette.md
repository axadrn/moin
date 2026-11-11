---
title: "Palette and keys"
description: "One box finds and does everything. A few keys do the rest."
group: "Reference"
order: 3
---

The palette is a box in the middle of the screen. Type what you want. Press `enter`. You are there. It is the same in the web and the terminal UI.

```text
⌘K
 │
 ▼
┌────────────────────────────────┐
│ my-app lo                      │
├────────────────────────────────┤
│ ▸ Logs    my-app             L │
│   ...                          │
└────────────────────────────────┘
  ↓ ↑ pick   enter go   esc close
```

| Opens it | Where |
| --- | --- |
| `⌘K` · `ctrl+k` | the web; `ctrl+k` in the terminal UI too |
| `/` | both, unless you type in a field; in the terminal UI's logs `/` filters |
| `space` | the terminal UI |
| the search in the bar | the web |

## What it finds

- **Every page**: Overview, Apps, Catalog, Servers, Settings, Team, Alerts, Bucket, Activity, and each section of every app.
- **Every app and every server**.
- **Actions**: deploy, restart, start, roll back, open the app's domain. In the web the harmless ones run at once; rolling back asks first. In the terminal UI deploy, restart, stop and roll back ask first: `y` does it.
- **The catalog**: an app you can install, such as ntfy or Umami (web only).
- **The source code** of moin on GitHub (web only).
- **The docs**, last, at most three pages (web only).

On the website, signed out, the magnifier at the top right is the same box. It finds the docs and the catalog.

With nothing typed, it shows what goes with the page you are on first, such as the sections and actions of the app you are looking at, then **Recent**: what you picked most and latest. The web remembers that in your browser, the terminal UI its last eight picks.

## Typing

You do not have to type it right.

| You type | It finds |
| --- | --- |
| `my-app logs` or `logs my-app` | the logs of my-app: words in any order |
| `deplyo`, `setings` | deploy, settings: one typo is fine in words of four letters or more |
| `dply` | deploy: the letters in order are enough |
| `ml` | my-app logs: the first letters of several words |

Every answer shows its key at the right: the key that does the same without the palette.

## Keys in the palette

| Key | Does |
| --- | --- |
| `↓` `↑` | the next answer, the one before |
| `ctrl+j` | the next answer |
| `ctrl+k` | web: the answer before. Terminal UI: closes the palette (`ctrl+p` goes up) |
| `enter` | opens or runs the picked answer |
| `esc` | closes it |

## Keys everywhere

None of them works while you type in a field.

| Key | Does |
| --- | --- |
| `j` `k` | down and up through a list; `enter` opens the row |
| `gg` · `G` | the top of the page, the end; in the terminal UI `g` · `G`, the first row and the last |
| `-` | one level up, the page in the `‹` link; in the terminal UI `esc` |
| `?` | every key of the place you are in |
| `esc` | closes what is open, or leaves a field; in the terminal UI also goes back |

## Keys on an app

| Key | Web | TUI |
| --- | --- | --- |
| `d` | Deploys | deploys |
| `o` | Domains | domains |
| `l` | Logs | logs |
| `b` | Data | backups |
| `e` | Settings | env |
| `u` `m` | | uptime, metrics |

The terminal UI has more keys on an app, such as `R` to redeploy and `P` to roll back: [Terminal UI](/docs/reference/tui#app).

## Go to a page (web)

`g`, then a letter:

| Keys | Page |
| --- | --- |
| `g` `h` or `g` `o` | Overview |
| `g` `s` | Servers |
| `g` `,` | Settings |
| `g` `t` | Team |
| `g` `l` | Alerts |
| `g` `b` | Bucket |
| `g` `y` | Activity |
