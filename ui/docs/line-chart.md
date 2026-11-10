---
title: "Line chart"
description: "A value over time. Press anywhere to read a point."
group: "Charts"
order: 1
---

A value over time. Press anywhere to read a point.

Styles: `components/item.css`, `components/card.css`, `components/chart.css`.
Needs `moin.js`: chart.

```example
line-chart
```

## Data

Write the data as JSON in a `script` element inside `<moin-chart>`. `lines` is
all it needs; the rest is for what the data can't say by itself.

| Field | What it is |
| --- | --- |
| `lines` | One or two lines: `label` (shown with two lines) and `values`, one for each point; `null` is no value and breaks the line. |
| `text` | In a line: the words for each value. Without it, the number and `unit`. |
| `unit` | Comes after each number when there is no `text`. |
| `top` | The top of the scale. Without it, a little over the highest value. |
| `scale` | The three words next to the scale: top, middle, bottom. Without it, the numbers and `unit`. |
| `times` | The words for each point, on top while the pointer is on it. |
| `ticks` | The words under the plot. |
| `at` | Where each point sits, as increasing numbers such as seconds. Without it, evenly spaced. |
| `gap` | Breaks the lines where two points in `at` lie further apart than this. |
| `marks` | Words at a point, by its index, drawn as a dot on the first line. |

## With a gap and a mark

```example
line-chart-with-marks
```
