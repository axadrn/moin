---
title: "Svelte"
description: "moin UI in a SvelteKit app: plain HTML and CSS classes in your components. The folder in static, linked once."
group: "Stacks"
order: 11
---

SvelteKit serves `static/` as it is. Link the files once in `src/app.html`. Svelte keeps `class`, so HTML copies over as it is.

## 1. Get the folder

```bash
cd static
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```html
<!-- src/app.html, in the <head> -->
<link rel="stylesheet" href="%sveltekit.assets%/ui/moin.css">
<script src="%sveltekit.assets%/ui/moin.js" defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```svelte
<section class="moin-card">
  <div class="moin-list">
    {#each apps as app (app.slug)}
    <a href="/apps/{app.slug}" class="moin-item">
      <div class="moin-item-text">
        <div class="moin-item-name">{app.name}</div>
        <div class="moin-item-note">{app.status}</div>
      </div>
      <i class="moin-chevron"></i>
    </a>
    {/each}
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.
