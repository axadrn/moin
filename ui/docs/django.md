---
title: "Django"
description: "moin UI in a Django app: plain HTML and CSS in your templates, served as static files. No package needed."
group: "Stacks"
order: 5
---

Put the folder in a static directory and link it with the `static` tag.

## 1. Get the folder

```bash
cd your_app/static
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```django
{% load static %}
<link rel="stylesheet" href="{% static 'ui/moin.css' %}">
<script src="{% static 'ui/moin.js' %}" defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```django
<section class="moin-card">
  <div class="moin-list">
    {% for app in apps %}
    <a href="{% url 'app' app.pk %}" class="moin-item">
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

With htmx, moin.js keeps working: it listens on the document, so swapped HTML works at once.
