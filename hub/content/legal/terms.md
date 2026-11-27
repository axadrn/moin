---
title: Terms of Service
description: The plain-language rules for the moin website and software
lastUpdated: November 27, 2026
---

These terms are the agreement between you and us, Berry Good Ideas LLC, 30 N Gould St Ste N, Sheridan, WY 82801, USA, the company that makes moin. They cover the website moin.run and the `moin` software you download from it. moin cloud, when it opens, gets its own terms here before you can sign up. How we handle data is in the [Privacy Policy](/legal/privacy).

## The service

moin deploys, runs and watches apps on servers you own. The `moin` program on your laptop logs in to your server over SSH, with your key, and runs your deploys there with Docker Compose. We run nothing between you and your server.

moin is open source. Its [license](/docs/faq#is-moin-open-source) covers the code.

## Who can use moin

You must be at least 16 years old. If you use moin for a company or another organization, you confirm that you may accept these terms for it.

## Your servers, apps and data

Your servers, apps, code and data stay yours. moin does not store your code or the data of your apps; they live on your servers and in the bucket you choose.

You decide what runs on your servers and you are responsible for it: its content, its security updates, the personal data of its users, and that it is legal where you run it. You also follow the terms of your server provider, your bucket provider and any other service you connect, such as GitHub, Cloudflare or Tailscale.

moin runs with root rights on your server, so it can manage Docker and the proxy. Use it only on servers you control.

## Acceptable use

Use moin only for legal things. Do not use it to:

- run malware, phishing, spam or scams
- host or spread illegal content, or content that infringes the rights of others
- attack, scan or overload other systems
- deploy cryptocurrency miners
- overload or break the website, or test its security without our written permission (to report a security issue, write to us)

## Abuse and copyright reports

If you see abuse or content that infringes your rights and it involves moin, write to [{{support_email}}](mailto:{{support_email}}). Tell us what it is, where it is (a domain or a link) and how to reach you. For copyright, also say which work is yours and confirm that you own it or may act for the owner. Apps on moin run on servers of our users, not on ours, so it can be faster to also report them to the provider of that server.

## Beta and changes to the service

moin is in beta and moves fast. That means:

- **Features come and go.** We may add, change or remove features when the product needs it. The release notes in the [changelog](/changelog) say what changed.

## No guarantees

We work hard to keep moin up and correct, but we do not promise uptime, uninterrupted service, a response time for support, or that moin is free of errors.

**MOIN, ITS SOFTWARE, DAEMON, COMMAND LINE TOOL, WATCHER AND WEBSITE ARE PROVIDED "AS IS" AND "AS AVAILABLE", WITHOUT WARRANTY OF ANY KIND. AS FAR AS THE LAW ALLOWS, WE DISCLAIM ALL WARRANTIES, EXPRESS OR IMPLIED, INCLUDING MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, NON-INFRINGEMENT, AND THAT BACKUPS OR RESTORES WILL BE COMPLETE, CORRECT OR AVAILABLE.**

Your apps run on your servers, not on ours. moin helps you keep apps running, but it cannot guarantee that a deploy, a rollback, a health check or an alert succeeds, or that your server stays reachable.

## Backups and your data

moin backs up the databases of your apps to the bucket you choose and checks the backups every day. These backups are best effort. Check that they are there and that you can restore them, and keep your own copy of anything you cannot afford to lose.

You alone are responsible for your data. We are not responsible for data that is lost, damaged, leaked or not backed up, in any form and for any reason: on your servers, in your bucket or anywhere else, including through a deploy, rollback, update, migration, backup, restore, check, a command you or moin runs, or an outage of moin or of any service it uses.

## Liability

**AS FAR AS THE LAW ALLOWS, WE ARE NOT LIABLE FOR ANY INDIRECT, INCIDENTAL, SPECIAL, CONSEQUENTIAL OR PUNITIVE DAMAGES, OR FOR LOST PROFITS, REVENUE, DATA, GOODWILL OR DOWNTIME, HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, EVEN IF WE WERE TOLD THEY MIGHT HAPPEN.**

**OUR TOTAL LIABILITY FOR ALL CLAIMS ABOUT MOIN IS LIMITED TO WHAT YOU PAID FOR THE HOSTED SERVICE IN THE 12 MONTHS BEFORE THE EVENT THAT CAUSED THE CLAIM. IF YOU PAID NOTHING, IT IS ZERO.**

For the use of moin, your contracting party is Berry Good Ideas LLC alone. The owners, members, managers, employees and contributors of Berry Good Ideas LLC are not personally liable to you, privately or in business, for anything about moin.

## Indemnity

You defend and hold us harmless from claims by others, and the costs that come with them, that arise from your apps, your data, your servers, or your breach of these terms or of the law.

These limits do not apply where the law does not allow them, such as for intent, gross negligence, or injury to life, body or health. Rights you have as a consumer under mandatory law stay unaffected.

## The law

These terms are governed by the laws of the State of Wyoming, USA, without its conflict of law rules. The state and federal courts in Wyoming are the only place for disputes about moin, and you agree to their jurisdiction. If you are a consumer, the mandatory protections of the country where you live still apply. If one part of these terms does not hold up, the rest still applies. Before going to court, write to us; most problems are solved by email.

## Changes to these terms

We may change these terms at any time. When they change, this page and its date change with it. If you keep using moin after a change, the new terms apply.

## Contact

Questions, abuse reports and requests go to [{{support_email}}](mailto:{{support_email}}).
