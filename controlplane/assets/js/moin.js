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

// A Select is a popover of radio buttons. Picking one writes its label into
// the button; a click or Enter closes the list. The mouse focuses what it
// points at, so one option is lit at a time.
document.addEventListener("change", (event) => {
  const select = event.target.closest("[data-select]");
  if (!select || event.target.type !== "radio") return;
  select.querySelector("[data-select-label]").textContent = event.target.closest("label").textContent.trim();
});

document.addEventListener("click", (event) => {
  const option = event.target.closest("[data-select] [popover] label");
  if (option) requestAnimationFrame(() => option.closest("[popover]").hidePopover());
});

document.addEventListener("keydown", (event) => {
  const list = event.target.closest?.("[data-select] [popover]");
  if (list && event.key === "Enter") {
    event.preventDefault();
    list.hidePopover();
    list.closest("[data-select]").querySelector("button").focus();
  }
});

document.addEventListener("mouseover", (event) => {
  const option = event.target.closest("[data-select] [popover] label");
  if (option && !option.contains(document.activeElement)) option.querySelector("input").focus({ preventScroll: true });
});
