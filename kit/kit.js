// kit.js: the little plain HTML cannot do by itself. Every part listens on
// document, so HTML swapped in later works at once. One part per behaviour.

(() => {
  const root = document.documentElement;
  // Storage can be missing or refused, so choices also live in memory and
  // keep working for this visit.
  const memory = new Map();
  const read = (key) => {
    if (memory.has(key)) return memory.get(key);
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  };
  const write = (key, value) => {
    memory.set(key, value);
    try {
      if (value === null) localStorage.removeItem(key);
      else localStorage.setItem(key, value);
    } catch {}
  };

  // Mode: system, light or dark, kept in this browser. No class means system.
  function applyMode() {
    const mode = read("kit-mode") || "system";
    root.classList.remove("light", "dark");
    if (mode !== "system") root.classList.add(mode);
    // Inline, so no other stylesheet on the page can pin the scheme.
    root.style.colorScheme = mode === "system" ? root.dataset.theme || "light dark" : mode;
    markMode(mode);
  }

  function markMode(mode) {
    document.querySelectorAll("[data-mode-set]").forEach((input) => {
      if (input.matches("input")) input.checked = input.value === mode;
      else input.setAttribute("aria-pressed", String(input.dataset.modeSet === mode));
    });
  }

  function setMode(mode) {
    write("kit-mode", mode === "system" ? null : mode);
    applyMode();
  }

  const isDark = () =>
    root.classList.contains("dark") ||
    (!root.classList.contains("light") && matchMedia("(prefers-color-scheme: dark)").matches);

  window.kit = { applyMode, setMode, read, write };
  // Nothing chosen here means system, which needs no class: the page keeps
  // what it has, so a site with its own switch can use the kit.
  if (read("kit-mode")) applyMode();
  else markMode("system");

  document.addEventListener("change", (event) => {
    if (event.target.matches?.("input[data-mode-set]")) setMode(event.target.value);
  });
  document.addEventListener("click", (event) => {
    if (event.target.closest("[data-mode-toggle]")) setMode(isDark() ? "light" : "dark");
    const set = event.target.closest("button[data-mode-set]");
    if (set) setMode(set.dataset.modeSet);
  });

  // Dialogs: data-open-dialog opens one, data-close-dialog closes the one it
  // is in, and a click beside it closes it, as Escape does.
  document.addEventListener("click", (event) => {
    const opener = event.target.closest("[data-open-dialog]");
    if (opener) document.getElementById(opener.dataset.openDialog)?.showModal();
    if (event.target.closest("[data-close-dialog]")) event.target.closest("dialog")?.close();
    const dialog = event.target;
    if (dialog instanceof HTMLDialogElement && dialog.matches(".kit-dialog")) {
      const box = dialog.getBoundingClientRect();
      const inside = event.clientX >= box.left && event.clientX <= box.right && event.clientY >= box.top && event.clientY <= box.bottom;
      if (!inside) dialog.close();
    }
  });

  // Asking first: data-confirm="Stop my-app? It goes offline." asks in the one
  // dialog, up to the first "? " the title. With data-confirm-name the button
  // works only once the name is typed. On yes the element gets kit:confirmed.
  function confirmDialog() {
    let dialog = document.getElementById("kit-confirm");
    if (dialog) return dialog;
    dialog = document.createElement("dialog");
    dialog.id = "kit-confirm";
    dialog.className = "kit-dialog";
    dialog.innerHTML = `<div class="kit-stack" data-gap="4" style="--gap:1.25rem">
      <div class="kit-stack" data-gap="1"><h2 class="kit-heading" data-title></h2><p class="kit-text kit-muted" data-note></p></div>
      <div class="kit-field" data-typed hidden><label for="kit-confirm-name"></label><input id="kit-confirm-name" class="kit-input" autocomplete="off" spellcheck="false"></div>
      <div class="kit-dialog-buttons"><button type="button" class="kit-button" data-close-dialog>Cancel</button><button type="button" class="kit-button" data-kind="danger" data-accept></button></div>
    </div>`;
    document.body.append(dialog);
    return dialog;
  }

  let asking = null;
  document.addEventListener("click", (event) => {
    const source = event.target.closest("[data-confirm]");
    if (source) {
      event.preventDefault();
      const dialog = confirmDialog();
      const text = source.dataset.confirm;
      const split = text.indexOf("? ");
      dialog.querySelector("[data-title]").textContent = split < 0 ? text : text.slice(0, split + 1);
      dialog.querySelector("[data-note]").textContent = split < 0 ? "" : text.slice(split + 2);
      const accept = dialog.querySelector("[data-accept]");
      accept.textContent = source.textContent.trim().slice(0, 24) || "Continue";
      const name = source.dataset.confirmName || "";
      const typed = dialog.querySelector("[data-typed]");
      typed.hidden = !name;
      typed.querySelector("label").textContent = `Type ${name} to confirm`;
      const input = typed.querySelector("input");
      input.value = "";
      accept.disabled = Boolean(name);
      asking = { source, name };
      dialog.showModal();
      (name ? input : accept).focus();
      return;
    }
    if (event.target.closest("#kit-confirm [data-accept]") && asking) {
      asking.source.dispatchEvent(new CustomEvent("kit:confirmed", { bubbles: true }));
      asking = null;
      document.getElementById("kit-confirm").close();
    }
  });
  document.addEventListener("input", (event) => {
    if (asking && event.target.id === "kit-confirm-name") {
      document.querySelector("#kit-confirm [data-accept]").disabled = event.target.value.trim() !== asking.name;
    }
  });

  // Select: radios show what you picked in the button, icon and all, and close
  // the list. With the keyboard it stays open while the arrows walk, and Enter
  // closes it. Checkboxes keep it open and the button says how many.
  document.addEventListener("change", (event) => {
    const select = event.target.closest?.(".kit-select");
    if (!select || !event.target.matches("[type=radio], [type=checkbox]")) return;
    const shown = select.querySelector("[data-select-label]");
    if (event.target.type === "checkbox") {
      const picked = [...select.querySelectorAll("input:checked")];
      shown.textContent = picked.length === 0 ? "None" : picked.length === 1 ? picked[0].closest("label").textContent.trim() : `${picked.length} selected`;
      return;
    }
    const option = event.target.closest("label");
    shown.replaceChildren(...[...option.childNodes].filter((node) => node !== event.target && (node.nodeType === Node.ELEMENT_NODE || node.textContent.trim())).map((node) => node.cloneNode(true)));
    if (!event.target.matches(":focus-visible")) select.querySelector("[popover]").hidePopover();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Enter" || !event.target.closest?.(".kit-select [popover]")) return;
    event.preventDefault();
    event.target.closest("[popover]").hidePopover();
  });
  document.addEventListener("mouseover", (event) => {
    event.target.closest?.(".kit-select [popover] label")?.querySelector("input")?.focus({ preventScroll: true });
  });
  document.addEventListener("toggle", (event) => {
    if (event.newState === "open" && event.target.closest?.(".kit-select")) {
      event.target.querySelector("input:checked")?.focus({ preventScroll: true });
    }
  }, true);

  // Copy: data-copy copies its value, or the element data-copy-target names,
  // and the icon turns into a check for a moment.
  document.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-copy]");
    if (!button) return;
    event.preventDefault();
    const target = button.dataset.copyTarget && document.querySelector(button.dataset.copyTarget);
    const text = target ? ("value" in target ? target.value : target.textContent).trim() : button.dataset.copy;
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      target?.select?.();
      return;
    }
    button.dataset.copied = "";
    clearTimeout(button.copyTimer);
    button.copyTimer = setTimeout(() => delete button.dataset.copied, 1400);
  });

  // A tip: any element with data-tip says it next to the pointer or finger.
  let tip = null;
  function showTip(event, text) {
    if (!tip) {
      tip = Object.assign(document.createElement("div"), { className: "kit-tip" });
      tip.setAttribute("role", "status");
      document.body.append(tip);
    }
    tip.textContent = text;
    tip.hidden = false;
    const { width, height } = tip.getBoundingClientRect();
    const left = Math.min(Math.max(event.clientX - width / 2, 8), innerWidth - width - 8);
    const above = event.clientY - height - 12;
    tip.style.left = `${left}px`;
    tip.style.top = `${above < 8 ? event.clientY + 20 : above}px`;
  }
  const hideTip = () => {
    if (tip) tip.hidden = true;
  };
  for (const type of ["pointermove", "pointerdown"]) {
    document.addEventListener(type, (event) => {
      const source = event.target.closest?.("[data-tip]");
      if (source?.dataset.tip) showTip(event, source.dataset.tip);
      else hideTip();
    });
  }
  document.documentElement.addEventListener("pointerleave", hideTip);
  addEventListener("scroll", hideTip, { passive: true });

  // The palette: ⌘K, ctrl+k, / or data-open-palette. Filters its own options.
  const palette = () => document.getElementById("kit-palette");
  const field = () => palette()?.querySelector("input");
  const options = () => [...palette().querySelectorAll(".kit-option:not([hidden])")];
  const typing = (target) => target instanceof Element && target.closest("input, textarea, select, [contenteditable]");

  function light(option) {
    palette().querySelectorAll(".kit-option").forEach((one) => one.setAttribute("aria-selected", String(one === option)));
    option?.scrollIntoView({ block: "nearest" });
  }

  function filter() {
    const words = field().value.toLowerCase().trim();
    palette().querySelectorAll(".kit-option").forEach((one) => {
      one.hidden = words !== "" && !one.textContent.toLowerCase().includes(words);
    });
    light(options()[0]);
  }

  function openPalette() {
    if (!palette() || palette().open) return;
    field().value = "";
    filter();
    palette().showModal();
    field().focus();
  }

  document.addEventListener("click", (event) => {
    if (event.target.closest("[data-open-palette]")) openPalette();
    if (!palette()) return;
    if (event.target === palette() || event.target.closest("#kit-palette .kit-option")) palette().close();
  });
  document.addEventListener("input", (event) => {
    if (palette() && event.target === field()) filter();
  });
  document.addEventListener("mouseover", (event) => {
    const option = event.target.closest?.("#kit-palette .kit-option");
    if (option && option.getAttribute("aria-selected") !== "true") light(option);
  });
  document.addEventListener("keydown", (event) => {
    if (!palette()) return;
    const inside = event.target === field();
    if (!inside && ((event.key === "k" && (event.metaKey || event.ctrlKey)) || (event.key === "/" && !typing(event.target)))) {
      event.preventDefault();
      openPalette();
      return;
    }
    if (!inside) return;
    const list = options();
    const at = list.findIndex((one) => one.getAttribute("aria-selected") === "true");
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      light(list[(at + step + list.length) % list.length]);
    } else if (event.key === "Enter") {
      event.preventDefault();
      (list[at] || list[0])?.click();
    }
  });

  // A chart: one or two lines, the newest value big, the pointer reads any
  // point. values="1,2,3" and second="1,2,3"; the page draws nothing itself.
  class KitChart extends HTMLElement {
    static observedAttributes = ["values", "second"];

    connectedCallback() {
      this.render();
    }

    attributeChangedCallback() {
      if (this.isConnected) this.render();
    }

    render() {
      if (!this.hasAttribute("values")) return;
      const numbers = (name) => (this.getAttribute(name) || "").split(",").filter(Boolean).map(Number);
      const lines = [{ label: this.getAttribute("label") || "", values: numbers("values"), ink: "var(--chart-1)" }];
      if (this.hasAttribute("second")) lines.push({ label: this.getAttribute("second-label") || "", values: numbers("second"), ink: "var(--chart-3)" });
      const unit = this.getAttribute("unit") || "";
      const top = Number(this.getAttribute("top")) || Math.ceil(Math.max(...lines.flatMap((line) => line.values)) * 1.15) || 1;
      const count = lines[0].values.length;
      const x = (i) => (count < 2 ? 0 : (i / (count - 1)) * 600);
      const y = (v) => 144 - (v / top) * 144;
      const text = (v) => `${Math.round(v * 10) / 10}${unit}`;
      const start = Number(this.getAttribute("start-hour")) || 0;
      const clock = (i) => {
        const minutes = start * 60 + Math.round((i / Math.max(count - 1, 1)) * 24 * 60);
        return `${String(Math.floor(minutes / 60) % 24).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
      };
      const path = (values) => values.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)} ${y(v).toFixed(1)}`).join("");
      const two = lines.length > 1;
      this.className = "kit-chart";
      this.innerHTML = `<div class="kit-chart-head">
          <div class="kit-chart-values">${lines.map((line) => `<span class="kit-stack" style="--gap:0;--ink:${line.ink}">${two ? `<span class="kit-chart-key">${line.label}</span>` : ""}<span class="kit-number" data-value></span></span>`).join("")}</div>
          <span class="kit-small kit-digits kit-muted" data-time></span>
        </div>
        <div class="kit-chart-grid">
          <div class="kit-chart-plot">
            <svg viewBox="0 0 600 144" preserveAspectRatio="none" aria-hidden="true">
              ${lines.map((line, n) => `<g style="color:${line.ink}">${n === 0 ? `<path d="${path(line.values)}L600 144L0 144Z" fill="currentColor" fill-opacity="0.08"/>` : ""}<path d="${path(line.values)}" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke"/></g>`).join("")}
            </svg>
            <div class="kit-chart-cursor" hidden></div>
          </div>
          <div class="kit-chart-scale"><span>${text(top)}</span><span>${text(top / 2)}</span><span>0${unit}</span></div>
          <div class="kit-chart-times"><span>${clock(0)}</span><span>${clock((count - 1) / 2)}</span><span>${clock(count - 1)}</span></div>
        </div>`;
      const plot = this.querySelector(".kit-chart-plot");
      const cursor = this.querySelector(".kit-chart-cursor");
      const shown = this.querySelectorAll("[data-value]");
      const time = this.querySelector("[data-time]");
      const show = (i) => {
        lines.forEach((line, n) => (shown[n].textContent = text(line.values[i])));
        time.textContent = clock(i);
      };
      show(count - 1);
      plot.addEventListener("pointermove", (event) => {
        const box = plot.getBoundingClientRect();
        const i = Math.max(0, Math.min(count - 1, Math.round(((event.clientX - box.left) / box.width) * (count - 1))));
        cursor.hidden = false;
        cursor.style.left = `${(i / (count - 1)) * 100}%`;
        show(i);
      });
      plot.addEventListener("pointerleave", () => {
        cursor.hidden = true;
        show(count - 1);
      });
    }
  }
  customElements.define("kit-chart", KitChart);

  // A trend in a row: values="1,2,3", a line and nothing else.
  class KitSpark extends HTMLElement {
    static observedAttributes = ["values"];

    connectedCallback() {
      this.render();
    }

    attributeChangedCallback() {
      if (this.isConnected) this.render();
    }

    render() {
      const values = (this.getAttribute("values") || "").split(",").filter(Boolean).map(Number);
      if (values.length < 2) return;
      const low = Math.min(...values);
      const span = Math.max(...values) - low || 1;
      const path = values.map((v, i) => `${i ? "L" : "M"}${((i / (values.length - 1)) * 80).toFixed(1)} ${(22 - ((v - low) / span) * 20).toFixed(1)}`).join("");
      this.innerHTML = `<svg viewBox="0 0 80 24" preserveAspectRatio="none" aria-hidden="true"><path d="${path}" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke"/></svg>`;
      // The tip names the point under the pointer.
      const unit = this.getAttribute("unit") || "";
      this.dataset.tip = `${values.at(-1)}${unit}`;
      this.onpointermove = (event) => {
        const box = this.getBoundingClientRect();
        const i = Math.max(0, Math.min(values.length - 1, Math.round(((event.clientX - box.left) / box.width) * (values.length - 1))));
        this.dataset.tip = `${values[i]}${unit}`;
      };
    }
  }
  customElements.define("kit-spark", KitSpark);

  // A month to pick from: value="2026-11-09". With range, two days:
  // value="2026-11-03,2026-11-09". With name it posts in a form like a field.
  // months="2" shows two side by side, min="2026-11-09" locks the days before.
  const dayOf = (date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
  const nice = (day, year) => new Date(`${day}T12:00`).toLocaleDateString("en", { month: "short", day: "numeric", year });

  const CALENDAR_CONTROLS = ["data-day", "data-step", "data-year", "data-pick", "data-month"];

  class KitCalendar extends HTMLElement {
    static observedAttributes = ["value"];

    connectedCallback() {
      this.classList.add("kit-calendar");
      const first = (this.getAttribute("value") || "").split(",")[0];
      this.shown = first ? new Date(`${first}T12:00`) : new Date();
      this.onclick = (event) => this.press(event);
      this.render();
    }

    attributeChangedCallback() {
      if (this.shown) this.render();
    }

    press(event) {
      const pick = event.target.closest("[data-pick]");
      if (pick) {
        const offset = Number(pick.dataset.pick);
        this.picking = this.picking === offset ? undefined : offset;
        this.pickYear = new Date(this.shown.getFullYear(), this.shown.getMonth() + offset, 1).getFullYear();
        this.render();
        return;
      }
      const year = event.target.closest("[data-year]");
      if (year) {
        this.pickYear += Number(year.dataset.year);
        this.render();
        return;
      }
      const month = event.target.closest("[data-month]");
      if (month) {
        this.shown = new Date(this.pickYear, Number(month.dataset.month) - this.picking, 1);
        this.picking = undefined;
        this.render();
        return;
      }
      const step = event.target.closest("[data-step]");
      if (step) {
        this.shown = new Date(this.shown.getFullYear(), this.shown.getMonth() + Number(step.dataset.step), 1);
        this.render();
        return;
      }
      const day = event.target.closest("[data-day]")?.dataset.day;
      if (!day) return;
      const [start, end] = (this.getAttribute("value") || "").split(",");
      const value = this.hasAttribute("range") && start && !end ? [start, day].sort().join(",") : day;
      this.setAttribute("value", value);
      this.dispatchEvent(new Event("change", { bubbles: true }));
      const shown = value.includes(",") ? value.split(",").map((one) => nice(one)).join(" – ") : nice(value, "numeric");
      const opened = this.closest("details");
      if (opened) {
        opened.querySelector("[data-date]").textContent = shown;
        if (!this.hasAttribute("range") || value.includes(",")) opened.open = false;
      }
    }

    // In place of the days: the months of a year to jump to.
    monthPicker(offset, min) {
      const open = new Date(this.shown.getFullYear(), this.shown.getMonth() + offset, 1);
      const now = new Date();
      const months = Array.from({ length: 12 }, (_, i) => {
        const label = new Date(2024, i, 1).toLocaleDateString("en", { month: "short" });
        const picked = this.pickYear === open.getFullYear() && i === open.getMonth();
        const current = this.pickYear === now.getFullYear() && i === now.getMonth();
        const past = min && dayOf(new Date(this.pickYear, i + 1, 0)) < min;
        return `<button type="button" data-month="${i}" aria-pressed="${picked}"${current ? ' aria-current="date"' : ""}${past ? " disabled" : ""}>${label}</button>`;
      }).join("");
      return `<div class="kit-calendar-month"><div class="kit-calendar-head"><button type="button" class="kit-button" data-icon data-quiet data-year="-1" aria-label="Previous year"><i class="kit-icon kit-icon-chevron-left"></i></button><button type="button" class="kit-calendar-title" data-pick="${offset}" aria-expanded="true">${this.pickYear}<i class="kit-icon kit-icon-chevron-down" data-size="s"></i></button><button type="button" class="kit-button" data-icon data-quiet data-year="1" aria-label="Next year"><i class="kit-icon kit-icon-chevron-right"></i></button></div><div class="kit-calendar-pick">${months}</div></div>`;
    }

    render() {
      const focused = this.contains(document.activeElement) && CALENDAR_CONTROLS.map((name) => [name, document.activeElement.getAttribute(name)]).find(([, value]) => value !== null);
      const [start, end] = (this.getAttribute("value") || "").split(",");
      const band = start && end && start !== end;
      const min = this.getAttribute("min");
      const today = dayOf(new Date());
      const heads = Array.from({ length: 7 }, (_, i) => `<span>${new Date(2024, 0, 1 + i).toLocaleDateString("en", { weekday: "short" }).slice(0, 2)}</span>`).join("");
      const months = Array.from({ length: Number(this.getAttribute("months")) || 1 }, (_, offset) => {
        if (offset === this.picking) return this.monthPicker(offset, min);
        const first = new Date(this.shown.getFullYear(), this.shown.getMonth() + offset, 1);
        const year = first.getFullYear();
        const month = first.getMonth();
        const blank = (first.getDay() + 6) % 7;
        const count = new Date(year, month + 1, 0).getDate();
        const title = first.toLocaleDateString("en", { month: "long", year: "numeric" });
        const days = Array.from({ length: count }, (_, i) => {
          const day = dayOf(new Date(year, month, i + 1));
          const picked = day === start || day === end;
          const between = start && end && day > start && day < end;
          return `<button type="button" data-day="${day}" aria-pressed="${picked}"${day === today ? ' aria-current="date"' : ""}${between ? " data-between" : ""}${band && day === start ? " data-start" : ""}${band && day === end ? " data-end" : ""}${min && day < min ? " disabled" : ""}><span>${i + 1}</span></button>`;
        }).join("");
        return `<div class="kit-calendar-month"><div class="kit-calendar-head"><button type="button" class="kit-button" data-icon data-quiet data-step="-1" aria-label="Previous month"><i class="kit-icon kit-icon-chevron-left"></i></button><button type="button" class="kit-calendar-title" data-pick="${offset}" aria-expanded="false">${title}<i class="kit-icon kit-icon-chevron-down" data-size="s"></i></button><button type="button" class="kit-button" data-icon data-quiet data-step="1" aria-label="Next month"><i class="kit-icon kit-icon-chevron-right"></i></button></div><div class="kit-calendar-grid">${heads}${"<i></i>".repeat(blank)}${days}</div></div>`;
      }).join("");
      const name = this.getAttribute("name");
      this.innerHTML = `<div class="kit-calendar-months">${months}</div>${name ? `<input type="hidden" name="${name}" value="${this.getAttribute("value") || ""}">` : ""}`;
      if (focused) this.querySelector(`[${focused[0]}="${focused[1]}"]`)?.focus();
    }
  }
  customElements.define("kit-calendar", KitCalendar);
})();
