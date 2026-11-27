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
