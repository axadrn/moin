# Changelog

moin UI is experimental. It can change or disappear at any time. It has the version
of the moin release it ships with.

## Unreleased

**In short:** The first version of moin UI, native HTML, CSS and JS for any stack.

### New

- **Components**: Every component as one CSS file, with its examples and a page of docs.
- **Blocks**: Whole pages to start from, only HTML to copy.
- **Themes**: A theme editor with light and dark, and twelve well-known dev themes.
- **Tabs with an icon and a dot**: A tab can start with an icon, and `data-live` puts a green dot after it: the one in use.
- **A folder icon**: `moin-icon-folder`.
- **A hint in an item**: `moin-item-hint` puts a sentence under an item's words that wraps.
- **A pick opens its part**: In a list, `data-picked` after a pick item shows only while that one is picked, with no script.
- **A footer with any number of columns**: On a wide screen every column of links sits next to the brand, however many there are.

### Upgrade

Copy the new `ui/` over yours, then read `git diff` and keep what you changed:
`curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui`
