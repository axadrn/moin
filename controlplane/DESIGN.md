# moin design

One product in four places: the landing page, the control panel, the TUI and
the CLI. They share these rules. Change a token here and in the code at once.

## Character

**Native.** It should feel like an Apple system app (Settings, Music, Finder
in iOS and macOS), not a website and not a dashboard template.

- **Calm.** One big answer per page, one primary button, the rest quiet.
  Few words: a title, the content, a button. A sentence only when nothing
  else says what happens. The app explains itself; the docs explain ideas.
- **Rounded.** Everything you press is a capsule; everything that groups
  content has big corners; inside, the radius is smaller (see Geometry).
- **Dark:** pure black ground (#000), grouped surfaces #1c1c1e, controls
  #2c2c2e, text white, secondary #98989f. **Light:** ground #f2f2f7,
  surfaces white, controls #e5e5ea, text black, secondary #6e6e73. Both are
  first-class.
- **Monochrome brand.** Color only carries meaning: green live, amber
  running, red failed. The primary action is an inverted pill (white on
  black in dark, black on white in light); every other button is a gray
  pill. One primary per page.
- **Alive.** Live dots pulse softly, running states blink, changes swap in
  place without layout jumps. Reduced motion turns it off.
- **Phone first.** Every page works with a thumb: 44 px targets, no
  sideways scrolling, a list that drills down where the desktop shows tabs.
- Real product, real output. The landing page keeps its floating pill
  navbar.
- **The harbor.** moin is Hamburg, and shipping containers are app
  containers. The landing page may draw that, as hand-built SVG only (no
  generated images): simple containers (a steel face and its ribs) with
  the app they carry stencilled big, like `postgres`. Grays from the tokens
  draw the scene, the user's app in ink; color stays status
  and long flat shadows give depth. `moin` appears only in the logo, never
  on a ship or a container. Light enough to read on both grounds, simple
  shapes, still. The hero is a wall of containers under the headline:
  `homeHarbor` in `internal/ui/pages/marketing_harbor.templ`: 20-foot
  containers from the side, staggered, only your-app and two others pulled
  out, the whole wall resting on the foot of the first screen.

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

**Logo.** The wordmark `moin.` in Satoshi Black (`font-wordmark`, only
there), lowercase, with Satoshi's own round full stop, round like the dot
on the i. `blocks.Wordmark` on the page;
`assets/img/logo.svg`, the favicon (`m.` white on a black tile) and the OG
image are the same shapes. The story behind the name is Why moin in the FAQ.

## Geometry

One system, in one place: the kit (`internal/ui/kit`). A page gives a kit
part its content, never a height, radius, padding or color. To change the
look, change the kit, not a page. `/dev/kit` (only with `APP_ENV=development`)
shows every part in every state, light and dark: check a part there once
instead of on every page.

| Part | Value |
| --- | --- |
| Everything you press: buttons, fields, segments, chips, the search | 44 px high, a capsule (`rounded-full`) |
| Groups: cards, lists, dialogs, the palette | `rounded-4xl`, 24 px inside |
| Inside a group: rows, boxes, a log | the group's radius minus its padding (`rounded-3xl`), so the curves run parallel |
| Page | `kit.Page`: one width (`max-w-5xl`), 16 px gutters on phones, 24 px from `sm` |

Never a card in a card. Marketing illustrations (terminal, chat, the
product frame) keep their own shapes.

## Navigation

Every page is a full page load; there is no hx-boost and no client router.

- **A link changes the page.** Tabs, rows, the bar and the palette are plain
  `<a href>` links to the page's own URL. `@view-transition { navigation:
  auto; }` holds the old page until the new one paints, so nothing flashes.
- **htmx only acts on the page:** save, delete, a switch, a search, a part
  that reloads itself. Never to switch pages.
- **One tab bar per page at most** (`kit.Segmented`). A second level is a
  list that drills down to its own page ("‹ shop" back), never a second tab
  bar. Filters that only narrow (a time range) are a `kit.Select`.
- **On a phone** an app's sections are a list on its first page, each row
  with its value ("Deploys · v12 live ›"); on the desktop they are the tab
  bar. The same links either way.
- **The bar** is the wordmark (home), the workspace name, the search and
  your picture. The workspace and your picture both open Settings, a page,
  not a menu.
- **The palette** (`⌘K`, `ctrl+k`, `/` or the search in the bar) opens in
  the middle of the screen, a native `<dialog>`. It finds apps, servers,
  places and actions, and shows each one's key. The same palette is in the
  TUI. There is no which-key menu.
- **Live parts** use `kit.Live(url)`, the only way a page opens a stream
  (hx-sse). It closes when the page is left, and a page back from the
  back-forward cache loads fresh (`moin.js`). A part that reloads itself
  has `hx-get` and `hx-trigger="<event> from:body"` and renders its content
  with the page; it is never empty until an event fills it.
- **Scripts** live in the component that needs them (`templ.OnceHandle`)
  or in `assets/js/moin.js` when every page needs them. Both listen on
  `document`, so HTML that htmx swaps in works at once. No page script.

## Pages

Rules every page follows:

- **For dummies.** Someone who opens a page for the first time sees what it
  is, what state things are in and the one thing to do next.
- **One question per page.** Its answer is the first thing on it, big: an
  app's head says "Live · v12 · 2 min ago" or "v13 failed" in the page
  title size, with the one primary action next to it.
- **Five sizes, no others.** No `text-[…px]`, no other `text-*` size on a
  page:

  | Name | Classes | Use |
  | --- | --- | --- |
  | page title | `text-3xl font-semibold tracking-tight` | the `h1`, once per page |
  | number | `text-2xl font-semibold tracking-tight tabular-nums` | a value that is the point |
  | section title | `text-base font-medium` | card titles |
  | body | `text-sm` | everything else (muted with `text-muted-foreground`) |
  | small | `text-xs text-muted-foreground` | meta next to a value: times, counts |

- **Families.** Geist (`font-sans`) for everything; Geist Mono (`font-mono`)
  only for code, commands, IDs, keys, commits, paths and log lines.
- **Parts.** `kit.Card` (a topic with its title and at most one action),
  `kit.List` and `kit.Row` (grouped rows, a line between rows, inset like
  iOS), `kit.Section` (a quiet title over a list), `kit.Grid` (cards side by
  side, as high as the highest), `kit.Field`, `kit.Input`, `kit.Select`,
  `kit.Choice`, `kit.Segmented`, `kit.Button`, `kit.Command`.
- **No explaining.** No description line under a title, no help icon next
  to it. A card about an empty thing says what is missing in one sentence
  and shows the one button or command that fills it.
- **Danger at the end.** Stop, remove and disconnect sit at the bottom of
  Settings, as red text buttons that ask first.
- **Text.** Muted for secondary facts, `tabular-nums` for numbers and
  times. Times are relative ("3h ago") with the full stamp as `title`.

## Feedback

Every answer to a click or a form takes one of these shapes, the same on
every page. Nothing on the page moves to make room for it. There are no
toasts.

- **If it worked:** the button that did it says so: "Save" turns into
  "Saved ✓" for a moment and back, at the same size (`kit.Done`). What
  changed on the page is the rest of the answer.
- **If a field is wrong** (empty, too long, taken, not a domain): the field
  turns red and takes the focus, what was typed stays, and the line under
  it says why in one short sentence (`kit.Field`). A field in one row with
  its button has that line too.
- **If it failed and no field is to blame** (agent offline, not allowed):
  a red line under the button that sent it, in place, until the next try.
  The page stays as it was.
- **If a control saves itself** (a switch, a cell): its new state is the
  answer. If saving fails, it goes back and says why under it.
- **If something was copied:** the copy icon turns into a check for a
  moment.
- **If a state lasts** while the page is open (trial ended, read-only
  workspace, server offline): a line in the page, rendered with the page.
- **If a request runs:** the button that sent it is disabled until the
  answer (`hx-disable`) and keeps its size; a running deploy or restore
  shows the running status. No spinner that moves anything.
- **Saving never restarts an app.** Changes that need a restart wait; the
  app's head says "2 changes waiting" next to Restart.

Messages read like the CLI: short and concrete, never "Success", "Error" or
"successfully".

## Asking first

Three tiers, the same on every surface. The API never asks: the caller
decides.

1. **Big and cannot be undone** (delete the workspace or the account,
   remove an app, a server or the bucket, restore over live data): the
   name must be typed out. Web: the confirm dialog shows the name with a
   copy button and the button works only once it matches. CLI: "Type
   blog to confirm:", `--yes` skips it for scripts. TUI: type the name,
   then `enter`; paste works. MCP: destructive, and the tool takes
   `confirm`, the name. Deleting the account asks for DELETE.
2. **Reversible or small** (stop an app, roll back, revoke a token or an
   invitation, remove a domain, an env var, a registry login, a key, a
   member or the password, unlink a repo, a new or no deploy hook, change
   how a server is reached): one question. Web: the confirm dialog. CLI:
   `[y/N]`, `--yes` skips it. TUI: `y`. MCP: destructive.
3. **Harmless** (deploy, redeploy, restart, start, back up, add or set
   something): no question.

Web: put the question in `hx-confirm` ("Remove blog? Its data stays."):
up to the first "? " is the title, the rest the description. The dialog
(`blocks.ConfirmDialog`, in the base layout) repeats the label of the
clicked button, or `data-confirm-action`. Add `data-confirm-name` for
tier 1. Never `window.confirm` and no question dialog of a page's own;
only the account keeps its dialog, as the server checks the DELETE. The
confirm dialog, the palette and the list of `kit.Select` are the only
overlays: no menus, popovers or
sheets. CLI:
`confirm` and `confirmName` in `internal/cli/args.go`; TUI: `confirmFirst`
and `confirmName`; MCP: `confirmed`.

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

## Activity

Every change in a workspace goes through one helper, `record` in `internal/service/activity.go`, with a `service.Actor`: the member, how it came in (`Source`: web, cli, tui, api, mcp, hook, github or system, the API token by name, the client IP and user agent) and what it touched. moin itself acts as `service.System(workspaceID)`.

- **What we log:** the exact time in UTC, who, how, the app and server, and the settings before and after: names, on and off, domains, branches, roles, counts, durations, versions, release numbers, exit codes and error messages.
- **What we never log:** env var values, secrets, tokens, passwords, SQL text, row contents or cell values, file contents and app log lines. A SQL write keeps the kind of statement, its tables and the row count; a row edit the table, row and column names.
- **Showing it:** seconds and the time zone everywhere (UTC on the web, local in the CLI and TUI, RFC 3339 in the API and MCP). IP and user agent only for admins and owners.
- `TestActivityNeverHoldsSecrets` guards the rule. A new action gets a sentence in `WorkspaceAuditEvent.Summary`.

## Terminal (moin/internal/style)

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
