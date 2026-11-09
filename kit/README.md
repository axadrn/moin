# Kit

Experimental. It can change or disappear at any time.

moin's UI kit in native HTML, CSS and JS. No Tailwind, no build step. Every
language can print HTML, so every language can use it.

Kit by moin: new design principles for the web. It is not a library. Copy this
folder into your repo and it is your code, so change whatever you want.

Link `kit.css`, add `kit.js`, write HTML with `kit-` classes. That is about
33 KB gzipped. Most parts need no JS at all; each page in `docs/` says so when
one does. Use it as plain HTML, or turn parts into components in your stack.

MIT licensed. What changed in each release is in `CHANGELOG.md`.

## Floors

Each floor only knows the floors under it.

| Floor | File | Holds |
| --- | --- | --- |
| 1 Foundation | `foundation.css` | every color, space, radius and motion value |
| 2 Frame | `layout.css` | `kit-stack`, `kit-row`, `kit-grid`, `kit-page` |
| 3 Parts | `parts/`, `icons.css`, `icons/` | one file per part: text, button, field, list, card, dialog and the rest |

`examples/` has every example as its own HTML file, and `docs/` has one page
per part that points at them. That HTML is the contract: whatever your language
prints, it prints this.

`rooms/` has whole pages built from the parts. They are only HTML to copy and
bring no CSS of their own.

`showroom/` shows all of it and holds the theme editor. Your site does not need
that folder.

## Rules

- `style=` holds only variables, never a look.
- A page writes no CSS. It puts floors 2 and 3 together.
- The container makes the space with `gap`, never the element.
- Colors and spaces come only from `foundation.css`.
- One `--radius` sets every corner. A part never gets its own radius variable.
- State comes from what HTML has: `disabled`, `aria-*`, `:checked`, `open`,
  `data-state`.

## Theme

A theme is a list of CSS variables. Each color is one line with its light and
its dark value. The editor in the showroom writes that list.

Light and dark follow the system with no script. To let people choose, load
`kit.js` in the head without `defer` and add a button with `data-mode-toggle`.
For one mode only, put `class="dark"` or `class="light"` on `<html>`.

The kit ships no fonts and uses
the system's. Pick another font in the theme editor and Copy theme writes out
how to add it: `<link rel="preload">`, `@font-face` with `font-display:
optional` so the text never changes font while it loads, and the variable.
Every font in `showroom/fonts/` is under the SIL Open Font License, its text is
next to it. The theme editor's colors and their notices are in
`showroom/THEMES.md`.
