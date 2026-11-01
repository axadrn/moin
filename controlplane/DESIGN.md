# ops design

One product in four places: the landing page, the control panel, the TUI and
the CLI. They share these rules. Change a token here and in the code at once.

## Character

**Native.** It should feel like an Apple system app (Settings, Music, Finder
in iOS and macOS), not a website and not a dashboard template. The owner
chose this look from three candidates (ops Looks artifact).

- **Grouped and rounded.** Content sits in grouped lists and cards with big
  radii (20–22 px on panels, 12 px on rows), soft system grays, no hairline
  borders between panels; separators only between rows inside a group.
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
- **Selection** is a filled pill (white/black) in the sidebar, a soft gray
  row in lists. Generous padding inside every rounded shape: text never sits
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
| `terminal`, `terminal-foreground` | log and command blocks, in both themes |
| neutrals | `background`, `card`, `muted`, `border`: pure grays, no tint |

Tailwind: `font-display` (Geist 800, tight), `bg-brand text-brand-foreground`, `text-live`,
`bg-live/12`, `text-running`, `text-failed`, `bg-terminal`. Numbers
`tabular-nums`.

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
