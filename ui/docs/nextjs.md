---
title: "Next.js"
description: "moin UI in a Next.js app: plain CSS classes in your components. The folder in public, linked once in the layout."
group: "Stacks"
order: 9
---

Next.js serves `public/` as it is. Link the files once in the root layout.

## 1. Get the folder

```bash
cd public
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```jsx
// app/layout.js
import Script from "next/script";

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <head>
        <link rel="stylesheet" href="/ui/moin.css" />
      </head>
      <body>
        {children}
        <Script src="/ui/moin.js" />
      </body>
    </html>
  );
}
```

## 3. Use it

A list of apps, each a row that opens its page:

```jsx
import Link from "next/link";

export function AppList({ apps }) {
  return (
    <section className="moin-card">
      <div className="moin-list">
        {apps.map((app) => (
          <Link key={app.slug} href={`/apps/${app.slug}`} className="moin-item">
            <div className="moin-item-text">
              <div className="moin-item-name">{app.name}</div>
              <div className="moin-item-note">{app.status}</div>
            </div>
            <i className="moin-chevron"></i>
          </Link>
        ))}
      </div>
    </section>
  );
}
```

Every other component works the same way: copy its HTML from its page and fill in your data.

Copying HTML from a page? In JSX `class` is `className` and `for` is `htmlFor`.
