---
title: "Rails"
description: "moin UI in a Rails app: plain HTML and CSS in your ERB views, no gem and no build step. Works with Turbo."
group: "Stacks"
order: 3
---

Rails serves `public/` as it is, so the folder needs no gem and no asset pipeline.

## 1. Get the folder

```bash
cd public
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```erb
<%# app/views/layouts/application.html.erb, in the <head> %>
<link rel="stylesheet" href="/ui/moin.css">
<script src="/ui/moin.js" defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```erb
<section class="moin-card">
  <div class="moin-list">
    <% @apps.each do |app| %>
    <%= link_to app_path(app), class: "moin-item" do %>
      <div class="moin-item-text">
        <div class="moin-item-name"><%= app.name %></div>
        <div class="moin-item-note"><%= app.status %></div>
      </div>
      <i class="moin-chevron"></i>
    <% end %>
    <% end %>
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.

Turbo swaps pages without a reload. moin.js listens on the document, so what Turbo brings in works at once.
