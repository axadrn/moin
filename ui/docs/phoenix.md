---
title: "Phoenix"
description: "moin UI in a Phoenix app: plain HTML and CSS classes in HEEx. Works with LiveView, no npm package."
group: "Stacks"
order: 7
---

Put the folder in `priv/static` and add `ui` to `static_paths` in `lib/my_app_web.ex`, so Phoenix serves it.

## 1. Get the folder

```bash
cd priv/static
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```heex
<%!-- root.html.heex, in the <head> --%>
<link rel="stylesheet" href={~p"/ui/moin.css"} />
<script src={~p"/ui/moin.js"} defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```heex
<section class="moin-card">
  <div class="moin-list">
    <.link :for={app <- @apps} navigate={~p"/apps/#{app}"} class="moin-item">
      <div class="moin-item-text">
        <div class="moin-item-name">{app.name}</div>
        <div class="moin-item-note">{app.status}</div>
      </div>
      <i class="moin-chevron"></i>
    </.link>
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.

LiveView patches the page. moin.js listens on the document, so what it patches in works at once.
