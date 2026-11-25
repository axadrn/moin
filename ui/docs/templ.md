---
title: "templ"
description: "moin UI with templ in Go: plain HTML and CSS classes in your components. moin's own web app is built this way."
group: "Stacks"
order: 2
---

moin's own web app is templ and moin UI. Serve the folder with `net/http`, then use the classes in your components.

## 1. Get the folder

```bash
cd your-app
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```go
mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir("ui"))))
```

## 3. Use it

A list of apps, each a row that opens its page:

```templ
// In the <head> of your layout:
// <link rel="stylesheet" href="/ui/moin.css"/>
// <script src="/ui/moin.js" defer></script>

templ AppList(apps []App) {
  <section class="moin-card">
    <div class="moin-list">
      for _, app := range apps {
        <a href={ templ.SafeURL("/apps/" + app.Slug) } class="moin-item">
          <div class="moin-item-text">
            <div class="moin-item-name">{ app.Name }</div>
            <div class="moin-item-note">{ app.Status }</div>
          </div>
          <i class="moin-chevron"></i>
        </a>
      }
    </div>
  </section>
}
```

Every other component works the same way: copy its HTML from its page and fill in your data.
