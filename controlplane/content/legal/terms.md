---
title: Terms of Service
description: The plain-language rules for using ops
lastUpdated: November 2, 2026
---

These terms govern ops: the website, the control plane, its API and MCP server, and the `ops` command line tool and agent. By creating an account you agree to them.

## The service

ops deploys, runs and watches apps on servers you own. You install the ops agent on your server; it dials out to the control plane and runs your deploys with Docker Compose. ops is open source under the GNU Affero General Public License 3.0. These terms cover the hosted service we run; a control plane you host yourself is yours.

## Your servers and your apps

Your servers, apps, code and data stay yours. You decide what runs on your servers and you are responsible for it: its content, its security updates, its users' data and that it is legal where you run it. The agent runs with root rights so it can manage Docker and the proxy. Install it only on servers you control.

ops helps you keep apps running, but it cannot guarantee that a deploy, a rollback or a health check succeeds, or that your server stays reachable. Keep your own backups of anything you cannot afford to lose.

## Accounts

One account belongs to one person or company. Keep your sign-in, API tokens and deploy hook URLs secret; anyone who has them can act as you. Tell us at [{{support_email}}](mailto:{{support_email}}) if you think someone else has access.

## Fair use

Do not use ops to run malware, phishing, spam, illegal content or attacks on other systems. Do not try to get into other accounts, break the control plane or get around limits of your plan. We may suspend an account that does, and close it when the abuse is serious or continues.

## Plans and payment

ops is a subscription per workspace, monthly or yearly; paying yearly costs less than twelve monthly payments. A subscription covers one workspace, with unlimited servers, apps and members in it. A new account gets a free trial of 30 days for its first workspace with everything, without a payment method. After the trial the workspace is read-only until you subscribe: your apps keep running on your servers, their backups continue, and you can still stop and start them, see everything and manage your account. Prices are shown before you buy.

Payments run through Stripe Managed Payments. Stripe is the seller of record: its checkout shows the price, tax and terms of the purchase, and Stripe handles invoices, receipts and refunds required by law. A subscription renews automatically until you cancel it. You can cancel at any time in the billing settings; your plan stays active until the end of the period you paid for. Payments already made are not refunded, except where the law requires it.

If a payment fails or is reversed, paid features stop until the subscription is paid again. Your apps keep running on your servers.

## Beta and changes

ops is in beta and moves fast. That means:

- **Prices, plans, limits and features can change at any time.** A new price applies to new subscriptions. While your subscription stays active, it keeps the price you started with.
- **The beta comes as it is.** We work hard to keep ops up, but there is no uptime guarantee and no promised response time.
- **Your data stays your responsibility.** ops backs up your databases and checks the backups every day, but you decide what runs and what is kept. Check that your backups are there and that you can restore them, and keep your own copy of anything you cannot afford to lose.

## When the control plane is down

Your apps run on your servers, not on ours. If the control plane is unavailable, your apps keep running; you just cannot deploy or change them through ops until it is back. We take reasonable care to keep it available but do not promise uninterrupted service.

## Ending

You can delete your account at any time in the settings. This removes your servers, apps and deploys from ops; the agent stops and your containers keep running until you remove them. We may end the service for everyone with at least 30 days notice by email, and refund the unused part of a prepaid period.

## Liability

We are liable without limit for intent and gross negligence and for injury to life, body or health. For slight negligence we are liable only for breach of an essential obligation of these terms and only for damage that was typical and foreseeable. Otherwise our liability is excluded to the extent the law permits. Mandatory consumer rights stay unaffected.

## Changes

We may change these terms. Material changes are announced by email at least 30 days before they apply. If you do not agree, you can cancel before they take effect.

## Contact

Questions go to [{{support_email}}](mailto:{{support_email}}). How we handle data is in the [Privacy Policy](/legal/privacy).
