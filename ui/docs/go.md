---
title: "Go"
description: "moin UI in a Go app with html/template: plain HTML and CSS, served by net/http. No build step."
group: "Stacks"
order: 1
---

Go's standard library is enough: `net/http` serves the folder, `html/template` fills in your data.

## 1. Get the folder

```bash
cd your-app
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```go
// Serve the folder at /ui/.
mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir("ui"))))
```

## 3. Use it

A list of apps, each a row that opens its page:

```html
<!-- In the <head> of your layout template -->
<link rel="stylesheet" href="/ui/moin.css">
<script src="/ui/moin.js" defer></script>

<section class="moin-card">
  <div class="moin-list">
    {{range .Apps}}
    <a href="/apps/{{.Slug}}" class="moin-item">
      <div class="moin-item-text">
        <div class="moin-item-name">{{.Name}}</div>
        <div class="moin-item-note">{{.Status}}</div>
      </div>
      <i class="moin-chevron"></i>
    </a>
    {{end}}
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.

To ship one binary, put the folder in an `embed.FS` and serve it with `http.FileServerFS`.
