---
name: Paperless-ngx
pitch: Scan it, find it. Every paper document, searchable.
category: Documents
website: "https://docs.paperless-ngx.com"
source: github.com/paperless-ngx/paperless-ngx
license: GPL-3.0
image: ghcr.io/paperless-ngx/paperless-ngx
database: postgres
port: 8000
login: "Sign in as admin with the password in ADMIN_PASSWORD: ops env --show."
---

Drop in scans and PDFs: it reads the text, tags them and makes every letter, invoice and contract findable in a second. Its index lives in your own Postgres. Give it 2 GB of memory.
