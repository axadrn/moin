# Contributing

Thanks for helping. Read `AGENTS.md` for how the code is built and tested.

## Issue first

Open an issue before you write code. We agree there on what to build and how.
Pull requests without an agreed issue get closed. Small fixes, like a typo or a
broken link, are welcome anytime.

## License of your contribution

moin is licensed under AGPL-3.0. Your contribution comes in under the
Apache License 2.0, so the project can keep choosing its license later. You
keep the copyright of your work.

On your first pull request a bot asks you to agree to the
[Contributor License Agreement](CLA.md). Agree once with a comment:

```
I have read the CLA Document and I hereby sign the CLA
```

It covers every later pull request. A pull request without it cannot be
merged.

## Commits and releases

Commits follow [Conventional Commits](https://www.conventionalcommits.org):

```
feat: stop, start and restart apps
fix(tui): say stopping at once
docs: why AGPL and how to contribute
```

`feat`, `fix`, `docs`, `chore`, `refactor` or `test`, an optional scope, a short first line. Release notes list what is new, what is fixed and how to upgrade.

## Never commit

Secrets, `.env` files, customer data or private details.
