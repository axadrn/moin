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
  // On an app's page, what you can do with that app comes first; then
  // what this browser picked most and latest.
  document.getElementById("search-here").value = document.querySelector("[data-app-page]")?.dataset.appPage || "";
  document.getElementById("search-recent").value = recentPicks().join("\n");
  dialog.showModal();
  searchField().focus();
}

// Recent: the palette remembers each pick in this browser, and how often,
// so what you open most and latest comes first (frecency). Nothing leaves
// the browser but the order.
const recentKey = "moin-palette-recent";

function readPicks() {
  try {
    return JSON.parse(localStorage.getItem(recentKey)) || {};
  } catch {
    return {};
  }
}

function rememberPick(id) {
  const picks = readPicks();
  picks[id] = { n: (picks[id]?.n || 0) + 1, at: Date.now() };
  const kept = Object.entries(picks).sort((a, b) => frecency(b[1]) - frecency(a[1])).slice(0, 50);
  try {
    localStorage.setItem(recentKey, JSON.stringify(Object.fromEntries(kept)));
  } catch {}
}

// frecency halves the weight of a pick every week.
const frecency = (pick) => pick.n * 0.5 ** ((Date.now() - pick.at) / 6048e5);

const recentPicks = () =>
  Object.entries(readPicks())
    .sort((a, b) => frecency(b[1]) - frecency(a[1]))
    .slice(0, 20)
    .map(([id]) => id);

function pick(option) {
  for (const answer of answers()) answer.setAttribute("aria-selected", String(answer === option));
  option?.scrollIntoView({ block: "nearest" });
  searchField()?.setAttribute("aria-activedescendant", option?.id || "");
}

document.addEventListener("click", (event) => {
  if (event.target.closest("[data-open-palette]")) openPalette();
  const picked = event.target.closest("#search-results [data-pick]");
  if (picked) rememberPick(picked.dataset.pick);
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
  const inPalette = event.target === searchField();
  // In the palette ctrl+j and ctrl+k move, like fzf; elsewhere ⌘K, ctrl+k
  // and / open it.
  const down = event.key === "ArrowDown" || (inPalette && event.ctrlKey && event.key === "j");
  const up = event.key === "ArrowUp" || (inPalette && event.ctrlKey && event.key === "k");
  if (!inPalette && ((event.key === "k" && (event.metaKey || event.ctrlKey)) || (event.key === "/" && !typing(event.target)))) {
    if (!palette()) return;
    event.preventDefault();
    openPalette();
    return;
  }
  if (!inPalette) return;
  const list = answers();
  const at = list.findIndex((option) => option.getAttribute("aria-selected") === "true");
  if (down || up) {
    event.preventDefault();
    const step = down ? 1 : -1;
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
  source?.closest?.("button") ||
  (document.activeElement?.closest?.("button") && source?.contains(document.activeElement) ? document.activeElement.closest("button") : null) ||
  source?.querySelector?.("button[type=submit], button:not([type])");

// saySaid puts a line in the page under what sent a request: under its
// row, its form, or the head it sits in, never between buttons. It stays
// until the next try. An action from the palette has nothing left on the
// page: its line goes on top of the page.
function saySaid(source, kind, text) {
  const place = source?.isConnected
    ? source.closest?.("[role=listitem], li, form, header") || source
    : document.querySelector("#main > *");
  if (!place) return;
  clearSaid(place);
  const line = Object.assign(document.createElement("p"), { role: kind === "failed" ? "alert" : "status", textContent: text });
  line.dataset.said = kind;
  if (source?.isConnected) place.after(line);
  else place.prepend(line);
}

function clearSaid(place) {
  if (place.nextElementSibling?.matches("[data-said]")) place.nextElementSibling.remove();
  place.querySelector(":scope > [data-said]")?.remove();
}

const feedbackBox = document.getElementById("feedback");
if (feedbackBox) new MutationObserver(() => {
  for (const answer of [...feedbackBox.children]) {
    answer.remove();
    const { feedback: kind, title } = answer.dataset;
    const more = answer.textContent.trim();
    if (kind === "done") {
      const button = buttonOf(asking);
      // Done is the check in the button; the page shows the rest.
      if (button?.isConnected) {
        button.dataset.done = "";
        setTimeout(() => delete button.dataset.done, 1600);
      } else {
        saySaid(asking, "note", "✓ " + title);
      }
    } else {
      const text = [title, more].filter(Boolean).join(". ");
      saySaid(asking, kind, text);
    }
  }
}).observe(feedbackBox, { childList: true });

// A new try clears the line of the last one.
document.addEventListener("htmx:before:request", (event) => {
  clearSaid(event.target.closest?.("[role=listitem], li, form, header") || event.target);
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

// Keys like Neovim, on every page (DESIGN.md, Navigation): j and k go
// through the rows of the lists, Enter opens one, gg and G go to the top
// and the end, - goes one level up (the "‹" link of the page). None of them
// works while you type or a dialog is open.
const rows = () =>
  [...document.querySelectorAll("#main [role=list] > a, #main [role=list] > button, #main [role=list] > li a[href]")].filter((row) => row.offsetParent);
let lastG = 0;
document.addEventListener("keydown", (event) => {
  if (typing(event.target) || event.metaKey || event.ctrlKey || event.altKey || document.querySelector("dialog[open]")) return;
  if (event.key === "j" || event.key === "k") {
    const list = rows();
    if (!list.length) return;
    event.preventDefault();
    const at = list.indexOf(document.activeElement.closest?.("a, button"));
    const next = at < 0 ? (event.key === "j" ? 0 : list.length - 1) : Math.min(Math.max(at + (event.key === "j" ? 1 : -1), 0), list.length - 1);
    list[next].focus();
    list[next].scrollIntoView({ block: "nearest" });
  } else if (event.key === "G") {
    event.preventDefault();
    scrollTo({ top: document.body.scrollHeight });
  } else if (event.key === "g") {
    if (Date.now() - lastG < 500) {
      event.preventDefault();
      scrollTo({ top: 0 });
      lastG = 0;
    } else {
      lastG = Date.now();
    }
  } else if (event.key === "-") {
    const back = [...document.querySelectorAll("[data-back]")].find((link) => link.offsetParent);
    if (back) {
      event.preventDefault();
      back.click();
    }
  }
});

// Dialogs (kit.Dialog): a button with data-open-dialog opens its dialog, one
// with data-close-dialog closes the dialog it is in, and a click beside the
// dialog closes it, as Escape does.
document.addEventListener("click", (event) => {
  const opener = event.target.closest("[data-open-dialog]");
  if (opener) document.getElementById(opener.dataset.openDialog)?.showModal();
  if (event.target.closest("[data-close-dialog]")) event.target.closest("dialog")?.close();
  const dialog = event.target;
  if (dialog instanceof HTMLDialogElement && dialog.matches("[data-dialog]")) {
    const box = dialog.getBoundingClientRect();
    const inside = event.clientX >= box.left && event.clientX <= box.right && event.clientY >= box.top && event.clientY <= box.bottom;
    if (!inside) dialog.close();
  }
});

// Copy: a [data-copy] button copies its value, or the value or text of the
// element its data-copy-target selects, and is marked data-copied for a
// moment, so its icon turns into a check without changing its size. The
// browser's clipboard works on https and on localhost. moinCopy is there
// for scripts too.
window.moinCopy = async (text) => {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
};

document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  event.preventDefault();
  const target = button.dataset.copyTarget && document.querySelector(button.dataset.copyTarget);
  const text = target ? ("value" in target ? target.value : target.textContent).trim() : button.dataset.copy;
  if (!text || !(await window.moinCopy(text))) return;
  button.dataset.copied = "";
  clearTimeout(button.copyTimer);
  button.copyTimer = setTimeout(() => delete button.dataset.copied, 1400);
});
