// moin.js: the little the HTML of the control panel cannot do by itself.
// Every part listens on document, so HTML that htmx swaps in works at once,
// with nothing to set up. One part per behaviour, each says what it does.

const typing = (target) => target instanceof Element && target.closest("input, textarea, select, [contenteditable]");

// Menus are native popovers. Choosing an item closes its menu, unless the
// item stays open on purpose (the theme switch).
document.addEventListener("click", (event) => {
  const item = event.target.closest("[popover] [role=menuitem]");
  if (item && !item.hasAttribute("data-menu-keep")) item.closest("[popover]").hidePopover();
});

// The search in the bar. / or ⌘K jumps in, the arrows pick an answer, Enter
// opens it, Escape or a click elsewhere closes the list.
const searchField = () => document.getElementById("search");
const searchList = () => document.getElementById("search-results");
const answers = () => [...document.querySelectorAll("#search-results [role=option]")];

function closeSearch() {
  const list = searchList();
  if (list) list.innerHTML = "";
  searchField()?.setAttribute("aria-expanded", "false");
}

function pick(option) {
  for (const answer of answers()) answer.setAttribute("aria-selected", String(answer === option));
  option?.scrollIntoView({ block: "nearest" });
  searchField()?.setAttribute("aria-activedescendant", option?.id || "");
}

document.addEventListener("keydown", (event) => {
  const field = searchField();
  if (!field) return;
  if ((event.key === "k" && (event.metaKey || event.ctrlKey)) || (event.key === "/" && !typing(event.target))) {
    event.preventDefault();
    field.focus();
    field.select();
    return;
  }
  if (event.target !== field) return;
  const list = answers();
  const at = list.findIndex((option) => option.getAttribute("aria-selected") === "true");
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    const step = event.key === "ArrowDown" ? 1 : -1;
    pick(list[(at + step + list.length) % list.length]);
  } else if (event.key === "Enter") {
    event.preventDefault();
    (list[at] || list[0])?.click();
  } else if (event.key === "Escape") {
    closeSearch();
    field.blur();
  }
});

// The mouse picks what it points at, so only one answer is ever lit.
document.addEventListener("mouseover", (event) => {
  const option = event.target.closest("#search-results [role=option]");
  if (option && option.getAttribute("aria-selected") !== "true") pick(option);
  const item = event.target.closest("[popover] [role=menuitem]");
  if (item && document.activeElement !== item) item.focus({ preventScroll: true });
});

document.addEventListener("htmx:after:swap", (event) => {
  if (event.target.id === "search-results") searchField()?.setAttribute("aria-expanded", String(answers().length > 0));
});

document.addEventListener("click", (event) => {
  if (event.target.closest("#search-results [role=option]")) {
    closeSearch();
    searchField().value = "";
    searchField().blur();
  } else if (!event.target.closest("[data-search]")) {
    closeSearch();
  }
});

// ? shows every key, as in the terminal UI.
document.addEventListener("keydown", (event) => {
  if (event.key === "?" && !typing(event.target) && !event.metaKey && !event.ctrlKey) {
    document.getElementById("app-shortcuts")?.showModal();
  }
});

// space shows what you can do here, like the TUI: the search, already
// narrowed to the app the page is about.
document.addEventListener("keydown", (event) => {
  if (event.key !== " " || typing(event.target) || event.metaKey || event.ctrlKey || event.altKey) return;
  const field = searchField();
  const page = document.querySelector("[data-app-page]");
  if (!field || event.target.closest("button, a, [role=menuitem], [role=option]")) return;
  event.preventDefault();
  field.value = page ? page.dataset.appPage + " " : "";
  field.focus();
  field.dispatchEvent(new Event("input", { bubbles: true }));
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
