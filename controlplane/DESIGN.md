# ops design

One product in four places: the landing page, the control panel, the TUI and
the CLI. They share these rules. Change a token here and in the code at once.

## Character

**Native.** It should feel like an Apple system app (Settings, Music, Finder
in iOS and macOS), not a website and not a dashboard template. The owner
chose this look from three candidates (ops Looks artifact).

- **Grouped and rounded.** Content sits in grouped lists and cards with big
  radii (see Geometry), soft system grays; separators only between rows
  inside a group.
- **Large titles.** Page titles big and heavy (`font-display`: Geist 800,
  tracking -0.03em), panel titles 20 px bold, like iOS large titles and
  section headers.
- **Dark:** pure black ground (#000), grouped surfaces #1c1c1e, controls
  #2c2c2e, text white, secondary #98989f. **Light:** ground #f2f2f7,
  surfaces white, controls #e5e5ea, text black, secondary #6e6e73. Both are
  first-class.
- **Monochrome brand.** Color only carries status: green live, amber
  running, red failed, as filled soft chips with a dot and the word.
  Primary action is an inverted pill (white on black in dark, black on white
  in light); secondary actions are gray pills. One primary per view.
- **Selection** is a soft gray row, in the sidebar and in lists. The sidebar
  is shadcn's sidebar-07: workspace switcher on top, the places and the apps
  in groups, the user at the bottom, collapsible to icons. Generous padding inside every rounded shape: text never sits
  close to a rounded edge.
- **Alive.** Live dots pulse softly, running states blink, changes swap in
  place without layout jumps. Reduced motion turns it off.
- Real product, real output, no illustrations. The landing page keeps its
  floating pill navbar.

Never: an accent color, gradients, glows or blurred blobs, glassmorphism,
emoji, sparkles, rocket icons, three generic icon cards in a row, small
uppercase mono "eyebrow" labels over every section, stretched type, stock
phrases ("supercharge", "unleash", "seamless", "effortless",
"revolutionize", "next-generation"), fake testimonials, made-up numbers.

## Tokens (assets/css/globals.css)

| Token | Use |
| --- | --- |
| `brand` | the ink: primary buttons, active nav, focus, selection. Foreground color, not a hue |
| `live` | online, live, succeeded |
| `running` | running, deploying |
| `failed` | failed, offline, destructive |
| `idle` | new, unknown, neutral |
| `terminal`, `terminal-foreground` | log and command blocks, in both themes. One step lighter than anything it sits on: dark on light pages, `#2c2c2e` over the black page and `#1c1c1e` cards in dark |
| neutrals | `background`, `card`, `muted`, `border`: pure grays, no tint |

Tailwind: `font-display` (Geist 800, tight), `bg-brand text-brand-foreground`, `text-live`,
`bg-live/12`, `text-running`, `text-failed`, `bg-terminal`. Numbers
`tabular-nums`.

## Geometry

One system, set globally, never per page. The components come from
shadcn-templ in the `base-luma` style (`components.json`, applied with
`shadcn-templ apply bKsE3bpA`: luma, stone, Geist, lucide), the corners from
one token, `--radius: 0.5rem` in `assets/css/globals.css`, with the radius
scale of the shadcn-templ theming docs (`sm` 0.6×, `md` 0.8×, `lg` 1×, `xl`
1.4×, `2xl` 1.8×, `3xl` 2.2×, `4xl` 2.6×).

| Part | Value |
| --- | --- |
| Input, select trigger, button (default), next to each other | 36 px high, pill |
| Button `sm` / `lg` | 32 px / 40 px, pill |
| Textarea, menu and command items | `rounded-2xl` (14.4 px) |
| Select, dropdown and popover menus | `rounded-3xl` (17.6 px), 6 px padding around the items |
| Cards, dialogs, panels | `rounded-4xl` (20.8 px) |
| Boxes and rows inside a card | `rounded-2xl` or `rounded-xl` |

Pages pass layout classes (width, margins, `font-mono`) to components, never
heights, radii, paddings or colors of a control. Every select puts its items
in `select.Group` (with a `select.Label` when nothing else names the list), every
dropdown its items in `dropdown.Group`, so the open menu keeps space around the
highlighted item. To change the look, change the style or `--radius`, not a
page. Marketing illustrations (terminal, chat, the product frame) keep their
own shapes.

## Navigation

htmx boosted navigation (swap `#main`) for every click inside the app:
sidebar nav, app list, tabs, settings. Full page loads only where the
context changes: sign in/out, switching workspace, the Stripe checkout
redirect, and the first load.

How it works (`internal/ui/layouts/app.templ`, `appBoost`):

- The body of every control panel page carries
  `hx-boost:inherited='{"target":"#main","select":"#main","swap":"outerHTML","selectOOB":"#sidebar-nav,#app-header"}'`.
  Every plain link and form inside the app is boosted without any attribute
  of its own: htmx fetches the full page, swaps its `#main` and brings the
  sidebar places (`#sidebar-nav`) and the top bar with the path
  (`#app-header`) along, pushes the URL and updates the title. The apps list
  reloads itself after every push. Reload, back and a link opened directly
  give the full page, as every route still renders one.
- A tab is a plain `<a href>` to its own URL that renders the full page with
  that tab open. Do not add `hx-get`, `hx-target` or `hx-push-url` to it: the
  boost does all of it. Elements with their own `hx-get`/`hx-post` are not
  boosted and keep working as before.
- A part that refreshes itself (`hx-get` with `hx-trigger="… from:body"`)
  renders its content with the page and may carry `hx-swap-oob="true"` for
  its fragment answer: a boosted answer drops those markers, so the part
  stays in `#main`. Never leave a part empty until an event fills it.
- A link that changes the context opts out with `hx-boost="false"`: another
  workspace, `/workspaces/new`, `/admin`, sign in and out. A link to a page
  outside the panel (no `#main`) loads as a full page by itself; a link with
  `target="_blank"`, `mailto:` or another origin is never boosted. Forms
  that end in a redirect (checkout, sign out) answer with `HX-Redirect`
  through `redirect()`, which loads the whole page.
- Scripts in swapped content run again with the page's nonce
  (`inlineScriptNonce`). A page script runs once per visit, so it binds to
  its own elements (`document.currentScript.closest(...)`), never once per
  document. When it must listen on `document` or `window`, or holds an
  `EventSource` or timer, it stops all of it when the page goes:

  ```js
  const gone = new AbortController();
  document.addEventListener("htmx:before:history:update", () => gone.abort(), { once: true });
  document.addEventListener("click", onClick, { signal: gone.signal });
  gone.signal.addEventListener("abort", () => events.close());
  ```

  Never wait for `DOMContentLoaded` or `load`: they do not fire after a swap.

## Pages

Calm like shadcn's dashboard-01, Linear and Vercel: a clear header, cards on
the page ground, few lines, the same spacing everywhere. The helpers live in
`internal/ui/pages/app_parts.templ`; use them, do not rebuild them per page.

Rules every page follows:

- **For dummies.** Clear over clever, uniform everywhere. Someone who opens
  a page for the first time sees what it is, what state things are in and
  the one thing to do next. Same thing, same look, on every page.
- **Five sizes, no others.** No `text-[…px]`, no other `text-*` size on a
  page:

  | Name | Classes | Use |
  | --- | --- | --- |
  | page title | `text-3xl font-bold tracking-tight` | the `h1`, once per page (`pageHeader`) |
  | number | `text-2xl font-semibold tracking-tight tabular-nums` | the value of a `statCard` |
  | section title | `text-base font-semibold` | card titles (`panel`), empty state titles |
  | body | `text-sm` | everything else: rows, forms, descriptions (muted with `text-muted-foreground`) |
  | small | `text-xs text-muted-foreground` | meta next to a value: times, counts, hints under a field |

- **Weights.** `font-normal` for text, `font-medium` for names in rows and
  labels, `font-semibold`/`font-bold` only for titles and numbers.
- **Families.** Geist (`font-sans`) for everything; Geist Mono (`font-mono`)
  only for code, commands, IDs, keys, commits, paths and log lines.
- **Same height.** Cards in one row have the same height: the row is a
  `grid items-stretch`, each card `h-full`.
- **One left edge.** Page title, tabs, card edges and the text in cards line
  up: the page gutter, then the card's 24 px. Nothing is indented on its own.

- **Page.** `@panelPage()` sets the width (`max-w-7xl`), the gutters and
  stacks its children with `gap-6`. Put sections straight into it. Never
  `mt-*`/`mb-*` between sections, and never inside a reusable panel.
  Every card takes the full width of the page; no narrow `max-w-*` column
  for some pages. Forms keep their fields short with grids (`lg:grid-cols-2`
  or `-3`) or `sm:max-w-sm` on a single field.
- **Forms.** A one-field form puts its button next to the field. A form
  with more fields ends with its buttons right aligned.
- **Header.** `@pageHeader(title, description) { actions }`: the page
  title, one muted body line below it, the actions right
  aligned at its bottom (below on phones). The description says what the
  page is for in one sentence. An app page uses
  `@appHeader(app, tab, description) { actions }`: the name, its status chip,
  the line, the actions, and the tabs below. A page about one server uses
  `@statusHeader(name, chip, description)` the same way, without tabs.
- **Tabs.** `@tabNav(label) { @tabLink(...) }` (`@appTabs(app, tab)` on app pages): a segmented control of plain `<a href>`
  links, one per page (boosted, see Navigation), the active one
  `aria-current="page"`. Tabs switch pages, never content in place.
- **Cards.** Every block is a `card.Card`, one per topic, built with
  `@panel(panelProps{...}) { body }`: `card.Header` with `card.Title`
  (section title), an optional `card.Description` (one muted sentence) and at most
  one `card.Action` (a small button or a "How it works" link). The card's own
  spacing (24 px) stays; never pass paddings, heights or radii to a card.
  The body is `Body: ""` (padded text and forms), `panelBodyRows` (row lists:
  rows bring `px-3`, so their text lines up with the title) or
  `panelBodyFlush` (logs and tables edge to edge).
- **Lines.** No `border-b`, `border-t`, `divide-y` or `<hr>` inside a card
  or between header and body: space separates. Rows are separated by space,
  a soft `hover:bg-muted/60` and a selected `bg-muted`, `rounded-xl`.
  Tables use `table` with its own row lines only.
- **Grid.** Cards side by side sit in `grid items-stretch gap-4`, each
  `h-full`, so their edges line up. Two equal columns from `lg`
  (`lg:grid-cols-2`), so the rows of a page share the same edges.
- **Spacing scale.** 24 px between sections and inside cards, 16 px between
  cards in a grid, 12 px between fields of a form, 8 px between a control
  and its button, 4 px between a label and its value.
- **Numbers.** `@statGrid() { @statCard(label, value, note) }`: up to four
  cards in a row, the label muted body, the value in the number size, an
  optional small note. No value is `—`, muted.
- **Status.** The state of the thing a page or row is about is a chip:
  `status.Status{Kind, Chip: true}` with the word ("live v4", "deploying
  v5", "failed"). Inside lists and timelines use the plain
  `status.Status{Kind}` (symbol and word, no pill). One chip per row.
- **Actions.** One primary (`button.VariantDefault`) per area: the page
  header or a card. Others are `VariantSecondary`; small tools in a card
  (copy, show values, load) are `SizeSm` secondary or ghost icons.
  Destructive actions (remove an app, a server) live in their own card at the
  end of the page: `@dangerPanel(title, body) { button }` with a
  `VariantDestructive` button behind a confirm. No red hover classes on gray
  buttons.
- **Empty states.** `@emptyState(title, body, cmd)`: what is missing, what
  makes it appear, and the command to type. Inside a card that already has a
  title, one muted sentence plus the command is enough:
  `@emptyHint(text, cmd)`. Never a blank card.
- **Text.** Muted for secondary facts, `tabular-nums` for numbers and
  times. Times are relative ("3h ago") with the full stamp as `title`.

## Status

Every status shows as a colored dot or symbol plus a word, everywhere:

| Kind | Symbol | Words |
| --- | --- | --- |
| live | ● | online, live, the live deploy |
| running | ◐ (animated where possible) | running, deploying |
| waiting | ○ in the running color | a server whose agent never connected |
| done | ✓ muted | a deploy that succeeded but is no longer live |
| failed | ✗ | failed, offline |
| idle | ○ | new, unknown |

## Terminal (ops/internal/style)

The TUI and the CLI use `internal/style`: the status colors as hex, the
symbols above, and `style.Badge`. The accent is bold text and the brand mark
reverse video, so it works on light and dark terminals. Colors only when stdout is a
terminal and `NO_COLOR` is unset; CI logs stay plain text.

CLI output: one line per fact, the result last. `✓` for done, `✗` with the
reason for failures, `→` for steps, logs indented and dimmed, the next command
to run in bold.

## Copy

Short, concrete, active. Say what happens: "Deploy v4 of blog is live", not
"Your deployment has been successfully completed". Show the command people
type next.
