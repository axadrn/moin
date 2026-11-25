---
title: "Flask"
description: "moin UI in a Flask app: plain HTML and CSS in your Jinja templates, served from static. No extension needed."
group: "Stacks"
order: 6
---

Flask serves `static/` by itself. Link the files with `url_for` and use the classes in Jinja.

## 1. Get the folder

```bash
cd static
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```jinja
<link rel="stylesheet" href="{{ url_for('static', filename='ui/moin.css') }}">
<script src="{{ url_for('static', filename='ui/moin.js') }}" defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```jinja
<section class="moin-card">
  <div class="moin-list">
    {% for app in apps %}
    <a href="{{ url_for('app', slug=app.slug) }}" class="moin-item">
      <div class="moin-item-text">
        <div class="moin-item-name">{{ app.name }}</div>
        <div class="moin-item-note">{{ app.status }}</div>
      </div>
      <i class="moin-chevron"></i>
    </a>
    {% endfor %}
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.
