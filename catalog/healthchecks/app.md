---
name: Healthchecks
pitch: Know when your cron jobs stop running.
category: Monitoring
website: https://healthchecks.io
source: github.com/healthchecks/healthchecks
license: BSD-3-Clause
image: healthchecks/healthchecks
database: postgres
port: 8000
logo: https://raw.githubusercontent.com/healthchecks/healthchecks/master/static/img/favicon.svg
---

Every job pings a URL when it runs. When a ping is late, Healthchecks tells you by email, Slack, Telegram and more. Checks and pings live in Postgres. Sign-in links go by email: set the SMTP settings as env vars.
