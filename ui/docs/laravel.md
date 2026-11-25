---
title: "Laravel"
description: "moin UI in a Laravel app: plain HTML and CSS in your Blade views. No package, no Vite step needed."
group: "Stacks"
order: 4
---

Laravel serves `public/` as it is. Link the files with `asset()` and use the classes in Blade.

## 1. Get the folder

```bash
cd public
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

## 2. Link it

```blade
{{-- resources/views/layouts/app.blade.php, in the <head> --}}
<link rel="stylesheet" href="{{ asset('ui/moin.css') }}">
<script src="{{ asset('ui/moin.js') }}" defer></script>
```

## 3. Use it

A list of apps, each a row that opens its page:

```blade
<section class="moin-card">
  <div class="moin-list">
    @foreach ($apps as $app)
    <a href="{{ route('apps.show', $app) }}" class="moin-item">
      <div class="moin-item-text">
        <div class="moin-item-name">{{ $app->name }}</div>
        <div class="moin-item-note">{{ $app->status }}</div>
      </div>
      <i class="moin-chevron"></i>
    </a>
    @endforeach
  </div>
</section>
```

Every other component works the same way: copy its HTML from its page and fill in your data.

With Livewire, moin.js keeps working: it listens on the document, so new HTML works at once.
