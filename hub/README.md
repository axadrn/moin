# moin site

The website at moin.run: the landing page, the docs, the blog, the
changelog, the catalog, the comparisons and the legal pages. No database,
no sign-in.

Read `DESIGN.md` for how it looks. The docs are in `content/docs`, served at
`/docs`: the only source of truth for how moin works.

## Develop

```bash
task dev   # the site on a free port, with hot reload
```

The image builds from the repo root: `docker build .`

## License

Open source: [what the licenses cover](https://moin.run/docs/faq#is-moin-open-source).
