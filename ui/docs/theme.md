---
title: "Theme"
description: "Colors, spacing and corners: a theme is a list of CSS variables."
group: "Start"
order: 2
---

A theme is a list of CSS variables. Each color is one line with its light and its dark value. The editor above writes that list.

## Your font

moin UI uses the system's fonts, so there is nothing to load. Pick another one above and Copy theme gives you these lines for it.

```css
@font-face {
  font-family: "Inter";
  src: url("/fonts/inter.woff2") format("woff2");
  font-weight: 100 900;
  font-display: optional;
}
:root {
  --font-sans: "Inter", system-ui, sans-serif;
}
```

## Light and dark

It follows the system by itself, with no script. Each color in `theme.css` holds its light and its dark value.

To let people choose, add a button. The choice is kept in the browser. Load `moin.js` in the head without `defer`, so a saved choice is there before the first paint.

```html
<script src="ui/moin.js"></script>

<button type="button" class="moin-button" data-mode-toggle>Light or dark</button>
```

For one mode only, set the class yourself.

```html
<html class="dark">
```
