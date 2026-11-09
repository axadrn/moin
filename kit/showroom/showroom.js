// The showroom only. Parts come from docs/, rooms from rooms/: one source
// for each, and this page only shows them.
const read = (path) => fetch(path).then((response) => response.text());
const groupsOf = (index) => index.split(/^## /m).slice(1).map((block) => ({
  group: block.split("\n")[0],
  files: [...block.matchAll(/\]\((.+?)\)/g)].map((link) => link[1]),
}));
const toSlug = (title) => title.toLowerCase().replace(/[^a-z0-9]+/g, "-");
const icons = "activity app apps bell book bucket catalog check chevron-down chevron-left chevron-right copy card database deploy disk external failed gauge globe key logs package play plus restart search server server-off settings shield shield-off stop theme trash undo user users x git-hub google".split(" ");
const RESIZE = ["grid-that-fits", "grid-with-columns"];
const JS_NEEDS = [["kit-calendar", "calendar"], [".kit-select", "select"], ["[data-confirm]", "confirm"], ["[data-open-dialog]", "dialog"], ["[data-open-palette]", "palette"], ["[data-copy]", "copy"], ["[data-tip]", "tips"], ["kit-chart", "chart"], ["kit-spark", "sparkline"]];

// One example, bundled in one frame: its name, what it looks like, its code.
let exampleCount = 0;
function addExample(list, part, heading, code, single) {
  const id = `demo-code-${exampleCount++}`;
  const frame = document.createElement("div");
  frame.className = "demo-frame";
  frame.innerHTML = `<div class="demo-frame-head">${single ? "<span></span>" : `<h3>${heading}</h3>`}<button type="button" class="demo-code-toggle" aria-expanded="false" aria-controls="${id}">Code</button></div><div class="demo-frame-body"></div><div class="kit-code" id="${id}" hidden><button type="button" class="kit-copy" data-copy data-copy-target="#${id}-text" aria-label="Copy the code"><i class="kit-icon kit-icon-copy"></i></button><pre id="${id}-text" tabindex="0"></pre></div>`;
  const toggle = frame.querySelector(".demo-code-toggle");
  const preview = frame.querySelector(".demo-frame-body");
  const box = frame.querySelector(":scope > .kit-code");
  // The palette's dialog is on this page already, once is enough.
  const shown = code.replace(/<dialog id="kit-palette"[\s\S]*<\/dialog>/, "");
  const parsed = document.createElement("template");
  parsed.innerHTML = shown;
  const uses = JS_NEEDS.filter(([selector]) => parsed.content.querySelector(selector) || code.includes(selector.replace(/[\[\]]/g, ""))).map(([, word]) => word);
  box.querySelector("pre").textContent = (uses.length ? `<!-- Needs kit.js: ${uses.join(", ")} -->\n\n` : "") + code;
  toggle.addEventListener("click", () => {
    box.hidden = !box.hidden;
    preview.hidden = !box.hidden;
    toggle.setAttribute("aria-expanded", String(!box.hidden));
    toggle.textContent = box.hidden ? "Code" : "Preview";
  });
  list.append(frame);
  const html = part === "icon"
    ? `<div class="demo-icons">${icons.map((icon) => `<div><i class="kit-icon kit-icon-${icon}"></i><span class="kit-small kit-muted">${icon}</span></div>`).join("")}</div>`
    : RESIZE.includes(part) ? `<div class="demo-resize">${shown}</div>` : shown;
  return { fill: () => (preview.innerHTML = html), clear: () => preview.replaceChildren() };
}

// One part from its page: the title, a sentence, its examples.
const partExamples = new Map();
function addPart(markdown, group, examples) {
  const title = markdown.match(/^# (.+)$/m)[1];
  const name = toSlug(title);
  const section = document.createElement("div");
  section.className = "kit-section";
  section.id = `part-${name}`;
  if (group !== "Layout") section.dataset.group = group;
  section.innerHTML = `<h2 class="demo-part-title">${title}</h2><p class="kit-text kit-muted"></p><div class="kit-stack" data-gap="6"></div>`;
  section.querySelector("p").textContent = markdown.split("\n\n")[1];
  const list = section.querySelector(":scope > .kit-stack");
  const shown = examples.map(({ heading, code }) => addExample(list, name, heading, code, examples.length === 1));
  partExamples.set(section, shown);
  document.querySelector(`[data-pane="${group === "Layout" ? "layout" : "parts"}"]`).append(section);
  if (group === "Layout") shown.forEach((example) => example.fill());
}

// One room from its page: what is in <main>, and the file to copy.
const roomFiles = new Map();
function addRoom(html, view, group) {
  const page = new DOMParser().parseFromString(html, "text/html");
  const room = document.importNode(page.querySelector("main > section"), true);
  room.dataset.view = view;
  room.dataset.name = page.title;
  room.dataset.group = group;
  room.hidden = true;
  roomFiles.set(view, html.replaceAll('"../kit', '"kit/kit'));
  document.getElementById("demo-room-body").append(room);
}

const [partIndex, roomIndex] = await Promise.all([read("../docs/README.md"), read("../rooms/README.md")]);
const roomList = groupsOf(roomIndex).flatMap(({ group, files }) => files.map((file) => ({ group, file })));
const parts = await Promise.all(groupsOf(partIndex).flatMap(({ group, files }) => files.map(async (file) => {
  const markdown = await read(`../docs/${file}`);
  const examples = await Promise.all(markdown.split(/^## /m).slice(1).map(async (block) => ({
    heading: block.split("\n")[0],
    code: (await read(block.match(/\]\((.+?)\)/)[1])).trim(),
  })));
  return { group, markdown, examples };
})));
const roomTexts = await Promise.all(roomList.map(({ file }) => read(`../rooms/${file}`)));
parts.forEach(({ group, markdown, examples }) => addPart(markdown, group, examples));
roomTexts.forEach((text, i) => addRoom(text, roomList[i].file.replace(".html", ""), roomList[i].group));
const pages = [...document.querySelectorAll("[data-view]")].filter((view) => view.dataset.view !== "kit");


// The same two buttons in every language.
const languages = {
  HTML: `<div class="kit-row" data-gap="2">
  <button type="button" class="kit-button" data-kind="primary">Deploy</button>
  <button type="button" class="kit-button">Cancel</button>
</div>`,
  templ: `type Kind string

const Primary Kind = "primary"

type ButtonProps struct {
	Kind  Kind
	Attrs templ.Attributes
}

templ Button(p ButtonProps) {
	<button
		type="button"
		class="kit-button"
		if p.Kind != "" {
			data-kind={ string(p.Kind) }
		}
		{ p.Attrs... }
	>
		{ children... }
	</button>
}

templ Actions() {
	<div class="kit-row" data-gap="2">
		@Button(ButtonProps{Kind: Primary}) {
			Deploy
		}
		@Button(ButtonProps{}) {
			Cancel
		}
	</div>
}`,
  Go: `const page = \`
{{define "button"}}<button type="button" class="kit-button"{{if .Kind}} data-kind="{{.Kind}}"{{end}}>{{.Label}}</button>{{end}}

<div class="kit-row" data-gap="2">
  {{template "button" .Deploy}}
  {{template "button" .Cancel}}
</div>\`

type Button struct{ Kind, Label string }

func main() {
	template.Must(template.New("page").Parse(page)).Execute(os.Stdout, map[string]Button{
		"Deploy": {Kind: "primary", Label: "Deploy"},
		"Cancel": {Label: "Cancel"},
	})
}`,
  Ruby: `require "erb"
include ERB::Util

def button(label, kind: nil)
  data = kind ? %( data-kind="#{h kind}") : ""
  %(<button type="button" class="kit-button"#{data}>#{h label}</button>)
end

puts ERB.new(<<~PAGE).result
  <div class="kit-row" data-gap="2">
    <%= button("Deploy", kind: "primary") %>
    <%= button("Cancel") %>
  </div>
PAGE`,
  Python: `{% macro button(label, kind="") -%}
<button type="button" class="kit-button"{% if kind %} data-kind="{{ kind }}"{% endif %}>{{ label }}</button>
{%- endmacro %}

<div class="kit-row" data-gap="2">
  {{ button("Deploy", kind="primary") }}
  {{ button("Cancel") }}
</div>`,
  PHP: `<?php
function button(string $label, string $kind = ''): string {
    $data = $kind ? ' data-kind="' . htmlspecialchars($kind) . '"' : '';
    return '<button type="button" class="kit-button"' . $data . '>' . htmlspecialchars($label) . '</button>';
}
?>
<div class="kit-row" data-gap="2">
  <?= button('Deploy', kind: 'primary') ?>
  <?= button('Cancel') ?>
</div>`,
  Rust: `use maud::{html, Markup};

fn button(label: &str, kind: Option<&str>) -> Markup {
    html! { button.kit-button type="button" data-kind=[kind] { (label) } }
}

fn actions() -> Markup {
    html! {
        .kit-row data-gap="2" {
            (button("Deploy", Some("primary")))
            (button("Cancel", None))
        }
    }
}`,
};
const langs = document.getElementById("demo-langs");
const langText = document.getElementById("demo-lang-text");
langs.innerHTML = Object.keys(languages).map((name, i) => `<label><input type="radio" name="demo-lang" value="${name}"${i ? "" : " checked"}>${name}</label>`).join("");
langText.textContent = languages.HTML;
langs.addEventListener("change", (event) => (langText.textContent = languages[event.target.value]));

const panes = [...document.querySelectorAll("[data-pane]")];

// The list of the parts, read from the samples, so it cannot go stale.
const slug = (section) => section.querySelector("h2").textContent.toLowerCase().replace(/[^a-z0-9]+/g, "-");
const samples = [...document.querySelectorAll('[data-pane="parts"] > .kit-section[data-group]')];
function groupedList(items) {
  return [...new Set(items.map((item) => item.group))].map((group) => `<div class="kit-section"><div class="kit-section-head"><h2>${group}</h2></div><div class="kit-list">${items.filter((item) => item.group === group).map((item) => `<a href="#${item.id}" class="kit-item"><div class="kit-item-text"><div class="kit-item-name">${item.name}</div></div><i class="kit-chevron"></i></a>`).join("")}</div></div>`).join("");
}
document.getElementById("demo-index").innerHTML = groupedList(samples.map((section) => ({ group: section.dataset.group, id: slug(section), name: section.querySelector("h2").textContent })));
document.getElementById("kit-results").innerHTML = [["kit", "Kit"], ...pages.map((view) => [view.dataset.view, view.dataset.name]), ...samples.map((section) => [slug(section), section.querySelector("h2").textContent])].map(([id, name]) => `<a class="kit-option" role="option" href="#${id}"><span class="kit-fill">${name}</span></a>`).join("");
document.getElementById("demo-links").innerHTML = groupedList(pages.map((view) => ({ group: view.dataset.group, id: view.dataset.view, name: view.dataset.name })));

// In a sample a link stays where it is. A tab shows what it would open.
document.addEventListener("click", (event) => {
  const link = event.target.closest('[data-pane] a[href="#kit"], [data-name] a[href^="#"]');
  if (!link) return;
  event.preventDefault();
  if (!link.matches(".kit-tab") || link.closest("[data-name]")) return;
  link.parentElement.querySelectorAll(".kit-tab").forEach((tab) => tab.removeAttribute("aria-current"));
  link.setAttribute("aria-current", "page");
  document.getElementById("demo-tab-text").textContent = link.textContent;
});

let roomCode = false;
document.getElementById("demo-room-toggle").addEventListener("click", () => {
  roomCode = !roomCode;
  show();
});

let shownPart;
function show() {
  const hash = location.hash.slice(1);
  const room = pages.find((view) => view.dataset.view === hash);
  const sample = samples.find((section) => slug(section) === hash);
  const part = sample;
  const pane = panes.find((one) => one.dataset.pane === (room ? "rooms" : part ? "parts" : hash)) || panes[0];
  panes.forEach((one) => (one.hidden = one !== pane));
  samples.forEach((section) => (section.hidden = section !== part));
  // Only the part on screen is in the page: one example can be on two pages,
  // and its ids and radio groups must exist once.
  if (shownPart !== part) {
    if (shownPart) partExamples.get(shownPart).forEach((example) => example.clear());
    if (part) partExamples.get(part).forEach((example) => example.fill());
    shownPart = part;
  }
  document.getElementById("demo-index").hidden = Boolean(part);
  document.getElementById("demo-back").hidden = !part;
  pages.forEach((view) => (view.hidden = view !== room || roomCode));
  document.getElementById("demo-links").hidden = Boolean(room);
  document.getElementById("demo-room-bar").hidden = !room;
  document.getElementById("demo-room-source").hidden = !room || !roomCode;
  if (room) {
    document.getElementById("demo-room-name").textContent = room.dataset.name;
    document.getElementById("demo-room-code").textContent = roomFiles.get(room.dataset.view);
    document.getElementById("demo-room-open").href = `../rooms/${room.dataset.view}.html`;
    const toggle = document.getElementById("demo-room-toggle");
    toggle.textContent = roomCode ? "Preview" : "Code";
    toggle.setAttribute("aria-expanded", String(roomCode));
  }
  document.querySelectorAll("#demo-tabs a").forEach((tab) => {
    if (tab.hash === `#${pane.dataset.pane}`) tab.setAttribute("aria-current", "page");
    else tab.removeAttribute("aria-current");
  });
}
// A link starts at the top of what it opens. Back and forward return to
// where you were.
history.scrollRestoration = "manual";
const scrolled = new Map();
let followed = false;
document.addEventListener("click", (event) => {
  if (!event.defaultPrevented && event.target.closest('a[href^="#"]')) followed = true;
});
addEventListener("hashchange", (event) => {
  scrolled.set(new URL(event.oldURL).hash, scrollY);
  roomCode = false;
  show();
  scrollTo(0, followed ? 0 : scrolled.get(location.hash) || 0);
  followed = false;
});
show();

// A demo button that did its job.
document.addEventListener("kit:confirmed", (event) => event.target.closest("[data-gone]")?.remove());
