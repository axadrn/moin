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
