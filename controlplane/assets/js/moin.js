// moin.js: the little the HTML of the control panel cannot do by itself.
// Every part listens on document, so HTML that htmx swaps in works at once,
// with nothing to set up. One part per behaviour, each says what it does.

const typing = (target) => target instanceof Element && target.closest("input, textarea, select, [contenteditable]");

// The palette: the search in the middle of the screen. ⌘K, ctrl+k, / or the
// search button open it; the arrows pick an answer, Enter opens it, Escape
// or a click outside closes it (DESIGN.md, Navigation).
const palette = () => document.getElementById("palette");
const searchField = () => document.getElementById("search");
const answers = () => [...document.querySelectorAll("#search-results [role=option]")];

function openPalette() {
  const dialog = palette();
  if (!dialog || dialog.open) return;
  // On an app's page, what you can do with that app comes first.
  document.getElementById("search-here").value = document.querySelector("[data-app-page]")?.dataset.appPage || "";
  dialog.showModal();
  searchField().focus();
}

function pick(option) {
  for (const answer of answers()) answer.setAttribute("aria-selected", String(answer === option));
  option?.scrollIntoView({ block: "nearest" });
  searchField()?.setAttribute("aria-activedescendant", option?.id || "");
}

document.addEventListener("click", (event) => {
  if (event.target.closest("[data-open-palette]")) openPalette();
  // A click on the dim around the palette, or on an answer, closes it.
  if (event.target === palette() || event.target.closest("#search-results [role=option]")) palette().close();
});

document.addEventListener("close", (event) => {
  if (event.target !== palette()) return;
  searchField().value = "";
  document.getElementById("search-results").innerHTML = "";
  searchField().setAttribute("aria-expanded", "false");
}, true);

document.addEventListener("keydown", (event) => {
  if ((event.key === "k" && (event.metaKey || event.ctrlKey)) || (event.key === "/" && !typing(event.target))) {
    if (!palette()) return;
    event.preventDefault();
    openPalette();
    return;
  }
  if (event.target !== searchField()) return;
  const list = answers();
  const at = list.findIndex((option) => option.getAttribute("aria-selected") === "true");
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    const step = event.key === "ArrowDown" ? 1 : -1;
    pick(list[(at + step + list.length) % list.length]);
  } else if (event.key === "Enter") {
    event.preventDefault();
    (list[at] || list[0])?.click();
  }
});

// The mouse picks what it points at, so only one answer is ever lit.
document.addEventListener("mouseover", (event) => {
  const option = event.target.closest("#search-results [role=option]");
  if (option && option.getAttribute("aria-selected") !== "true") pick(option);
});

document.addEventListener("htmx:after:swap", (event) => {
  if (event.target.id === "search-results") searchField()?.setAttribute("aria-expanded", String(answers().length > 0));
});

// ? shows every key, as in the terminal UI.
document.addEventListener("keydown", (event) => {
  if (event.key === "?" && !typing(event.target) && !event.metaKey && !event.ctrlKey) {
    document.getElementById("app-shortcuts")?.showModal();
  }
});

// A page back from the back-forward cache has its live streams closed
// (pagehide): load it fresh.
addEventListener("pageshow", (event) => {
  if (event.persisted) location.reload();
});

// hx-sse 4.0.0 aborts a stream it closes on purpose (hx-sse:close, a page
// left) and leaves that abort unhandled. It is not an error: keep it out of
// the console.
addEventListener("unhandledrejection", (event) => {
  if (event.reason?.name === "AbortError") event.preventDefault();
});

// Feedback (DESIGN.md): the answer to a click shows where you clicked. The
// server sends it into #feedback (kit.Feedback); it goes to the element that
// sent the request. "done" turns its button into a check for a moment,
// "failed" and "note" put the words in a line under its form.
let asking = null;
// The answer goes to the element whose response it came with.
document.addEventListener("htmx:before:response", (event) => { asking = event.target; });

const buttonOf = (source) =>
  source?.closest("button") ||
  (document.activeElement?.closest?.("button") && source?.contains(document.activeElement) ? document.activeElement.closest("button") : null) ||
  source?.querySelector?.("button[type=submit], button:not([type])");

// saySaid puts a line under the form or button that sent a request. It
// floats under it, so nothing on the page moves, and goes with the next try
// or after a while.
function saySaid(source, kind, text) {
  const place = source?.closest("form") || source;
  const box = place?.parentElement;
  if (!box) return;
  box.querySelectorAll(":scope > [data-said]").forEach((line) => line.remove());
  if (getComputedStyle(box).position === "static") box.style.position = "relative";
  const line = Object.assign(document.createElement("p"), { role: kind === "failed" ? "alert" : "status", textContent: text });
  line.dataset.said = kind;
  line.style.top = `${place.offsetTop + place.offsetHeight + 6}px`;
  const right = box.clientWidth - place.offsetLeft - place.offsetWidth;
  if (right < place.offsetLeft) line.style.right = `${right}px`;
  else line.style.left = `${place.offsetLeft}px`;
  box.append(line);
  setTimeout(() => line.remove(), 8000);
}

const feedbackBox = document.getElementById("feedback");
if (feedbackBox) new MutationObserver(() => {
  for (const answer of [...feedbackBox.children]) {
    answer.remove();
    const { feedback: kind, title } = answer.dataset;
    const more = answer.textContent.trim();
    if (kind === "done") {
      const button = buttonOf(asking);
      if (button) {
        button.dataset.done = "";
        setTimeout(() => delete button.dataset.done, 1600);
      }
      if (more) saySaid(asking, "note", more);
    } else {
      const text = [title, more].filter(Boolean).join(". ");
      saySaid(asking, kind, text);
    }
  }
}).observe(feedbackBox, { childList: true });

// A new try clears the line of the last one.
document.addEventListener("htmx:before:request", (event) => {
  const place = event.target.closest("form") || event.target;
  place.parentElement?.querySelectorAll(":scope > [data-said]").forEach((line) => line.remove());
});

// A field a request failed on turns red and takes the focus; its message
// replaces the hint under it, or goes in a line under its form when it has
// none. Changing the field clears it again.
document.addEventListener("moin:invalid", (event) => {
  const { field: id, title, message } = event.detail;
  const field = document.getElementById(id);
  if (!field) return;
  const hint = document.querySelector(`[data-field-hint="${CSS.escape(id)}"]`);
  const error = hint?.querySelector("[data-hint-error]");
  field.setAttribute("aria-invalid", "true");
  if (message && error) {
    error.textContent = message;
    hint.dataset.invalid = "";
  } else if (message) {
    saySaid(field, "failed", message);
  }
  field.focus();
  field.addEventListener("input", () => {
    field.removeAttribute("aria-invalid");
    if (hint) delete hint.dataset.invalid;
  }, { once: true });
});

// Appearance in Settings: System, Light or Dark, kept in this browser.
const markTheme = () => {
  const theme = window.themeUtils?.getThemePreference() || "system";
  document.querySelectorAll("[data-theme-set]").forEach((button) => button.setAttribute("aria-pressed", String(button.dataset.themeSet === theme)));
};
document.addEventListener("click", (event) => {
  const button = event.target.closest("[data-theme-set]");
  if (!button) return;
  try {
    if (button.dataset.themeSet === "system") localStorage.removeItem("theme");
    else localStorage.setItem("theme", button.dataset.themeSet);
  } catch {}
  window.themeUtils?.applyTheme();
  markTheme();
});
markTheme();
