---
title: "Card"
description: "One topic on one surface."
group: "Content"
order: 2
---

One topic on one surface.

Styles: `components/button.css`, `components/field.css`, `components/item.css`, `components/card.css`, `components/copy.css`.
Needs `moin.js`: copy.

```example
card
```

## A list in a card

A list is only rows, the Card is the surface. A `moin-list` right inside a card runs almost to its edge. Its words line up with the card's, and the light of an item sits the same distance from the card on every side.

```example
card-with-a-list
```

## To the edge

`moin-bleed` takes what is in a card out to its edge, past the card's padding. `moin-inset` moves something inside it back in by that padding. Use the two for a log or a table that fills the card.

```example
card-to-the-edge
```
