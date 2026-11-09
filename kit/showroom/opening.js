// A link to a part or a room: they load after the page, so the start page
// stays out of sight until then.
if (location.hash.length > 1 && location.hash !== "#kit") document.documentElement.dataset.opening = "";
