// The theme editor: <kit-theme-editor>. A theme is a list of CSS variables,
// so the editor only sets them on the page and writes them out to copy.

(() => {
  const COLORS = [
    ["--background", "Ground"],
    ["--foreground", "Text"],
    ["--card", "Card"],
    ["--popover", "Overlay"],
    ["--muted", "Control"],
    ["--muted-foreground", "Quiet text"],
    ["--primary", "Primary"],
    ["--primary-foreground", "On primary"],
    ["--live", "Live"],
    ["--running", "Running"],
    ["--failed", "Failed"],
    ["--terminal", "Code ground"],
    ["--terminal-foreground", "Code text"],
    ["--code-keyword", "Keyword"],
    ["--code-string", "String"],
    ["--code-comment", "Comment"],
    ["--code-function", "Function"],
    ["--code-number", "Number"],
  ];

  const GEIST = '"Geist", ui-sans-serif, system-ui, sans-serif';
  const INTER = '"Inter", ui-sans-serif, system-ui, sans-serif';
  const SYSTEM = "ui-sans-serif, system-ui, sans-serif";
  const SERIF = "ui-serif, Georgia, serif";
  const MONO = '"Geist Mono", ui-monospace, SFMono-Regular, Menlo, monospace';
  const BRICOLAGE = '"Bricolage Grotesque", ui-sans-serif, system-ui, sans-serif';

  // Fonts with a file to load. Copy theme writes out how to add the ones used.
  const FONTS = [
    { family: "Geist", file: "geist.woff2", type: "woff2", weight: "100 900", from: "https://vercel.com/font" },
    { family: "Geist Mono", file: "geist-mono.woff2", type: "woff2", weight: "100 900", from: "https://vercel.com/font" },
    { family: "Inter", file: "inter.woff2", type: "woff2", weight: "100 900", from: "https://rsms.me/inter/" },
    { family: "Bricolage Grotesque", file: "bricolage-grotesque.ttf", type: "truetype", weight: "700", from: "https://fonts.google.com/specimen/Bricolage+Grotesque" },
  ];

  function fontLines(vars) {
    return FONTS.filter((font) => Object.values(vars).some((value) => value.includes(`"${font.family}"`))).map((font) => `/* ${font.family}: get the file at ${font.from}
   and put it in /fonts. Add this to your <head>:
   <link rel="preload" href="/fonts/${font.file}" as="font" crossorigin> */
@font-face {
  font-family: "${font.family}";
  src: url("/fonts/${font.file}") format("${font.type}");
  font-weight: ${font.weight};
  font-display: optional;
}
`).join("\n");
  }

  const pair = (light, dark) => `light-dark(${light}, ${dark})`;

  // The neutrals of a theme, from one list of light and dark values.
  const base = (ground, card, control, quiet, text) => ({
    "--background": pair(...ground),
    "--card": pair(...card),
    "--popover": pair(card[0], control[1]),
    "--muted": pair(...control),
    "--muted-foreground": pair(...quiet),
    "--foreground": pair(...text),
    "--card-foreground": pair(...text),
    "--popover-foreground": pair(...text),
  });

  const accent = (light, dark) => ({
    "--primary": pair(light, dark),
    "--primary-foreground": pair("#ffffff", "#0b0b0d"),
    "--ring": pair(light, dark),
  });

  const hue = (light, dark) => pair(light, dark);

  // A whole theme, every value as [light, dark]. Code sits on the card and
  // uses the text color unless the theme says otherwise.
  const palette = (c) => ({
    ...base(c.ground, c.card, c.control, c.quiet, c.text),
    "--primary": pair(...c.primary),
    "--primary-foreground": pair(...c.onPrimary),
    "--ring": pair(...c.primary),
    "--chart-base": pair(...c.primary),
    "--live": pair(...c.live),
    "--running": pair(...c.running),
    "--failed": pair(...c.failed),
    "--terminal": pair(...(c.code || c.card)),
    "--terminal-foreground": pair(...(c.codeText || c.text)),
    "--code-keyword": pair(...c.keyword),
    "--code-string": pair(...c.string),
    "--code-comment": pair(...c.comment),
    "--code-function": pair(...c.func),
    "--code-number": pair(...c.number),
  });

  // The well-known dev themes, each with its light and its dark side. Their
  // notices are in THEMES.md.
  const THEMES = [
    ["Dracula", palette({
      ground: ["#f4eed5", "#21222c"], card: ["#fffbeb", "#282a36"], control: ["#e8e1c5", "#44475a"], quiet: ["#6c664b", "#a3abc9"], text: ["#1f1f1f", "#f8f8f2"],
      primary: ["#644ac9", "#bd93f9"], onPrimary: ["#fffbeb", "#282a36"], live: ["#14710a", "#50fa7b"], running: ["#846e15", "#ffb86c"], failed: ["#cb3a2a", "#ff5555"],
      keyword: ["#a3144d", "#ff79c6"], string: ["#846e15", "#f1fa8c"], comment: ["#6c664b", "#6272a4"], func: ["#14710a", "#50fa7b"], number: ["#644ac9", "#bd93f9"],
    })],
    ["Solarized", palette({
      ground: ["#eee8d5", "#002b36"], card: ["#fdf6e3", "#073642"], control: ["#e3dcc6", "#0f4552"], quiet: ["#586e75", "#93a1a1"], text: ["#073642", "#eee8d5"],
      primary: ["#268bd2", "#268bd2"], onPrimary: ["#fdf6e3", "#fdf6e3"], live: ["#859900", "#859900"], running: ["#b58900", "#b58900"], failed: ["#dc322f", "#dc322f"],
      code: ["#fdf6e3", "#002b36"], codeText: ["#657b83", "#839496"],
      keyword: ["#859900", "#859900"], string: ["#2aa198", "#2aa198"], comment: ["#93a1a1", "#586e75"], func: ["#268bd2", "#268bd2"], number: ["#d33682", "#d33682"],
    })],
    ["Nord", palette({
      ground: ["#e5e9f0", "#2e3440"], card: ["#eceff4", "#3b4252"], control: ["#d8dee9", "#434c5e"], quiet: ["#4c566a", "#a8b1c2"], text: ["#2e3440", "#eceff4"],
      primary: ["#5e81ac", "#88c0d0"], onPrimary: ["#eceff4", "#2e3440"], live: ["#a3be8c", "#a3be8c"], running: ["#d08770", "#ebcb8b"], failed: ["#bf616a", "#bf616a"],
      code: ["#eceff4", "#2e3440"], codeText: ["#2e3440", "#d8dee9"],
      keyword: ["#5e81ac", "#81a1c1"], string: ["#6a8a4f", "#a3be8c"], comment: ["#7b8799", "#616e88"], func: ["#4f8a9c", "#88c0d0"], number: ["#8d6a87", "#b48ead"],
    })],
    ["Gruvbox", palette({
      ground: ["#f2e5bc", "#1d2021"], card: ["#fbf1c7", "#282828"], control: ["#ebdbb2", "#3c3836"], quiet: ["#665c54", "#a89984"], text: ["#3c3836", "#ebdbb2"],
      primary: ["#af3a03", "#fe8019"], onPrimary: ["#fbf1c7", "#282828"], live: ["#79740e", "#b8bb26"], running: ["#b57614", "#fabd2f"], failed: ["#9d0006", "#fb4934"],
      keyword: ["#9d0006", "#fb4934"], string: ["#79740e", "#b8bb26"], comment: ["#928374", "#928374"], func: ["#427b58", "#8ec07c"], number: ["#8f3f71", "#d3869b"],
    })],
    ["Catppuccin", palette({
      ground: ["#e6e9ef", "#181825"], card: ["#eff1f5", "#1e1e2e"], control: ["#ccd0da", "#313244"], quiet: ["#6c6f85", "#a6adc8"], text: ["#4c4f69", "#cdd6f4"],
      primary: ["#8839ef", "#cba6f7"], onPrimary: ["#eff1f5", "#1e1e2e"], live: ["#40a02b", "#a6e3a1"], running: ["#df8e1d", "#f9e2af"], failed: ["#d20f39", "#f38ba8"],
      keyword: ["#8839ef", "#cba6f7"], string: ["#40a02b", "#a6e3a1"], comment: ["#9ca0b0", "#6c7086"], func: ["#1e66f5", "#89b4fa"], number: ["#fe640b", "#fab387"],
    })],
    ["Tokyo Night", palette({
      ground: ["#d6d8df", "#16161e"], card: ["#e6e7ed", "#1a1b26"], control: ["#cfd1d9", "#292e42"], quiet: ["#707280", "#787c99"], text: ["#343b59", "#c0caf5"],
      primary: ["#2959aa", "#7aa2f7"], onPrimary: ["#ffffff", "#1a1b26"], live: ["#385f0d", "#9ece6a"], running: ["#8f5e15", "#e0af68"], failed: ["#bd4040", "#f7768e"],
      codeText: ["#343b59", "#a9b1d6"],
      keyword: ["#65359d", "#bb9af7"], string: ["#385f0d", "#9ece6a"], comment: ["#888b94", "#51597d"], func: ["#2959aa", "#7aa2f7"], number: ["#965027", "#ff9e64"],
    })],
    ["One", palette({
      ground: ["#f0f0f0", "#21252b"], card: ["#fafafa", "#282c34"], control: ["#e5e5e6", "#3e4452"], quiet: ["#696c77", "#9da5b4"], text: ["#383a42", "#abb2bf"],
      primary: ["#4078f2", "#61afef"], onPrimary: ["#fafafa", "#21252b"], live: ["#50a14f", "#98c379"], running: ["#c18401", "#e5c07b"], failed: ["#e45649", "#e06c75"],
      keyword: ["#a626a4", "#c678dd"], string: ["#50a14f", "#98c379"], comment: ["#a0a1a7", "#5c6370"], func: ["#4078f2", "#61afef"], number: ["#986801", "#d19a66"],
    })],
    ["Rosé Pine", palette({
      ground: ["#faf4ed", "#191724"], card: ["#fffaf3", "#1f1d2e"], control: ["#f2e9e1", "#26233a"], quiet: ["#797593", "#908caa"], text: ["#464261", "#e0def4"],
      primary: ["#907aa9", "#c4a7e7"], onPrimary: ["#fffaf3", "#191724"], live: ["#286983", "#9ccfd8"], running: ["#ea9d34", "#f6c177"], failed: ["#b4637a", "#eb6f92"],
      keyword: ["#286983", "#31748f"], string: ["#ea9d34", "#f6c177"], comment: ["#9893a5", "#6e6a86"], func: ["#d7827e", "#ebbcba"], number: ["#907aa9", "#c4a7e7"],
    })],
    ["Everforest", palette({
      ground: ["#f4f0d9", "#2d353b"], card: ["#fdf6e3", "#343f44"], control: ["#e6e2cc", "#3d484d"], quiet: ["#6f7f76", "#9da9a0"], text: ["#5c6a72", "#d3c6aa"],
      primary: ["#8da101", "#a7c080"], onPrimary: ["#fdf6e3", "#2d353b"], live: ["#8da101", "#a7c080"], running: ["#dfa000", "#dbbc7f"], failed: ["#f85552", "#e67e80"],
      code: ["#fdf6e3", "#272e33"],
      keyword: ["#f85552", "#e67e80"], string: ["#8da101", "#a7c080"], comment: ["#939f91", "#859289"], func: ["#3a94c5", "#7fbbb3"], number: ["#df69ba", "#d699b6"],
    })],
    ["Kanagawa", palette({
      ground: ["#e5ddb0", "#16161d"], card: ["#f2ecbc", "#1f1f28"], control: ["#dcd5ac", "#363646"], quiet: ["#716e61", "#a6a69c"], text: ["#545464", "#dcd7ba"],
      primary: ["#4d699b", "#7e9cd8"], onPrimary: ["#f2ecbc", "#1f1f28"], live: ["#6f894e", "#98bb6c"], running: ["#cc6d00", "#e6c384"], failed: ["#c84053", "#e46876"],
      keyword: ["#624c83", "#957fb8"], string: ["#6f894e", "#98bb6c"], comment: ["#8a8980", "#727169"], func: ["#4d699b", "#7e9cd8"], number: ["#b35b79", "#d27e99"],
    })],
    ["Ayu", palette({
      ground: ["#f3f4f5", "#0d1017"], card: ["#fcfcfc", "#10141c"], control: ["#eceef0", "#1c212b"], quiet: ["#787b80", "#8a919c"], text: ["#5c6166", "#bfbdb6"],
      primary: ["#f29718", "#e6b450"], onPrimary: ["#2b1a00", "#0d1017"], live: ["#86b300", "#aad94c"], running: ["#eba400", "#ffb454"], failed: ["#e65050", "#d95757"],
      keyword: ["#fa8532", "#ff8f40"], string: ["#86b300", "#aad94c"], comment: ["#adaeb1", "#5a6673"], func: ["#eba400", "#ffb454"], number: ["#a37acc", "#d2a6ff"],
    })],
    ["GitHub", palette({
      ground: ["#f6f8fa", "#010409"], card: ["#ffffff", "#0d1117"], control: ["#eaeef2", "#212830"], quiet: ["#59636e", "#9198a1"], text: ["#1f2328", "#f0f6fc"],
      primary: ["#0969da", "#4493f8"], onPrimary: ["#ffffff", "#0d1117"], live: ["#1a7f37", "#3fb950"], running: ["#9a6700", "#d29922"], failed: ["#d1242f", "#f85149"],
      code: ["#f6f8fa", "#151b23"],
      keyword: ["#cf222e", "#ff7b72"], string: ["#0a3069", "#a5d6ff"], comment: ["#59636e", "#9198a1"], func: ["#8250df", "#d2a8ff"], number: ["#0550ae", "#79c0ff"],
    })],
  ];

  // Each setting is a choice of a few options, each option a few variables.
  // The first option with no variables is the kit's own.
  const SETTINGS = [
    { id: "palette", label: "Palette", kind: "theme", options: [["Kit", {}], ...THEMES] },
    { id: "base", label: "Base color", kind: "select", options: [
      ["Gray", {}],
      ["Stone", base(["#f4f1ea", "#171512"], ["#fffdf8", "#23201b"], ["#e8e3d8", "#2e2a24"], ["#625b4f", "#a39a8a"], ["#1c1a16", "#f4f1ea"])],
      ["Slate", base(["#eef1f8", "#070a14"], ["#ffffff", "#121729"], ["#dfe4f0", "#1c2238"], ["#525b74", "#8e97b3"], ["#0f1424", "#eef1f8"])],
      ["Mauve", base(["#f5f1f6", "#120d14"], ["#ffffff", "#1f1823"], ["#e7e0ea", "#2b2230"], ["#615768", "#a397a9"], ["#1b1520", "#f5f1f6"])],
      ["Olive", base(["#f1f2ea", "#10120c"], ["#fdfef8", "#1c1f17"], ["#e2e5d6", "#292d21"], ["#5a5f4c", "#9ca28b"], ["#191c12", "#f1f2ea"])],
    ] },
    { id: "accent", label: "Accent", kind: "select", options: [
      ["Ink", {}],
      ["Blue", accent("#2a4bd7", "#8ea6ff")],
      ["Green", accent("#157a43", "#5fd38d")],
      ["Orange", accent("#c2560a", "#ff9d5c")],
      ["Rose", accent("#c2185b", "#ff7fae")],
      ["Violet", accent("#6d3bd1", "#b79bff")],
    ] },
    { id: "chart", label: "Chart color", kind: "select", options: [
      ["Ink", {}],
      ["Blue", { "--chart-base": hue("#2a4bd7", "#8ea6ff") }],
      ["Green", { "--chart-base": hue("#157a43", "#5fd38d") }],
      ["Orange", { "--chart-base": hue("#c2560a", "#ff9d5c") }],
      ["Rose", { "--chart-base": hue("#c2185b", "#ff7fae") }],
      ["Violet", { "--chart-base": hue("#6d3bd1", "#b79bff") }],
    ] },
    { id: "font", label: "Text font", kind: "select", options: [
      ["System", {}],
      ["Geist", { "--font-sans": GEIST }],
      ["Inter", { "--font-sans": INTER }],
      ["Serif", { "--font-sans": SERIF }],
      ["Mono", { "--font-sans": MONO }],
    ] },
    { id: "heading", label: "Heading font", kind: "select", options: [
      ["Same as text", {}],
      ["Geist", { "--font-heading": GEIST }],
      ["Inter", { "--font-heading": INTER }],
      ["System", { "--font-heading": SYSTEM }],
      ["Serif", { "--font-heading": SERIF }],
      ["Mono", { "--font-heading": MONO }],
      ["Bricolage", { "--font-heading": BRICOLAGE }],
    ] },
    { id: "code", label: "Code font", kind: "select", options: [
      ["System mono", {}],
      ["Geist Mono", { "--font-mono": MONO }],
    ] },
    { id: "menu", label: "Menu color", kind: "select", options: [
      ["Default", {}],
      ["Inverted", { "--menu": "var(--foreground)", "--menu-foreground": "var(--background)" }],
      ["Translucent", { "--menu": "color-mix(in oklab, var(--popover) 72%, transparent)", "--menu-blur": "blur(24px)" }],
    ] },
    { id: "lit", label: "Menu accent", kind: "select", options: [
      ["Subtle", {}],
      ["Bold", { "--menu-lit": "var(--primary)", "--menu-lit-foreground": "var(--primary-foreground)" }],
    ] },
    { id: "style", label: "Style", kind: "choice", start: 1, options: [
      ["Compact", { "--density": "0.85" }],
      ["Default", {}],
      ["Spacious", { "--density": "1.15" }],
    ] },
    { id: "size", label: "Size", kind: "choice", start: 1, options: [
      ["Small", { "--scale": "0.9" }],
      ["Default", {}],
      ["Large", { "--scale": "1.1" }],
    ] },
    { id: "weight", label: "Heading weight", kind: "choice", start: 1, options: [
      ["Medium", { "--weight-title": "500" }],
      ["Semibold", {}],
      ["Bold", { "--weight-title": "700" }],
    ] },
    { id: "spacing", label: "Heading spacing", kind: "choice", start: 1, options: [
      ["Open", { "--tracking-title": "0em" }],
      ["Tight", {}],
      ["Tighter", { "--tracking-title": "-0.04em" }],
    ] },
  ];

  const PRESETS = {
    Moin: { picks: { font: "Geist", code: "Geist Mono" } },
    Paper: { radius: "0.625rem", picks: { base: "Stone", font: "Geist", heading: "Serif", code: "Geist Mono", spacing: "Open" } },
    Night: { radius: "0.375rem", picks: { base: "Slate", accent: "Blue", chart: "Blue", font: "Inter", code: "Geist Mono", weight: "Bold" } },
    Square: { radius: "0rem", picks: { font: "Mono", code: "Geist Mono", spacing: "Open", weight: "Medium", style: "Compact" } },
    ...Object.fromEntries(THEMES.map(([name]) => [name, { picks: { palette: name, font: "Geist", code: "Geist Mono" } }])),
  };

  // A few lines to see what a theme does to code.
  const CODE_SAMPLE = `<span class="c1">// Deploy pushes an app to its server.</span>
<span class="kd">func</span> <span class="nf">Deploy</span>(name <span class="kt">string</span>) <span class="kt">error</span> {
  retries := <span class="mi">3</span>
  <span class="k">return</span> <span class="nf">push</span>(name, <span class="s">"main"</span>, retries)
}`;

  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const paint = canvas.getContext("2d", { willReadFrequently: true });

  function toHex(color) {
    paint.clearRect(0, 0, 1, 1);
    paint.fillStyle = color;
    paint.fillRect(0, 0, 1, 1);
    const [r, g, b] = paint.getImageData(0, 0, 1, 1).data;
    return `#${[r, g, b].map((n) => n.toString(16).padStart(2, "0")).join("")}`;
  }

  // The value a token has in one scheme, whatever wrote it.
  function valueOf(name, scheme) {
    const probe = document.createElement("i");
    probe.style.cssText = `position:absolute;color-scheme:${scheme};color:var(${name})`;
    document.body.append(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return toHex(color);
  }

  const load = () => {
    try {
      return JSON.parse(kit.read("kit-theme")) || {};
    } catch {
      return {};
    }
  };

  const title = (word) => word[0].toUpperCase() + word.slice(1);
  const startOf = (setting) => setting.options[setting.start || 0][0];
  const pickOf = (theme, setting) => theme.picks?.[setting.id] || startOf(setting);

  // The variables of everything picked.
  function varsOf(theme) {
    const vars = {};
    for (const setting of SETTINGS) {
      const option = setting.options.find(([text]) => text === pickOf(theme, setting));
      Object.assign(vars, option?.[1]);
    }
    return vars;
  }

  // A theme as a short code, to pass around.
  const encode = (theme) => btoa(JSON.stringify({ radius: theme.radius, picks: theme.picks, colors: theme.colors })).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");
  const decode = (code) => JSON.parse(atob(code.trim().replace(/-/g, "+").replace(/_/g, "/")));

  function selectHTML(setting, name = `pick-${setting.id}`) {
    const id = `kit-pick-${setting.id}`;
    return `<div class="kit-field"><label for="${id}">${setting.label}</label><div class="kit-select">
      <button type="button" id="${id}" popovertarget="${id}-list" aria-label="${setting.label}" style="anchor-name:--${id}"><span class="kit-truncate" data-select-label>${setting.options[0][0]}</span></button>
      <div popover id="${id}-list" role="radiogroup" aria-label="${setting.label}" style="position-anchor:--${id}">
        ${setting.options.map(([text]) => `<label class="kit-option"><input type="radio" name="${name}" value="${text}"><span>${text}</span></label>`).join("")}
      </div></div></div>`;
  }

  function choiceHTML(setting) {
    return `<div class="kit-field"><span>${setting.label}</span><div class="kit-choice" role="radiogroup" aria-label="${setting.label}">
      ${setting.options.map(([text]) => `<label><input type="radio" name="pick-${setting.id}" value="${text}">${text}</label>`).join("")}
    </div></div>`;
  }

  class ThemeEditor extends HTMLElement {
    connectedCallback() {
      if (this.ready) return;
      this.ready = true;
      this.className = "kit-stack";
      this.dataset.gap = "6";
      this.innerHTML = `
        <div class="kit-grid">
          ${selectHTML({ id: "preset", label: "Theme", options: Object.keys(PRESETS).map((name) => [name]) }, "kit-preset")}
          <div class="kit-field"><span>Mode</span><div class="kit-choice" role="radiogroup" aria-label="Mode">
            ${["system", "light", "dark"].map((mode) => `<label><input type="radio" name="kit-mode" value="${mode}" data-mode-set>${title(mode)}</label>`).join("")}
          </div></div>
        </div>
        <div class="kit-field">
          <label for="kit-radius">Radius <span class="kit-muted kit-digits" data-radius></span></label>
          <input id="kit-radius" class="kit-range" type="range" min="0" max="1" step="0.025">
        </div>
        <div class="kit-grid">${SETTINGS.filter((s) => s.kind === "select").map(selectHTML).join("")}</div>
        <div class="kit-grid">${SETTINGS.filter((s) => s.kind === "choice").map(choiceHTML).join("")}</div>
        <div class="demo-colors">
          <span class="kit-small kit-muted">Color</span><span class="kit-small kit-center kit-muted">Light</span><span class="kit-small kit-center kit-muted">Dark</span>
          ${COLORS.map(([name, label]) => `<span class="kit-text">${label}</span><input type="color" aria-label="${label}, light" data-token="${name}" data-side="0"><input type="color" aria-label="${label}, dark" data-token="${name}" data-side="1">`).join("")}
        </div>
        <div class="kit-field">
          <span>Preview</span>
          <div class="kit-code"><pre tabindex="0">${CODE_SAMPLE}</pre></div>
        </div>
        <div class="kit-field">
          <label for="kit-theme-out">Your theme</label>
          <textarea id="kit-theme-out" class="kit-textarea" readonly spellcheck="false"></textarea>
        </div>
        <div class="kit-field">
          <label for="kit-theme-code">Theme code</label>
          <div class="kit-row" data-gap="2" data-phone="stack">
            <input id="kit-theme-code" class="kit-input kit-mono" autocomplete="off" spellcheck="false">
            <button type="button" class="kit-button" data-apply>Apply</button>
          </div>
          <p class="kit-hint" data-invalid hidden>That is not a theme code.</p>
        </div>
        <div class="kit-row" data-end data-wrap data-gap="2">
          <button type="button" class="kit-button" data-shuffle>Shuffle</button>
          <button type="button" class="kit-button" data-reset>Reset</button>
          <button type="button" class="kit-button" data-kind="primary" data-copy data-copy-target="#kit-theme-out">Copy theme</button>
        </div>`;

      this.addEventListener("input", (event) => {
        const input = event.target;
        if (input.id === "kit-radius") {
          const theme = load();
          theme.radius = `${input.value}rem`;
          this.save(theme);
        } else if (input.dataset.token) {
          const theme = load();
          theme.colors ||= {};
          const name = input.dataset.token;
          const colors = theme.colors[name] || [valueOf(name, "light"), valueOf(name, "dark")];
          colors[Number(input.dataset.side)] = input.value;
          theme.colors[name] = colors;
          this.save(theme, true);
        }
      });
      this.addEventListener("change", (event) => {
        const input = event.target;
        if (input.name === "kit-preset") {
          this.save(structuredClone(PRESETS[input.value]));
        } else if (input.name?.startsWith("pick-")) {
          const theme = load();
          theme.picks ||= {};
          const setting = SETTINGS.find((one) => one.id === input.name.slice(5));
          theme.picks[setting.id] = input.value;
          // A new pick wins over a color set by hand.
          const option = setting.options.find(([text]) => text === input.value);
          for (const name of Object.keys(option[1])) delete theme.colors?.[name];
          this.save(theme);
        }
      });
      this.querySelector("[data-reset]").addEventListener("click", () => this.save({}));
      this.querySelector("[data-shuffle]").addEventListener("click", () => {
        const any = (list) => list[Math.floor(Math.random() * list.length)];
        const picks = Object.fromEntries(SETTINGS.filter((s) => s.id !== "size").map((s) => [s.id, any(s.options)[0]]));
        this.save({ radius: `${any([0, 0.25, 0.375, 0.5, 0.625, 0.75, 1])}rem`, picks });
      });
      this.querySelector("[data-apply]").addEventListener("click", () => {
        const field = this.querySelector("#kit-theme-code");
        const hint = field.closest(".kit-field").querySelector(".kit-hint");
        try {
          const theme = decode(field.value);
          if (typeof theme !== "object" || theme === null) throw new Error("not a theme");
          field.removeAttribute("aria-invalid");
          hint.hidden = true;
          this.save(theme);
        } catch {
          field.setAttribute("aria-invalid", "true");
          hint.hidden = false;
        }
      });
      // start="moin": the preset to begin with when nothing is saved yet.
      const start = this.getAttribute("start");
      if (start && !kit.read("kit-theme")) this.save(structuredClone(PRESETS[start]));
      kit.applyMode();
      this.refresh();
    }

    save(theme, keepInputs) {
      theme.vars = varsOf(theme);
      kit.write("kit-theme", JSON.stringify(theme));
      kit.applyTheme();
      this.refresh(keepInputs);
    }

    refresh(keepInputs) {
      const theme = load();
      const codeField = this.querySelector("#kit-theme-code");
      codeField.removeAttribute("aria-invalid");
      codeField.closest(".kit-field").querySelector(".kit-hint").hidden = true;
      const radius = theme.radius || getComputedStyle(document.documentElement).getPropertyValue("--radius").trim();
      this.querySelector("#kit-radius").value = parseFloat(radius);
      this.querySelector("[data-radius]").textContent = radius;
      for (const setting of SETTINGS) {
        const value = pickOf(theme, setting);
        this.querySelectorAll(`input[name="pick-${setting.id}"]`).forEach((input) => {
          input.checked = input.value === value;
          if (input.checked) {
            const select = input.closest(".kit-select");
            if (select) select.querySelector("[data-select-label]").textContent = value;
          }
        });
      }
      if (!keepInputs) {
        this.querySelectorAll("[data-token]").forEach((input) => {
          input.value = valueOf(input.dataset.token, input.dataset.side === "0" ? "light" : "dark");
        });
        const shape = (one) => JSON.stringify({ radius: one.radius, picks: one.picks || {} });
        const preset = theme.colors && Object.keys(theme.colors).length ? null : Object.entries(PRESETS).find(([, one]) => shape(one) === shape(theme));
        this.querySelectorAll("[name=kit-preset]").forEach((input) => {
          input.checked = preset ? input.value === preset[0] : false;
        });
        this.querySelector("#kit-pick-preset [data-select-label]").textContent = preset ? preset[0] : "Custom";
      }
      const colorNames = new Set(COLORS.map(([name]) => name));
      const lines = [`  --radius: ${radius};`];
      for (const [name, value] of Object.entries(varsOf(theme))) {
        if (!colorNames.has(name)) lines.push(`  ${name}: ${value};`);
      }
      for (const [name] of COLORS) {
        lines.push(`  ${name}: light-dark(${valueOf(name, "light")}, ${valueOf(name, "dark")});`);
      }
      const fonts = fontLines(varsOf(theme));
      this.querySelector("#kit-theme-out").value = `${fonts && `${fonts}\n`}:root {\n${lines.join("\n")}\n}`;
      this.querySelector("#kit-theme-code").value = encode(theme);
    }
  }
  customElements.define("kit-theme-editor", ThemeEditor);
})();
