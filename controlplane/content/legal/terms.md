---
title: Terms of Service
description: The plain-language rules for using the hosted moin service
lastUpdated: November 4, 2026
---

These terms are the agreement between you and us, Berry Good Ideas LLC, 30 N Gould St Ste N, Sheridan, WY 82801, USA, the company that runs the hosted moin service. They cover the website, the control plane, its API and MCP server, and the `moin` command line tool and daemon when you use them with the hosted service. By creating an account you agree to them. How we handle data is in the [Privacy Policy](/legal/privacy).

## The service

moin deploys, runs and watches apps on servers you own. You install the moin daemon on your server; it dials out to the control plane and runs your deploys with Docker Compose.

moin is open source under the GNU Affero General Public License 3.0. That license covers the code. These terms cover only the hosted service we run. A control plane you host yourself is yours: these terms do not apply to it, and it shows no legal pages.

## Who can use moin

You must be at least 16 years old. If you sign up for a company or another organization, you confirm that you may accept these terms for it. One account belongs to one person; invite your team to a workspace instead of sharing a sign-in.

## Your account

Keep your sign-in, API tokens and deploy hook URLs secret: anyone who has them can act as you. You are responsible for what happens with your account and your tokens. Tell us at [{{support_email}}](mailto:{{support_email}}) if you think someone else has access.

## Your servers, apps and data

Your servers, apps, code and data stay yours. moin does not store your code or the data of your apps; they live on your servers and in the bucket you choose.

You decide what runs on your servers and you are responsible for it: its content, its security updates, the personal data of its users, and that it is legal where you run it. You also follow the terms of your server provider, your bucket provider and any other service you connect, such as GitHub, Cloudflare or Tailscale.

The daemon runs with root rights so it can manage Docker and the proxy. Install it only on servers you control.

## Acceptable use

Use moin only for legal things. Do not use it to:

- run malware, phishing, spam or scams
- host or spread illegal content, or content that infringes the rights of others
- attack, scan or overload other systems
- deploy cryptocurrency miners
- get into other accounts, get around the limits of your plan, or abuse free trials
- overload or break the control plane, or test its security without our written permission (to report a security issue, write to us)
- resell the hosted service without our agreement

## Suspension

If an account breaks these terms, puts others at risk or causes us legal trouble, we may limit or suspend it, with or without notice. When we can, we tell you first and give you time to fix it. When the abuse is serious or continues, we may close the account. Your apps keep running on your servers even while your account is suspended; only the control plane stops working for you.

## Abuse and copyright reports

If you see abuse or content that infringes your rights and it involves moin, write to [{{support_email}}](mailto:{{support_email}}). Tell us what it is, where it is (a domain or a link) and how to reach you. For copyright, also say which work is yours and confirm that you own it or may act for the owner. Apps on moin run on servers of our users, not on ours, so it can be faster to also report them to the provider of that server.

## Plans and payment

moin is a subscription per workspace, monthly or yearly. A subscription covers one workspace, with all its servers, apps and members, within the limits shown for your plan. A new account gets a free trial for its first workspace, without a payment method. After the trial the workspace is read-only until you subscribe: your apps keep running on your servers, their backups continue, and you can still stop and start them, see everything and manage your account. Prices are shown before you buy.

Payments run through Stripe Managed Payments. Stripe is the seller of record: its checkout shows the price, tax and terms of the purchase, and Stripe handles invoices, receipts and refunds required by law. A subscription renews automatically until you cancel it. You can cancel at any time in the billing settings; your plan stays active until the end of the period you paid for. Payments already made are not refunded, except where the law requires it or these terms say so.

If a payment fails or is reversed, paid features stop after a short grace period, until the subscription is paid again. Your apps keep running on your servers.

## Beta and changes to the service

moin is in beta and moves fast. That means:

- **Prices, plans, limits and features can change at any time.** We tell you about important changes by email.
- **Join early, keep your price.** A new price applies to new subscriptions. While your subscription stays active, it keeps the price you started with. If it ends and you subscribe again later, the price of that time applies.
- **Features come and go.** We may add, change or remove features, also features of your plan, when the product needs it.

## No guarantees

We work hard to keep moin up and correct, but we do not promise uptime, uninterrupted service, a response time for support, or that moin is free of errors.

**MOIN, ITS SOFTWARE, DAEMON, COMMAND LINE TOOL AND HOSTED SERVICE ARE PROVIDED "AS IS" AND "AS AVAILABLE", WITHOUT WARRANTY OF ANY KIND. AS FAR AS THE LAW ALLOWS, WE DISCLAIM ALL WARRANTIES, EXPRESS OR IMPLIED, INCLUDING MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, NON-INFRINGEMENT, AND THAT BACKUPS OR RESTORES WILL BE COMPLETE, CORRECT OR AVAILABLE.**

Your apps run on your servers, not on ours. If the control plane is down, your apps keep running; you just cannot deploy or change them through moin until it is back. moin helps you keep apps running, but it cannot guarantee that a deploy, a rollback or a health check succeeds, or that your server stays reachable.

## Backups and your data

moin backs up the databases of your apps to the bucket you choose and checks the backups every day. These backups are best effort. Check that they are there and that you can restore them, and keep your own copy of anything you cannot afford to lose.

You alone are responsible for your data. We are not responsible for data that is lost, damaged, leaked or not backed up, in any form and for any reason: on your servers, in your bucket or in moin, including through a deploy, rollback, update, migration, backup, restore, restore check, a command you or moin runs, or an outage of moin or of any service it uses.

## Liability

**AS FAR AS THE LAW ALLOWS, WE ARE NOT LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL OR PUNITIVE DAMAGES, OR FOR LOST PROFITS, REVENUE, DATA, GOODWILL OR DOWNTIME, HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, EVEN IF WE WERE TOLD THEY MIGHT HAPPEN.**

**OUR TOTAL LIABILITY FOR ALL CLAIMS ABOUT MOIN IS LIMITED TO WHAT YOU PAID FOR THE HOSTED SERVICE IN THE 12 MONTHS BEFORE THE EVENT THAT CAUSED THE CLAIM. IF YOU PAID NOTHING, INCLUDING WHEN YOU HOST MOIN YOURSELF, IT IS ZERO.**

For the use of moin, your contracting party is Berry Good Ideas LLC alone; for the purchase itself, Stripe is the seller of record (see Plans and payment). The owners, members, managers, employees and contributors of Berry Good Ideas LLC are not personally liable to you, privately or in business, for anything about moin.

## Indemnity

You defend and hold us harmless from claims by others, and the costs that come with them, that arise from your apps, your data, your servers, or your breach of these terms or of the law.

These limits do not apply where the law does not allow them, such as for intent, gross negligence, or injury to life, body or health. Rights you have as a consumer under mandatory law stay unaffected.

## Ending

You can delete your account at any time in the settings. Your servers, apps and deploys belong to a workspace, not to your account: deleting the workspace on its Team page removes them from moin. A workspace with a subscription can be deleted once the subscription has ended. If you are the only owner of a workspace, delete it or hand it to someone else before you delete your account.

Once a server is gone from moin, its daemon can no longer connect, and your containers keep running until you remove them. What moin set up in your own accounts, such as the GitHub App installation or Cloudflare tunnels and DNS records, stays there until you remove it.

If we ever end the hosted service, we tell you by email in good time and refund the unused part of any prepaid period.

## The law

These terms are governed by the laws of the State of Wyoming, USA, without its conflict of law rules. The state and federal courts in Wyoming are the only place for disputes about moin, and you agree to their jurisdiction. If you are a consumer, the mandatory protections of the country where you live still apply. If one part of these terms does not hold up, the rest still applies. Before going to court, write to us; most problems are solved by email.

## Changes to these terms

We may change these terms at any time. We tell you about important changes by email. If you keep using moin after a change, the new terms apply. If you do not agree, you can cancel and delete your account.

## Contact

Questions, abuse reports and requests go to [{{support_email}}](mailto:{{support_email}}).
