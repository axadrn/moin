# moin design

One product in two places: the website and the CLI. They share these
rules. Change a token here and in the code at once.

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
  sideways scrolling.
- Real product, real output. The landing page keeps its floating pill
  navbar.
- **The harbor.** moin is Hamburg, and shipping containers are app
  containers. The landing page may draw that, as hand-built SVG only (no
  generated images): simple containers (a steel face and its ribs) with
  the app they carry stencilled big, like `postgres`. Grays from the tokens
  draw the scene, the user's app in ink; color stays status
  and long flat shadows give depth. `moin` appears only in the logo, never
  on a ship or a container. Light enough to read on both grounds, simple
  shapes, still. The home page's story card (the name and where it comes
  from, to read, in the one roomy card; the way in follows it without a
  card) ends on a wall of containers:
  `homeHarbor` in `internal/ui/pages/marketing_harbor.templ`: 20-foot
  containers from the side, staggered, only your-app and two others pulled
  out, the whole wall resting on the card's bottom edge.
- **The transcript.** Under the headline, in the first screen, the
  landing page shows a real deploy as terminal output (`components.Code`):
  real moin only, no made-up numbers.

Never: an accent color, gradients, glows or blurred blobs, glassmorphism,
emoji, sparkles, rocket icons, three generic icon cards in a row, small
uppercase mono "eyebrow" labels over every section, stretched type, stock
phrases ("supercharge", "unleash", "seamless", "effortless",
"revolutionize", "next-generation"), fake testimonials, made-up numbers.

## Tokens (assets/css/site.css)

| Token | Use |
| --- | --- |
| `brand` | the ink: primary buttons, active nav, focus, selection. Foreground color, not a hue |
| `live` | online, live, succeeded |
| `running` | running, deploying |
| `failed` | failed, offline, destructive |
| `idle` | new, unknown, neutral |
| `terminal`, `terminal-foreground` | log and command blocks, in both themes. One step lighter than anything it sits on: dark on light pages, `#2c2c2e` over the black page and `#1c1c1e` cards in dark |
| neutrals | `background`, `card`, `muted`, `border`: pure grays, no tint |

Classes: `moin-display` (Geist 800, tight), `moin-live`, `moin-running`,
`moin-failed`. Numbers `moin-digits`.

**Logo.** The wordmark `moin.`, drawn as a shape: lowercase, with a round full
stop, round like the dot on the i. `components.Wordmark` on the page;
`assets/img/logo.svg`, the favicon (`m.` white on a black tile) and the OG
image are the same shapes. The story behind the name is Why moin in the FAQ.

## Geometry

One system, in one place: `assets/css/site.css`, and its Go side in
`internal/ui/components`. A page gives a component its content, never a
height, radius, padding or color. To change the look, change the
component, not a page.

| Part | Value |
| --- | --- |
| Everything you press: buttons, fields, segments, chips, the search | 44 px high, `--radius-md` |
| Groups: cards, lists, dialogs, the palette | `--radius-lg`, 24 px inside |
| Inside a group: rows, boxes, a log | the group's radius minus the gap to it, so the curves run parallel |
| Page | `moin-page`: one width, 16 px gutters on phones, 24 px from a tablet up |

Overlays (the search palette) float:
one step lighter than what they cover (`--popover`: white in light, #2c2c2e
in dark), a shadow, no border.

What the pointer is on is lit with `--hover`, the foreground at 8 %, and
pressed with `--active`, at 12 %: cards, rows, items, everywhere. One value
in `theme.css`, so it shows on any surface. On a white card in light mode
that is a step darker than the page, as in iOS.

Never a card in a card. Marketing illustrations (terminal, chat, the
product frame) keep their own shapes.

## Navigation

Every page is a full page load; there is no hx-boost and no client router.
A link changes the page. htmx only answers the search palette. Scripts
live in `assets/js/site.js` and listen on `document`; no page script.

## Status

Every status shows as a colored dot or symbol plus a word, everywhere:

| Kind | Symbol | Words |
| --- | --- | --- |
| live | ● | online, live, the live deploy |
| running | ◐ (animated where possible) | running, deploying |
| done | ✓ muted | a deploy that succeeded but is no longer live |
| failed | ✗ | failed, offline |
| idle | ○ | new, unknown |

## Terminal (moin/internal/style)

The TUI and the CLI use `internal/style`: the status colors as hex, the
symbols above, and `style.Badge`. The accent is bold text and the brand mark
reverse video, so it works on light and dark terminals. Colors only when stdout is a
terminal and `NO_COLOR` is unset; CI logs stay plain text.

CLI output: one line per fact, the result last. `✓` for done, `✗` with the
reason for failures, `→` for steps, logs indented and dimmed, the next command
to run in bold.

## Words

One thing, one word, on every surface and in every layer: the site, the
CLI, the docs, and the code under them. moin is in beta: a rename is a
clean break, no alias and no redirect; the release notes say it under
Changed.

| Word | Means | Not |
| --- | --- | --- |
| server | a machine you reach with `ssh`; its name is your SSH alias, like `my-server` | node, host, machine (in copy) |
| daemon | the moin process on a server (`moin daemon`): backups, checks, follows, reports | agent, node agent, connector |
| app | what you deploy | project, service (a service is one container of an app's compose file) |
| deploy | one version of an app, v12; also the verb | release (in copy), build, rollout |
| domain | a name the app answers on, the instant address included | address, route, routing (in copy) |
| backup | a copy of an app's database in your bucket, on a schedule | snapshot, dump, history, point in time |
| restore | going back to a copy; it can be undone as long as the copies are kept | rollback (that is for deploys), recover |
| check | only the daily test that the newest copy restores | drill, verification, restore check |
| bucket | the S3-compatible bucket you bring, for backups | storage, S3 (alone), object storage (in copy) |
| watcher | what receives the reports of the daemons and alerts when a server goes silent (`moin watcher`) | monitor, dashboard, hub, control plane |
| moin cloud | the watcher and the write-only copy that moin runs for you | hub, hosted hub, SaaS |
| catalog | the open source apps you deploy with one command; its items are apps | apps (for the place), templates, marketplace |
| changelog | what changed in each version of moin | what's new, release notes |

Examples everywhere: the server `my-server` (two: `staging` and
`production`), the app `my-app` (a second one `blog`), the domain
`my-app.example.com`.

## Copy

Short, concrete, active. Say what happens: "Deploy v4 of blog is live", not
"Your deployment has been successfully completed". Show the command people
type next.

Titles and subtitles carry no commas. Short sentences with full stops:
"Deploy anything. Lose nothing." A section has one title and at most one
short subtitle.

Who speaks:

- Blog: Axel writes it, as "I", with his name as the author.
- Site and docs: the product and the reader. "moin does …", "your server". No "I", no "we".
- "We" only where the company promises or answers for something: moin cloud, privacy, terms, prices, "we can't read your backups".
