---
title: Terms of Service
description: The plain-language rules for moin and moin cloud
lastUpdated: November 27, 2026
---

These terms are the agreement between you and us, Berry Good Ideas LLC, 30 N Gould St Ste N, Sheridan, WY 82801, USA, the company that makes moin. They cover the website moin.run, the `moin` software you download from it, and moin cloud, the paid service we run. By using moin, or by signing in to moin cloud, you agree to them. How we handle data is in the [Privacy Policy](/legal/privacy).

## The service

moin deploys, runs and watches apps on servers you own. The `moin` program on your laptop logs in to your server over SSH, with your key, and runs your deploys there with Docker Compose. We run nothing between you and your server.

moin is open source. Its [license](/docs/faq#is-moin-open-source) covers the code: the CLI, the daemon and the watcher. You can use them for free, without an account and without moin cloud.

moin cloud is optional and paid. It runs a watcher for you, which alerts you when a connected server stops reporting or a domain is down, and it keeps a second, encrypted copy of your backups that your server can add to but never delete. moin cloud receives reports and copies. It cannot log in to your server or command it.

## Who can use moin

You must be at least 16 years old. If you use moin for a company or another organization, you confirm that you may accept these terms for it.

## Your moin cloud account

Your account is your mail address. Keep your mail, your sign-in links, your CLI sessions and the tokens of your servers secret: anyone who has them can act as you. You are responsible for what happens with your account and your tokens. Tell us at [{{support_email}}](mailto:{{support_email}}) if you think someone else has access.

## Your servers, apps and data

Your servers, apps, code and data stay yours. moin does not store your code or the data of your apps; they live on your servers and in the bucket you choose. The copies at moin cloud are encrypted on your server, with a key we never get.

You decide what runs on your servers and you are responsible for it: its content, its security updates, the personal data of its users, and that it is legal where you run it. You also follow the terms of your server provider, your bucket provider and any other service you connect, such as GitHub, Cloudflare or Tailscale.

moin runs with root rights on your server, so it can manage Docker and the proxy. Use it only on servers you control.

Keep your backup key safe. Without it, nobody can restore your copies, not even we.

## Acceptable use

Use moin only for legal things. Do not use it to:

- run malware, phishing, spam or scams
- host or spread illegal content, or content that infringes the rights of others
- attack, scan or overload other systems
- deploy cryptocurrency miners
- get into other accounts, get around the limits of your plan, or abuse free trials
- store anything other than backups of your apps in moin cloud
- overload or break the website or moin cloud, or test their security without our written permission (to report a security issue, write to us)
- resell moin cloud without our agreement

## Suspension

If a moin cloud account breaks these terms, puts others at risk or causes us legal trouble, we may limit or suspend it, with or without notice. When we can, we tell you first and give you time to fix it. When the abuse is serious or continues, we may close the account. Your apps keep running on your servers even while your account is suspended; only moin cloud stops working for you.

## Abuse and copyright reports

If you see abuse or content that infringes your rights and it involves moin, write to [{{support_email}}](mailto:{{support_email}}). Tell us what it is, where it is (a domain or a link) and how to reach you. For copyright, also say which work is yours and confirm that you own it or may act for the owner. Apps on moin run on servers of our users, not on ours, so it can be faster to also report them to the provider of that server.

## moin cloud: plans and payment

moin cloud is for businesses only: you subscribe as a business, not as a consumer, and confirm it before you subscribe. moin cloud is a subscription per workspace, monthly or yearly, at the price shown at checkout.

A new workspace gets a free trial of the watcher, without a payment method. The trial starts with the first report of a connected server and lasts 30 days. The copies of your backups come with a subscription. After the trial, you need a subscription to keep the watcher.

Payments run through Stripe Managed Payments. Stripe is the seller of record: its checkout shows the price, tax and terms of the purchase, and Stripe handles invoices, receipts and refunds required by law. A subscription renews automatically until you cancel it. You can cancel at any time in the billing portal; your plan stays active until the end of the period you paid for. Payments already made are not refunded, except where the law requires it or these terms say so.

If a payment fails or is reversed, moin cloud stops after a short grace period, until the subscription is paid again. Your apps keep running on your servers.

## moin cloud: storage

A subscription includes storage for the copies of your backups, within fair use: the normal backups of the apps on your servers. Each copy is kept 14 days. If a workspace stores far more than its apps need, we tell you, and moin cloud may refuse new copies until it is back within fair use. It never deletes old copies for that.

## When a subscription ends

When a subscription ends, by cancelling or because a payment failed for good, we tell you by mail. moin cloud takes no new copies. The copies you have stay 30 days, so you can restore them, and are then deleted. Subscribe again within those 30 days and they stay.

## Beta and changes to the service

moin is in beta and moves fast. That means:

- **Prices, plans, limits and features can change at any time.** We tell moin cloud customers about important changes by mail.
- **Join early, keep your price.** A new price applies to new subscriptions. While your subscription stays active, it keeps the price you started with. If it ends and you subscribe again later, the price of that time applies.
- **Features come and go.** We may add, change or remove features, also features of your plan, when the product needs it. The release notes in the [changelog](/changelog) say what changed.

## No guarantees

We work hard to keep moin up and correct, but we do not promise uptime, uninterrupted service, a response time for support, or that moin is free of errors.

**MOIN, ITS SOFTWARE, DAEMON, COMMAND LINE TOOL, WATCHER, WEBSITE AND MOIN CLOUD ARE PROVIDED "AS IS" AND "AS AVAILABLE", WITHOUT WARRANTY OF ANY KIND. AS FAR AS THE LAW ALLOWS, WE DISCLAIM ALL WARRANTIES, EXPRESS OR IMPLIED, INCLUDING MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, NON-INFRINGEMENT, AND THAT BACKUPS, COPIES, RESTORES OR ALERTS WILL BE COMPLETE, CORRECT, TIMELY OR AVAILABLE.**

The software is also provided under its open source license, which says the same.

Your apps run on your servers, not on ours. If moin cloud is down, your apps keep running and your backups keep going to your bucket. moin helps you keep apps running, but it cannot guarantee that a deploy, a rollback, a health check or an alert succeeds, that the watcher notices every outage, or that your server stays reachable.

## Backups and your data

moin backs up the databases of your apps to the bucket you choose and checks the backups every day. moin cloud keeps a second copy. These backups and copies are best effort. Check that they are there and that you can restore them, and keep your own copy of anything you cannot afford to lose. moin cloud is a second copy, never your only one.

You alone are responsible for your data and for your backup key. We are not responsible for data that is lost, damaged, leaked or not backed up, in any form and for any reason: on your servers, in your bucket, in moin cloud or anywhere else, including through a deploy, rollback, update, migration, backup, copy, restore, check, a lost backup key, a command you or moin runs, or an outage of moin, moin cloud or of any service they use.

## Liability

**AS FAR AS THE LAW ALLOWS, WE ARE NOT LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL OR PUNITIVE DAMAGES, OR FOR LOST PROFITS, REVENUE, DATA, GOODWILL OR DOWNTIME, HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, EVEN IF WE WERE TOLD THEY MIGHT HAPPEN.**

**OUR TOTAL LIABILITY FOR ALL CLAIMS ABOUT MOIN AND MOIN CLOUD IS LIMITED TO WHAT YOU PAID FOR MOIN CLOUD IN THE 12 MONTHS BEFORE THE EVENT THAT CAUSED THE CLAIM. IF YOU PAID NOTHING, INCLUDING WHEN YOU USE ONLY THE FREE SOFTWARE, IT IS ZERO.**

For the use of moin, your contracting party is Berry Good Ideas LLC alone; for the purchase of moin cloud, Stripe is the seller of record (see Plans and payment). The owners, members, managers, employees and contributors of Berry Good Ideas LLC are not personally liable to you, privately or in business, for anything about moin.

These limits, and those in No guarantees and Backups and your data, do not apply where the law does not allow them, such as for intent, gross negligence, or injury to life, body or health. Rights you have as a consumer under mandatory law stay unaffected.

## Indemnity

You defend and hold us harmless from claims by others, and the costs that come with them, that arise from your apps, your data, your servers, or your breach of these terms or of the law.

## Ending

You can stop using moin at any time. Your apps keep running on your servers; moin on them stays until you remove it.

To leave moin cloud, cancel your subscription in the billing portal and disconnect your servers with `moin cloud disconnect <server>`. A disconnected server no longer reports and sends no copies. To delete your account, write to us. Copies younger than 14 days stay locked until they are 14 days old, then they are deleted.

If we ever end moin cloud, we tell you by mail in good time, keep your copies long enough for you to restore them, and refund the unused part of any prepaid period.

## The law

These terms are governed by the laws of the State of Wyoming, USA, without its conflict of law rules. The state and federal courts in Wyoming are the only place for disputes about moin, and you agree to their jurisdiction. If you are a consumer, the mandatory protections of the country where you live still apply. If one part of these terms does not hold up, the rest still applies. Before going to court, write to us; most problems are solved by email.

## Changes to these terms

We may change these terms at any time. When they change, this page and its date change with it, and we tell moin cloud customers about important changes by mail. If you keep using moin after a change, the new terms apply. If you do not agree, you can cancel and leave moin cloud.

## Contact

Questions, abuse reports and requests go to [{{support_email}}](mailto:{{support_email}}).
