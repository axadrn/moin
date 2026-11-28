// site.js: the little the pages of the site cannot do in plain HTML.

// The mode switch in the footer: light or dark, kept in this browser.
// applyMode is in the head of every page (layouts.Base), so the first paint
// is in the right mode already.
document.addEventListener("click", (event) => {
  if (!event.target.closest("[data-mode-toggle]")) return;
  const dark = !document.documentElement.classList.contains("dark");
  try {
    localStorage.setItem("moin-mode", dark ? "dark" : "light");
  } catch {}
  window.applyMode(dark ? "dark" : "light");
});

// Copy: a data-copy button copies its value, or the code its
// data-copy-target names, and its icon turns into a check for a moment.
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  const target = button.dataset.copyTarget && document.querySelector(button.dataset.copyTarget);
  try {
    await navigator.clipboard.writeText(target ? target.textContent.trim() : button.dataset.copy);
  } catch {
    return;
  }
  button.dataset.copied = "";
  clearTimeout(button.copyTimer);
  button.copyTimer = setTimeout(() => delete button.dataset.copied, 1400);
});

// The marquee: a row of posts that moves slowly to the left by itself, and
// a hand can drag or swipe. It holds its posts twice: past the first copy
// it jumps back by one copy, which looks the same, so the loop has no seam.
const stillMotion = matchMedia("(prefers-reduced-motion: reduce)");
let marqueeRest = 0; // the time the rows may move again
let marqueeDrag = null; // a drag with the mouse: its row and last x
const restMarquee = () => (marqueeRest = performance.now() + 2000);

// Every frame: about 27 px a second, unless a hand is on it or was a
// moment ago. row.x keeps the fraction of a pixel the browser rounds away.
function driftMarquees() {
  for (const row of document.querySelectorAll("[data-marquee]")) {
    const half = row.scrollWidth / 2;
    let x = Math.abs(row.x - row.scrollLeft) < 1 ? row.x : row.scrollLeft;
    const busy = marqueeDrag || performance.now() < marqueeRest || row.matches(":hover, :focus-within");
    if (!busy) x += 0.45;
    if (x < 1) x += half;
    if (x >= half + 1) x -= half;
    row.x = x;
    row.scrollLeft = x;
  }
  requestAnimationFrame(driftMarquees);
}
if (!stillMotion.matches && document.querySelector("[data-marquee]")) requestAnimationFrame(driftMarquees);

// A hand on the row stops it for two seconds. A finger swipes the row
// natively; the mouse drags it here.
for (const type of ["touchstart", "wheel"]) {
  document.addEventListener(type, (event) => event.target.closest?.("[data-marquee]") && restMarquee(), { passive: true });
}
document.addEventListener("pointerdown", (event) => {
  const row = event.target.closest("[data-marquee]");
  if (!row) return;
  restMarquee();
  delete row.dataset.dragged;
  if (event.pointerType === "mouse") marqueeDrag = { row, x: event.clientX, moved: 0 };
});
document.addEventListener("pointermove", (event) => {
  if (!marqueeDrag) return;
  const { row } = marqueeDrag;
  row.scrollLeft -= event.clientX - marqueeDrag.x;
  marqueeDrag.moved += Math.abs(event.clientX - marqueeDrag.x);
  marqueeDrag.x = event.clientX;
  if (marqueeDrag.moved > 5) row.dataset.dragging = "";
  restMarquee();
});
for (const type of ["pointerup", "pointercancel"]) {
  document.addEventListener(type, () => {
    if (!marqueeDrag) return;
    const { row } = marqueeDrag;
    if (row.dataset.dragging !== undefined) row.dataset.dragged = "";
    delete row.dataset.dragging;
    marqueeDrag = null;
    restMarquee();
  });
}

// A drag is not a click: the link under the mouse stays shut. Links and
// pictures are not dragged away either.
document.addEventListener("click", (event) => {
  const row = event.target.closest("[data-marquee][data-dragged]");
  if (!row) return;
  event.preventDefault();
  delete row.dataset.dragged;
}, true);
document.addEventListener("dragstart", (event) => event.target.closest?.("[data-marquee]") && event.preventDefault());
