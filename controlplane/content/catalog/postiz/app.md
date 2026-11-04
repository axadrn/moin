---
name: Postiz
pitch: Schedule posts to every social network from one calendar.
category: Marketing
website: "https://postiz.com"
source: github.com/gitroomhq/postiz-app
license: AGPL-3.0
image: ghcr.io/gitroomhq/postiz-app
database: postgres
port: 5000
---

Plan and publish to X, LinkedIn, Instagram, Bluesky, Mastodon and more, with a calendar, teams and AI help. It needs its domain. Each network needs its own API keys: set them as env vars after install, such as X_API_KEY, then deploy again. Which keys each network needs: https://docs.postiz.com/providers. Its posts live in Postgres; Redis is only a cache.
