# moin

Deploy anything. Lose nothing.

One command puts your app on your own server. Your backups are restored and checked every day. Nothing but your SSH key can command your server.

```bash
curl -fsSL https://moin.run/cli.sh | sh
cd shop
moin deploy prod     # ✓ shop v1 is live: https://shop.203-0-113-7.sslip.io
moin                 # every server, every app, what needs you
moin backups shop    # the copies, and the last restore check
```

If `ssh prod` works, `moin deploy prod` works. No account, no dashboard, no Docker on your laptop.

A watcher on another machine alerts you when a server goes silent. moin cloud is the watcher we run: `moin watcher connect prod`. It keeps a copy of every database that a hacked server can't delete.

## Documentation

Everything else is in the docs: [moin.run/docs](https://moin.run/docs).

News on X: [@usemoin](https://x.com/usemoin).

## License

Open source, all of it: [what the license covers](https://moin.run/docs/faq#is-moin-open-source). Contributions: [CONTRIBUTING.md](CONTRIBUTING.md). The name and the logo: [TRADEMARKS.md](TRADEMARKS.md).
