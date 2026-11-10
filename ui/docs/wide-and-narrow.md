---
title: "Wide and narrow"
description: "One wide and one narrow, under each other on a phone. Follows the window."
group: "Layout"
order: 4
---

One wide and one narrow, under each other on a phone. Follows the window.

Styles: `components/item.css`, `components/card.css`.

```example
wide-and-narrow
```

## A label and two answers

`data-cols="2-3-3"` is a label and two answers, under each other on a phone. Rows in a list keep the same columns, so they line up. `data-fit` and `data-cols="3"` cannot do this: their columns are equal, and they wrap differently from row to row as the window narrows.

```example
wide-and-narrow-three
```

## A part with its own width

`moin-width` gives a part of a row its own `--width` from a tablet up, and the whole line on a phone. With `data-fixed` it keeps that width on a phone too.

```example
row-with-a-width
```
