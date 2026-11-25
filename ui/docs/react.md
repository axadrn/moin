---
title: "React"
description: "moin UI in a React app: plain CSS classes in JSX. Put the folder in public, link it once, write className."
group: "Stacks"
order: 8
---

With Vite, everything in `public/` is served as it is. Link the files once in `index.html`.

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

```jsx
export function AppList({ apps }) {
  return (
    <section className="moin-card">
      <div className="moin-list">
        {apps.map((app) => (
          <a key={app.slug} href={`/apps/${app.slug}`} className="moin-item">
            <div className="moin-item-text">
              <div className="moin-item-name">{app.name}</div>
              <div className="moin-item-note">{app.status}</div>
            </div>
            <i className="moin-chevron"></i>
          </a>
        ))}
      </div>
    </section>
  );
}
```

Every other component works the same way: copy its HTML from its page and fill in your data.

Copying HTML from a page? In JSX `class` is `className`, `for` is `htmlFor` and `tabindex` is `tabIndex`. moin.js listens on the document, so it works with what React renders.
