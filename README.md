# moin

Your `./deploy.sh`. But your data comes back.

One command puts your app on your server. Every backup is restored and checked, every day. No dashboard holds a key to your server.

```bash
curl -fsSL https://moin.run/cli.sh | sh
cd shop
moin deploy prod     # ✓ moin. shop is live: https://shop.203-0-113-7.sslip.io
```

If `ssh prod` works, `moin deploy prod` works.

## Documentation

Everything else is in the docs: [moin.run/docs](https://moin.run/docs). They are the source of truth.

News on X: [@usemoin](https://x.com/usemoin).

## Build

```bash
go install github.com/axadrn/moin/cmd/moin@latest
```

## License

Open source: [what the license covers](https://moin.run/docs/faq#is-moin-open-source). See also [CONTRIBUTING.md](CONTRIBUTING.md). The license covers the code, not the name or the logo: see [TRADEMARKS.md](TRADEMARKS.md).
