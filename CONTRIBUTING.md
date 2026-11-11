# Contributing

Thanks for helping. `AGENTS.md` says how the code is built and tested.

## Issue first

Open an issue before you write code. We agree there on what to build.
Pull requests without an agreed issue get closed. A typo or a broken link is
welcome anytime. Maintainers may skip the issue for their own work.

## Small pull requests

One pull request is one thing. Small enough to read in a few minutes.
Big ones get closed: split them.

## AI is fine. Slop is not.

We use AI for almost everything: code, commit messages, descriptions. We
don't read every line it writes. We read what matters, keep the code
readable and understand what a change does and why.

Do the same:

- You know what your change does and why, and can explain any part of it.
- The description is short and says what and why, in your own words.
- No walls of generated text.

If we cannot follow a pull request, we close it.

## How a change gets in

Everyone, maintainers too:

1. A branch and a pull request. Nobody pushes to `main`.
2. CI turns green.
3. A maintainer reviews it.
4. Squash merge: the title and description become the one commit on `main`.

The title follows [Conventional Commits](https://www.conventionalcommits.org):
`feat:`, `fix:`, `docs:`, `chore:`, `refactor:` or `test:`, an optional
scope, short. Like `fix(tui): say stopping at once`. Commits on your branch
can say anything. They get squashed.

## License

Your contribution comes in under the Apache License 2.0, so the project can
keep choosing its license later. You keep the copyright. The pull request
template says so in one line. Leave it in.

## Never commit

Secrets, `.env` files, customer data or private details.
