---
title: "Two ways"
description: "One form, two sets of fields. A radio picks which shows, with no script."
group: "Forms"
order: 10
---

One form, two sets of fields. A radio picks which shows, with no script.

Styles: `layout.css`, `components/choice.css`.

```example
two-ways
```

`data-either` goes on the form. The radio that means the second way carries `data-other`. Parts with `data-way="first"` show until it is checked. Parts with `data-way="other"` show once it is. Everything else stays. Both ways are in the page, so the server gets the one that was picked. Works on any element with radios in it.
