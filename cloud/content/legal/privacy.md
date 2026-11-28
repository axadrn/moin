---
title: Privacy Policy
description: What moin and moin cloud store, why, where it lives and how to get rid of it
lastUpdated: November 27, 2026
---

Short version: the `moin` program runs on your own computers and servers and sends us nothing. The website sets no cookies, shows no ads and never sells your data. moin cloud, if you use it, keeps your mail address, the newest status of your connected servers and encrypted copies of your backups that we cannot read. Your code, your apps and their data stay on your own servers and in your own bucket.

This policy covers the website moin.run, the `moin` software you download from it, and moin cloud, the paid service we run. A watcher you run yourself is yours: it sends us nothing, and you decide what it stores.

## Who we are

moin and moin cloud are run by Berry Good Ideas LLC, 30 N Gould St Ste N, Sheridan, WY 82801, USA ("we"). We decide how the data described here is used. Write to [{{support_email}}](mailto:{{support_email}}) about anything in this policy.

## Website visits and server logs

To answer a request, the server must process your IP address, the time, the page you asked for, and your browser and operating system. The site logs the time, page and result of each request, without your IP address or your browser, for security and troubleshooting. These logs are rotated and kept only for a short time.

Downloading moin from the site, such as with `curl -fsSL https://moin.run/cli.sh | sh`, is a website visit like any other.

Legal basis: our legitimate interest in a secure, working service (Art. 6(1)(f) GDPR).

## Page views

We count page views with Umami, an open source page counter, on a server we run. Umami sets no cookies, uses your IP address only to tell the country and does not store it, and shows us only totals, such as which pages are read and which site sent a visitor. We do not use Google Analytics or any advertising network.

Legal basis: our legitimate interest in knowing which pages help (Art. 6(1)(f) GDPR).

## Posts from X

The home page shows posts from X. The text of each post is on our page, but the profile pictures and photos in them load straight from X's servers (`pbs.twimg.com`). So when your browser shows those pictures, X gets your IP address, your browser and the page you came from, like with any image from another site. What X does with it is covered by [X's privacy policy](https://x.com/privacy).

Legal basis: our legitimate interest in showing what people say about the problem moin solves (Art. 6(1)(f) GDPR).

## Cookies and browser storage

The website sets no cookies, so there is no cookie banner. moin cloud sets only cookies it needs to work, and only once you sign in:

- **session** keeps you signed in to moin cloud, for up to 30 days.
- **github_state** remembers a sign-in with GitHub, for at most 10 minutes.

Your light or dark theme stays in your browser's local storage and is never sent to us.

## The moin program

`moin` runs on your laptop and your servers. It talks to your servers over SSH, with your key, and to the watchers you choose. It sends us nothing: no usage counts, no names, no addresses. Downloading or updating it is a download from the site or from GitHub, like any other. Your apps, their files, their databases, their env vars and their backups never pass through us, unless you connect a server to moin cloud (see below).

If your apps hold personal data of other people, you are the controller for that data.

## moin cloud: your account

Only if you use moin cloud. Your account is your mail address: we store it, and nothing else about you. You sign in in one of two ways:

- **A link by mail.** We send a link that works once, for 15 minutes. To stop floods of mails, we count the links asked for per mail address and per IP address, in memory, for 15 minutes.
- **GitHub.** We ask GitHub for the verified primary mail address of your GitHub account and sign you in with it. We do not keep anything else from GitHub.

`moin watcher connect <server>` on a computer that is not signed in yet shows a code that you allow in the browser. A session in the browser lasts 30 days, one of the CLI a year. Sessions and sign-in links are stored only as cryptographic hashes.

We send only mails that belong to the service: sign-in links, alerts about your servers and apps, the end of a subscription, and important changes to moin cloud. No newsletters or marketing mails.

If you had a subscription, a coupon or an offer on the old hosted moin service, we moved that workspace to moin cloud: its id, the mail address of its owner, and its Stripe references, coupon or offer. You sign in with the same mail and keep what you paid for.

Legal basis: the contract with you (Art. 6(1)(b) GDPR).

## moin cloud: servers and reports

A server you connect to moin cloud gets a token. We store the server's name and the token as a hash. The server then sends a status report every minute. moin cloud keeps:

- **The newest report** of each server: its name and moin version, the name, release, domains and state of each app, the name and kind of each database, when its last backup and its last restore check ran, whether they worked and the error if not, and how many connections the server's firewall dropped.
- **When the last report came**, so it notices when a server goes silent.
- **The history**: outages of servers and domains and the results of restore checks, the last 100 of each.
- **Where alerts go**: your mail address, and any other target you add.

To tell whether your apps are up, moin cloud visits the domains of your running apps from outside.

A report never holds your code, your env vars, your secrets or the data in your databases.

Legal basis: the contract with you (Art. 6(1)(b) GDPR).

## moin cloud: copies of your backups

During the trial and with a subscription, your server sends moin cloud a copy of each backup. Each copy is encrypted on your server before it leaves, with a key that stays with you and your server. We never get that key, so we cannot read your copies. The copies are stored at Cloudflare R2. We keep the app, the database, the time and the size of each copy.

Each copy is kept 14 days. The bucket is locked so that nobody, not even we, can delete a copy inside those 14 days. After a trial or a subscription ends, the copies are deleted after the time the [Terms](/legal/terms) state.

Legal basis: the contract with you (Art. 6(1)(b) GDPR).

## Payments

Payments for moin cloud run through Stripe Managed Payments. Stripe is the seller of record: it takes your payment details, handles tax, invoices and receipts, and has its own [privacy policy](https://stripe.com/privacy). We pass Stripe your mail address for the checkout. We never see your card number. We store the Stripe customer and subscription reference, whether you paid and until when, and a coupon if you have one, so we know what your workspace may do.

Legal basis: the contract with you (Art. 6(1)(b) GDPR) and tax law (Art. 6(1)(c) GDPR).

## When you write to us

When you mail us, we keep your message and our answer, to help you and to keep track of what we said.

Legal basis: our legitimate interest in good support (Art. 6(1)(f) GDPR).

## Who else processes data

We share data only with the services that run moin for us, and only what each one needs:

| Service | What it does | What it gets |
| --- | --- | --- |
| Hetzner Online (EU) | Hosts the site and moin cloud | Everything moin cloud stores, on our server |
| Cloudflare (USA) | Protects the site and carries its traffic, stores the copies of your backups (R2), sends our mails (Cloudflare Email) | Every request to the site, the encrypted copies, your mail address and the mails we send you |
| Stripe (USA and EU) | Payments, tax and invoices | Your mail address and what you enter at checkout |
| GitHub (USA) | Sign-in with GitHub, downloads of moin | What you allow on GitHub; moin cloud reads only your verified mail address |
| Our mail provider | Our inbox | The mails you send us |

Umami runs on a server we control, so no other company gets the page views. X gets what your browser sends when it loads pictures from X (see Posts from X); X does not work for us.

We never sell your data, and we do not share it for advertising. We hand data to authorities only when the law forces us to.

## Data outside the EU

Cloudflare, Stripe and GitHub are based in the USA. Where they handle personal data from the EU, they are bound by the EU standard contractual clauses or certified under the EU-US Data Privacy Framework.

## How long we keep data

- **Server logs**: a short time, for security and troubleshooting.
- **Sign-in links**: 15 minutes, and they work once.
- **Sessions**: 30 days in a browser, a year for the CLI, or until you sign out.
- **Account**: while it exists. You delete it in moin cloud's Settings. Deleting it deletes your workspace, its servers, their reports and the history.
- **Reports**: only the newest one of each server. Disconnecting a server deletes its report.
- **Copies of backups**: 14 days each. After a trial or a subscription ends, they are deleted after the time the [Terms](/legal/terms) state. A copy younger than 14 days stays locked until it is 14 days old, also when you delete your account, then it expires by itself.
- **Mails**: as long as we need them to help you, then deleted. Ask and we delete them sooner.
- **Payment records**: Stripe keeps them as long as tax law requires.

## Your rights

You can ask for access to your data, a copy of it in a common format, its correction or deletion, and you can restrict or object to its processing (Art. 15 to 21 GDPR). An email to [{{support_email}}](mailto:{{support_email}}) is enough; we answer within one month. You can also complain to the data protection authority where you live.

## Security

Your secrets stay on your server. moin cloud cannot log in to your server and cannot read your copies. Tokens, sessions and sign-in links are stored only as hashes, and our server has no open port: traffic reaches it only through Cloudflare. No system is perfectly secure, though. If we learn that your data was exposed, we tell you without delay.

## Age

moin cloud is not meant for anyone under 16. If we learn that an account belongs to someone younger, we delete it.

## Changes

When this policy changes, this page and its date change with it. We tell moin cloud customers about important changes by mail.
