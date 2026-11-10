---
title: "Layout helpers"
description: "Small rules for what the other layouts do not cover."
group: "Layout"
order: 10
---

Small rules for what the other layouts do not cover.

Styles: `layout.css`.

```example
layout-helpers
```

- `moin-narrow` is as wide as text reads well, or `--width`. On a `moin-page` it stays centered.
- `moin-hide-empty` takes no room while it is empty. Use it for a place an answer swaps into.
- `moin-contents` holds only an id. What is in it counts as being in its place in a row or grid.
- `moin-scroll` scrolls sideways. `data-both` scrolls both ways, up to `--scroll-max` high. What stays in view inside needs this box.

## Jumping to a place

A link to `#something` stops under a floating bar, for anything with an `id`. Nothing to add. `--jump` on the target changes the distance, 6rem by default.
