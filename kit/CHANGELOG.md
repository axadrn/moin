# Changelog

Kit is experimental. It can change or disappear at any time. It has the version
of the moin release it ships with.

## Unreleased

**In short:** The first version of Kit, native HTML, CSS and JS for any stack.

### New

- **Parts**: Every part as one CSS file, with its examples and a page of docs.
- **Rooms**: Whole pages to start from, only HTML to copy.
- **Themes**: A theme editor with light and dark, and twelve well-known dev themes.

### Upgrade

Copy the new `kit/` over yours, then read `git diff` and keep what you changed:
`curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/kit`
