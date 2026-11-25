---
title: "Astro"
description: "moin UI in an Astro site: plain HTML and CSS in your components, no integration. The folder in public."
group: "Stacks"
order: 12
---

Astro serves `public/` as it is. Link the files in your layout; `is:inline` keeps Astro from bundling the script.

## 1. Get the folder

```bash
cd public
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```astro
---
// src/layouts/Layout.astro
---
<html lang="en">
  <head>
    <link rel="stylesheet" href="/ui/moin.css" />
    <script is:inline src="/ui/moin.js" defer></script>
  </head>
  <body><slot /></body>
</html>
```

## 3. Use it

A list of apps, each a row that opens its page:

```astro
<section class="moin-card">
  <div class="moin-list">
    {apps.map((app) => (
      <a href={`/apps/${app.slug}`} class="moin-item">
        <div class="moin-item-text">
          <div class="moin-item-name">{app.name}</div>
          <div class="moin-item-note">{app.status}</div>
        </div>
        <i class="moin-chevron"></i>
      </a>
    ))}
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.
