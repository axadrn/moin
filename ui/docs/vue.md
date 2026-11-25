---
title: "Vue"
description: "moin UI in a Vue app: plain HTML and CSS classes in your templates. The folder in public, linked once."
group: "Stacks"
order: 10
---

With Vite, everything in `public/` is served as it is. Link the files once in `index.html`. Vue templates keep `class`, so HTML copies over as it is.

## 1. Get the folder

```bash
cd public
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```html
<!-- index.html, in the <head> -->
<link rel="stylesheet" href="/ui/moin.css">
<script src="/ui/moin.js" defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```vue
<template>
  <section class="moin-card">
    <div class="moin-list">
      <a v-for="app in apps" :key="app.slug" :href="`/apps/${app.slug}`" class="moin-item">
        <div class="moin-item-text">
          <div class="moin-item-name">{{ app.name }}</div>
          <div class="moin-item-note">{{ app.status }}</div>
        </div>
        <i class="moin-chevron"></i>
      </a>
    </div>
  </section>
</template>
```

Every other component works the same way: copy its HTML from its page and fill in your data.
