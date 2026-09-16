// Run with Playwright available: node src/__tests__/classic-layout.browser.cjs <dev URL> <evidence directory>
const { chromium } = require('playwright');
const fs = require('node:fs/promises');
const path = require('node:path');

const url = new URL(process.argv[2]);
if (!['127.0.0.1', 'localhost'].includes(url.hostname)) throw new Error('Use an isolated local browser mock');
url.search = '?platform=windows&mock=demo';
const output = path.resolve(process.argv[3]);
const widths = [760, 900, 1024, 1180, 1181, 1366, 1920];
const official = 'DeepSeek V4.1 Flash';
const longModel = 'deepseek-flash-custom-provider-long-model-label-1234567890';
const report = { cases: [], assertions: 0, failures: [], console: [], pageErrors: [], requestFailures: [], screenshots: [] };
function check(ok, name, details) {
  report.assertions++;
  if (!ok) report.failures.push({ name, ...details });
}
async function screenshot(page, name) {
  await page.screenshot({ path: path.join(output, name), animations: 'disabled' });
  report.screenshots.push(name);
}

async function measure(page) {
  return page.evaluate(() => {
    const visible = el => el.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) && el.getBoundingClientRect().width > 0;
    const box = el => {
      const { left, right, top, bottom, width, height } = el.getBoundingClientRect();
      return { left, right, top, bottom, width, height };
    };
    const intersects = (a, b) => Math.min(a.right, b.right) - Math.max(a.left, b.left) > 1 && Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 1;
    const contains = (a, b) => b.left >= a.left - 1 && b.right <= a.right + 1 && b.top >= a.top - 1 && b.bottom <= a.bottom + 1;
    const header = document.querySelector('.app-chrome');
    const headerBox = box(header);
    const buttons = [...header.querySelectorAll('button,summary,.app-chrome__window-control')].filter(visible);
    const controls = buttons.map(el => ({ label: el.getAttribute('aria-label') || el.textContent.trim(), ...box(el) }));
    const headerCollisions = [];
    for (let i = 0; i < controls.length; i++) for (let j = i + 1; j < controls.length; j++) {
      if (intersects(controls[i], controls[j])) headerCollisions.push([controls[i].label, controls[j].label]);
    }
    const headerClipping = buttons.flatMap(el => {
      const range = document.createRange(); range.selectNodeContents(el);
      const text = range.getBoundingClientRect();
      return !contains(headerBox, box(el)) || (text.height && (text.top < headerBox.top - 1 || text.bottom > headerBox.bottom + 1)) ? [el.textContent.trim() || el.getAttribute('aria-label')] : [];
    });
    const truncatedHeaderLabels = [...header.querySelectorAll('.app-chrome__action span')].filter(visible).filter(el => el.scrollWidth > el.clientWidth + 1).map(el => el.textContent);
    const bar = document.querySelector('.statusbar'), barBox = box(bar);
    const texts = [];
    const walker = document.createTreeWalker(bar, NodeFilter.SHOW_TEXT);
    let source = 0;
    while (walker.nextNode()) {
      source++;
      const node = walker.currentNode, parent = node.parentElement;
      if (!node.textContent.trim() || !visible(parent)) continue;
      const range = document.createRange(); range.selectNode(node);
      for (const rect of range.getClientRects()) {
        let { left, right, top, bottom } = rect;
        for (let el = parent; el; el = el.parentElement) {
          const css = getComputedStyle(el), clip = box(el);
          if (['hidden', 'clip', 'auto', 'scroll'].includes(css.overflowX)) { left = Math.max(left, clip.left); right = Math.min(right, clip.right); }
          if (['hidden', 'clip', 'auto', 'scroll'].includes(css.overflowY)) { top = Math.max(top, clip.top); bottom = Math.min(bottom, clip.bottom); }
        }
        if (right > left && bottom > top) texts.push({ source, text: node.textContent.trim(), left, right, top, bottom });
      }
    }
    for (const el of bar.querySelectorAll('img')) if (visible(el)) texts.push({ source: ++source, text: el.alt, ...box(el) });
    const statusCollisions = [];
    for (let i = 0; i < texts.length; i++) for (let j = i + 1; j < texts.length; j++) {
      // An ellipsized text node may return overlapping range fragments.
      if (texts[i].source !== texts[j].source && intersects(texts[i], texts[j])) statusCollisions.push([texts[i].text, texts[j].text]);
    }
    const statusClipping = [...bar.querySelectorAll(':scope > .statusbar__group, :scope > img')].filter(visible)
      .filter(el => !contains(barBox, box(el)) || (!el.classList.contains('statusbar__group--model') && el.scrollWidth > el.clientWidth + 1)).map(el => el.className);
    const label = bar.querySelector('.statusbar__model');
    return { headerBox, headerClipping, headerCollisions, truncatedHeaderLabels, headerControls: controls, statusCollisions, statusClipping,
      label: { text: label.textContent, width: label.clientWidth, scrollWidth: label.scrollWidth, overflow: getComputedStyle(label).textOverflow },
      documentOverflow: document.documentElement.scrollWidth - innerWidth,
      baseline: { headerBackground: getComputedStyle(header).backgroundColor, headerFontSize: getComputedStyle(header).fontSize, statusFontSize: getComputedStyle(bar).fontSize },
    };
  });
}

(async () => {
  await fs.mkdir(output, { recursive: true });
  const executablePath = process.env.ORCA_BROWSER_EXECUTABLE || (process.platform === 'win32' ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : undefined);
  const browser = await chromium.launch({ headless: true, executablePath });
  try {
    for (const style of ['classic', 'modern']) for (const lang of ['zh', 'en']) {
      const context = await browser.newContext({ viewport: { width: 1366, height: 900 }, deviceScaleFactor: 1, locale: lang === 'zh' ? 'zh-CN' : 'en-US' });
      await context.addInitScript(({ style, lang }) => {
        localStorage.setItem('orca-ui-style', style);
        localStorage.setItem('orca-lang', lang);
        localStorage.setItem('orca-theme', 'light');
      }, { style, lang });
      const page = await context.newPage();
      page.on('console', msg => { if (['warning', 'warn', 'error'].includes(msg.type())) report.console.push({ style, lang, type: msg.type(), message: msg.text() }); });
      page.on('pageerror', error => report.pageErrors.push({ style, lang, message: error.message }));
      page.on('requestfailed', req => report.requestFailures.push({ style, lang, url: req.url(), error: req.failure()?.errorText }));
      await page.goto(url.href);
      await page.locator('.startup-splash').waitFor({ state: 'detached', timeout: 12000 });
      await page.locator('.statusbar__model').waitFor();
      await page.evaluate(async model => {
        const bridgeURL = performance.getEntriesByType('resource').map(entry => entry.name).find(url => new URL(url).pathname === '/src/lib/bridge.ts');
        const { app } = await import(bridgeURL);
        const settings = await app.Settings();
        await app.SaveProvider({ ...settings.providers[0], name: 'ui-fixture', builtIn: false, models: [model], default: model, baseUrl: 'https://ui-fixture.invalid', apiKeyEnv: 'UI_FIXTURE_ONLY', keySet: true });
      }, longModel);
      for (const model of [official, longModel]) {
        await page.locator('.modelsw:not(.effortsw) > .modelsw__trigger').filter({ visible: true }).click();
        await page.locator('.modelsw__menu:not(.effortsw__menu) [role="option"]').filter({ hasText: model }).click();
        await page.waitForFunction(model => document.querySelector('.statusbar__model')?.textContent === model, model);
        for (const width of widths) {
          await page.setViewportSize({ width, height: 900 });
          for (const sidebar of [false, true]) for (const workspace of [false, true]) {
            for (const [selector, expanded] of [[style === 'classic' ? '.app-chrome__panel-toggle--left' : '.modern-chrome__sidebar-toggle', sidebar], [style === 'classic' ? '.app-chrome__panel-toggle--right' : '.modern-chrome__workspace-actions button[aria-pressed]', workspace]]) {
              const toggle = page.locator(selector).last();
              if ((await toggle.getAttribute('aria-pressed') === 'true') !== expanded) await toggle.click();
            }
            await page.mouse.move(width / 2, 200);
            await page.waitForTimeout(220);
            const dimensions = { style, lang, width, sidebar, workspace, model };
            const geometry = await measure(page);
            report.cases.push({ ...dimensions, ...geometry });
            check(!geometry.headerClipping.length && !geometry.headerCollisions.length, 'Header controls stay within one rail without overlap', { ...dimensions, clipping: geometry.headerClipping, collisions: geometry.headerCollisions });
            check(!geometry.truncatedHeaderLabels.length, 'Visible header action labels stay readable', { ...dimensions, labels: geometry.truncatedHeaderLabels });
            check(!geometry.statusCollisions.length && !geometry.statusClipping.length, 'Status text and logo fit without overlap or clipping', { ...dimensions, clipping: geometry.statusClipping, collisions: geometry.statusCollisions });
            check(geometry.documentOverflow <= 1, 'Document has no horizontal overflow', dimensions);
            check(geometry.label.text === model && (model === official ? geometry.label.scrollWidth <= geometry.label.width + 1 : geometry.label.width >= 60 && geometry.label.overflow === 'ellipsis'), model === official ? 'Official status label is fully readable' : 'Long custom label truncates in a usable flexible region', { ...dimensions, label: geometry.label });
            if (sidebar && workspace) {
              await screenshot(page, `${style}-${lang}-${width}-${model === official ? 'official' : 'long'}.png`);
              const summary = page.locator('.statusbar__details > summary').filter({ visible: true });
              if (model === official && await summary.count()) {
                await summary.click();
                const metrics = page.locator('.statusbar__details[open] .statusbar__metrics');
                const bounds = await metrics.boundingBox();
                check(await metrics.locator('dd').count() === 4 && bounds.x >= -1 && bounds.x + bounds.width <= width + 1 && bounds.y >= 0, 'Compact metrics remain accessible in the existing details menu', dimensions);
                if ([760, 1024, 1920].includes(width)) await screenshot(page, `${style}-${lang}-${width}-details.png`);
                await page.keyboard.press('Escape');
              }
            }
          }
        }
      }
      await context.close();
      console.log(`Completed ${style}/${lang}: ${report.cases.length} cases, ${report.failures.length} failures`);
    }
  } finally {
    await browser.close();
    await fs.writeFile(path.join(output, 'results.json'), JSON.stringify(report, null, 2));
  }
  console.log(JSON.stringify({ cases: report.cases.length, assertions: report.assertions, failures: report.failures.length, screenshots: report.screenshots.length, console: report.console.length, pageErrors: report.pageErrors.length, requestFailures: report.requestFailures.length }));
  if (report.failures.length || report.pageErrors.length || report.requestFailures.length) process.exitCode = 1;
})().catch(error => { console.error(error); process.exitCode = 1; });
