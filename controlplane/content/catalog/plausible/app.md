---
name: Plausible
pitch: Simple, privacy-friendly web analytics. No cookies.
category: Analytics
website: "https://plausible.io"
source: github.com/plausible/analytics
license: AGPL-3.0
image: ghcr.io/plausible/community-edition
database: clickhouse
port: 8000
---

One light script, one clear dashboard: visitors, pages, sources and goals. Its accounts live in Postgres, which ops backs up; its events in ClickHouse, which ops does not back up yet. Give it 4 GB of memory. The first visit creates the account.
