---
title: "Log"
description: "What a process printed, line by line."
group: "Content"
order: 4
---

What a process printed, line by line.

Styles: `components/card.css`, `components/log.css`.

```example
log
```

Each `div` is a line, `data-kind` says what kind (`out`, `step`, `ok`, `err`),
`data-n` sets its number and `data-dim` makes a part of it quieter. A `details`
with lines right in it folds them under its `summary`. With `data-meta` on the
log, each line has two more columns in front of the text, like a time and a
service.

## Tail

`moin-tail` keeps the newest line in view while you are at the bottom, and stays put when you scroll up. It holds a `moin-log`. `--tail` is how high it grows.

- `data-top` keeps few lines at the top.
- `data-hold` keeps the place of lines still to come.
- `data-tall` holds a height on a large screen.

```example
log-tail
```

## Plain output

A `pre` with `moin-log` holds output in one piece. It keeps its own line breaks and wraps long lines.

```example
log-plain
```
