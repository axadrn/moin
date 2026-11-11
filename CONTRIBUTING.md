# Contributing

Thanks for helping. Read `AGENTS.md` for how the code is built and tested.

## Issue first

Open an issue before you write code. We agree there on what to build and how.
Pull requests without an agreed issue get closed. Small fixes, like a typo or a
broken link, are welcome anytime.

## How a change gets in

Every change goes through a pull request, from maintainers too. Nobody pushes
to `main`.

1. One branch per change.
2. A pull request. CI runs on every push to it and has to turn green.
3. A maintainer reviews it, to the best of their knowledge, with AI where it
   helps.
4. A squash merge: the title and description of the pull request become the
   one commit on `main`. The single commits stay on the pull request's page.

## License of your contribution

Your contribution comes in under the Apache License 2.0, so the project can
keep choosing its license later. You keep the copyright of your work.

The pull request template says so in one line. Leave it in. Without it, a check
turns red and tells you what to add.

## Commits and releases

Pull request titles and commits follow [Conventional Commits](https://www.conventionalcommits.org):

```
feat: stop, start and restart apps
fix(tui): say stopping at once
docs: why AGPL and how to contribute
```

`feat`, `fix`, `docs`, `chore`, `refactor` or `test`, an optional scope, a short first line. Release notes list what is new, what is fixed and how to upgrade.

## Never commit

Secrets, `.env` files, customer data or private details.
