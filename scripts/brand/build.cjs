// The files of /brand: cloud/assets/brand, the mark, the wordmark and the
// lockup, black and white, SVG and PNG, made from cloud/assets/img/mark.svg
// and logo.svg. Run it again from the root of the repo when one of them
// changes, with Playwright and its Chromium (npx playwright install chromium):
//
//   npm install --no-save --prefix "$HOME/.cache/brand" playwright
//   NODE_PATH="$HOME/.cache/brand/node_modules" node scripts/brand/build.cjs
const fs = require('fs');
const { chromium } = require('playwright');

const img = 'cloud/assets/img', out = 'cloud/assets/brand';
const path = (file) => fs.readFileSync(`${img}/${file}`, 'utf8').match(/ d="([^"]+)"/)[1];
const mark = path('mark.svg'), word = path('logo.svg');

// The lockup is components.Wordmark with Mark: the mark as high as the i
// with its dot down to the bottom of the full stop, one o-counter before
// the m. In the wordmark's units.
const top = -753, height = 766, gap = 241, left = 58, right = 2487;
const drawings = {
  mark: ['0 0 24 24', `<path fill-rule="evenodd" d="${mark}"/>`],
  wordmark: [`${left} ${top} ${right - left} ${height}`, `<path d="${word}"/>`],
  lockup: [`0 ${top} ${height + gap + right - left} ${height}`,
    `<path fill-rule="evenodd" transform="translate(0 ${top}) scale(${(height / 24).toFixed(4)})" d="${mark}"/>` +
    `<path transform="translate(${height + gap - left} 0)" d="${word}"/>`],
};

(async () => {
  fs.mkdirSync(out, { recursive: true });
  const browser = await chromium.launch();
  for (const [name, [viewBox, inner]] of Object.entries(drawings)) {
    for (const [color, fill] of [['black', '#000'], ['white', '#fff']]) {
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}" fill="${fill}">${inner}</svg>\n`;
      fs.writeFileSync(`${out}/${name}-${color}.svg`, svg);
      // The PNG: 1024 high for the mark, 512 for the words, on nothing.
      const [, , w, h] = viewBox.split(' ').map(Number);
      const ph = name === 'mark' ? 1024 : 512, pw = Math.round((ph * w) / h);
      const page = await browser.newPage({ viewport: { width: pw, height: ph } });
      await page.setContent(`<style>html,body{margin:0;background:transparent}svg{display:block;width:${pw}px;height:${ph}px}</style>${svg}`);
      await page.screenshot({ path: `${out}/${name}-${color}.png`, omitBackground: true });
      await page.close();
    }
  }
  await browser.close();
})();
