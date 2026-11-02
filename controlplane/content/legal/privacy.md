---
title: Privacy Policy
description: What ops stores, why, where it lives and how to get rid of it
lastUpdated: November 2, 2026
---

Short version: ops stores what it needs to run your servers and apps, encrypts every secret, uses no tracking cookies and no advertising, and deletes your data when you delete your account. Your apps and their data stay on your own servers.

## Who we are

ops is responsible for the data described here. Write to [{{support_email}}](mailto:{{support_email}}) about anything in this policy.

## Website visits

Every request is processed with technically necessary data: IP address, time, the requested page, browser and operating system. The server logs them for security and troubleshooting. Legal basis is our legitimate interest in a secure, working service (Art. 6(1)(f) GDPR).

The website runs on a server of Hetzner Online GmbH in Germany. Cloudflare, Inc. (San Francisco, USA) sits in front of it as a proxy and protects it against attacks. Cloudflare is certified under the EU-US Data Privacy Framework and bound by a data processing agreement.

## Cookies and browser storage

ops sets only cookies it needs to work: a session cookie that keeps you signed in, a CSRF cookie that protects forms, and a short-lived state cookie while you sign in with GitHub. There are no analytics, advertising or tracking cookies, so there is no cookie banner. Page views on the hosted ops are counted with Umami on our own server; Umami sets no cookies and stores no IP addresses, and we only see totals, such as which pages are read and which site sent a visitor. A control plane you host yourself counts nothing unless you set it up. Your light or dark theme choice stays in your browser's local storage and is never sent to us.

## Your account

To create an account we store your email address and, if you add them, your name and avatar. You sign in with a link sent by email or with GitHub; with GitHub we receive your GitHub user ID, username and verified email address. Sessions and API tokens are stored only as cryptographic hashes. Legal basis is the contract with you (Art. 6(1)(b) GDPR).

Emails such as sign-in links are sent through Cloudflare Email Sending.

## Servers, apps and deploys

To run your apps ops stores:

- **Servers**: name, hostname, operating system, architecture, agent version and the IP address the agent connects from. The agent token is stored as a hash.
- **Apps**: name, domains, routing settings, deploy history, deploy logs and what the agent detected about the app, such as its services, port and SQLite databases.
- **Code**: never. `ops deploy` puts your code into your own bucket through a link ops signs, and your server deletes it there after downloading; deploys from GitHub go from GitHub straight to your server.
- **Secrets**: env vars, registry passwords, the secret key of your backup bucket, your Cloudflare API token, Cloudflare tunnel tokens and your Tailscale auth key are encrypted with AES-256-GCM before they are stored. Deploy hook tokens are stored as SHA-256 hashes, protection passwords as bcrypt hashes.

Your apps, their files and their databases live on your servers, not with us. When you open the logs or the data of an app, ops fetches them from your server for that one view and does not keep them. Backups go from your server straight to the bucket you configure, never through ops; ops keeps only their status, such as when the last restore drill passed.

If your apps hold personal data of other people, you are the controller for that data and ops processes it only on your instructions. Ask at [{{support_email}}](mailto:{{support_email}}) for a data processing agreement.

## Activity log

Each workspace keeps a log of who changed what, so a team can see who deployed, restored or changed a setting. Each entry holds the time, the member's email or the API token's name, how the change came in (web, CLI, terminal UI, API, MCP, GitHub or a deploy hook), the IP address and browser or program of the request, and what changed. It never holds env var values, secrets, your SQL or the values in your database. Every member of the workspace sees the log; only admins and owners see IP addresses and browsers. The log stays as long as the workspace exists. When you delete your account, your email stays on your earlier entries, so the team still knows who made them. Legal basis is our and your team's legitimate interest in a secure, accountable service (Art. 6(1)(f) GDPR).

## Payments

Payments run through Stripe Managed Payments. Stripe is the seller of record: it takes your payment details, handles tax, invoices and receipts, and has its own [privacy policy](https://stripe.com/privacy). We never see your card number. We store the Stripe customer and subscription reference, your plan and its status, so we know which features your account has.

## Retention

We keep your data while your account exists. Deleting your account deletes your servers, apps, deploys, env vars and tokens. Your email stays on your entries in the activity log, as described above. Payment records stay with Stripe for as long as tax law requires.

## Your rights

You can ask for access to your data, have it corrected, deleted, restricted or handed over, and object to its processing (Art. 15 to 21 GDPR). An email to [{{support_email}}](mailto:{{support_email}}) is enough. You can also complain to a data protection authority.

## Changes

When this policy changes, this page and its date change with it.
