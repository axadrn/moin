---
title: "moin UI"
description: "The UI moin runs on: native HTML, CSS and JS for any stack, under the MIT license."
group: "Start"
order: 1
---

## Get started

### 1. Get the folder

```bash
curl -L https://github.com/axadrn/moin/archive/main.tar.gz | tar xz --strip-components=1 moin-main/ui
```

### 2. Link it

```html
<link rel="stylesheet" href="ui/moin.css">
<script src="ui/moin.js"></script>
```

### 3. Copy a block

Open the [blocks](/ui/blocks), copy a whole page, and change what you need. Use it as plain HTML, or turn it into components of your stack.

## Where everything lives

```text
ui/
├── moin.css       loads everything
├── moin.js        small behaviors
├── theme.css      colors, spacing, corners
├── layout.css     stack, row, grid, page
├── components/    one CSS file per component
├── icons.css      icon classes
├── icons/         SVG files
├── docs/          one page per component
├── examples/      one file per example
├── blocks/        whole pages to copy
├── CHANGELOG.md   what changed
├── LICENSE        MIT
└── README.md
```

## How it is built

- **[Blocks](/ui/blocks)**: whole pages to start from.
- **[Components](/ui/components)**: buttons, lists, cards.
- **[Layout](/ui/layout)**: where things stand.
- **[Theme](/ui/theme)**: colors, spacing, corners.

## The rules

- **A value exists once**, in `theme.css`.
- **A later layer wins**. CSS does that by itself, with layers.
- **A page is only HTML**: `moin-` classes, and no CSS of its own.
- **One dial, everything follows**. Turn the radius and every corner moves.
- **It is your code**. Copy the folder into your repo and change what you want.

## In your language

The same two buttons in each one. Every language prints this HTML.

### HTML

```html
<div class="moin-row" data-gap="2">
  <button type="button" class="moin-button" data-kind="primary">Deploy</button>
  <button type="button" class="moin-button">Cancel</button>
</div>
```

### templ

```templ
type Kind string

const Primary Kind = "primary"

type ButtonProps struct {
	Kind  Kind
	Attrs templ.Attributes
}

templ Button(p ButtonProps) {
	<button
		type="button"
		class="moin-button"
		if p.Kind != "" {
			data-kind={ string(p.Kind) }
		}
		{ p.Attrs... }
	>
		{ children... }
	</button>
}

templ Actions() {
	<div class="moin-row" data-gap="2">
		@Button(ButtonProps{Kind: Primary}) {
			Deploy
		}
		@Button(ButtonProps{}) {
			Cancel
		}
	</div>
}
```

### Go

```go
const page = \
```

### Ruby

```erb
require "erb"
include ERB::Util

def button(label, kind: nil)
  data = kind ? %( data-kind="#{h kind}") : ""
  %(<button type="button" class="moin-button"#{data}>#{h label}</button>)
end

puts ERB.new(<<~PAGE).result
  <div class="moin-row" data-gap="2">
    <%= button("Deploy", kind: "primary") %>
    <%= button("Cancel") %>
  </div>
PAGE
```

### Python

```python
{% macro button(label, kind="") -%}
<button type="button" class="moin-button"{% if kind %} data-kind="{{ kind }}"{% endif %}>{{ label }}</button>
{%- endmacro %}

<div class="moin-row" data-gap="2">
  {{ button("Deploy", kind="primary") }}
  {{ button("Cancel") }}
</div>
```

### PHP

```php
<?php
function button(string $label, string $kind = ''): string {
    $data = $kind ? ' data-kind="' . htmlspecialchars($kind) . '"' : '';
    return '<button type="button" class="moin-button"' . $data . '>' . htmlspecialchars($label) . '</button>';
}
?>
<div class="moin-row" data-gap="2">
  <?= button('Deploy', kind: 'primary') ?>
  <?= button('Cancel') ?>
</div>
```

### Rust

```rust
use maud::{html, Markup};

fn button(label: &str, kind: Option<&str>) -> Markup {
    html! { button.moin-button type="button" data-kind=[kind] { (label) } }
}

fn actions() -> Markup {
    html! {
        .moin-row data-gap="2" {
            (button("Deploy", Some("primary")))
            (button("Cancel", None))
        }
    }
}
```
