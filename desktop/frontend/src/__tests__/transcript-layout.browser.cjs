// NODE_PATH=<bundled node_modules> node transcript-layout.browser.cjs <local Vite URL> <evidence directory>
const { chromium } = require('playwright');
const fs = require('node:fs/promises');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

const root = path.resolve(__dirname, '../../../..');
const widths = [760, 1024, 1280, 1366, 1920];
const executablePath = process.env.ORCA_BROWSER_EXECUTABLE || (process.platform === 'win32'
  ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : undefined);
function configuration() {
  const url = new URL(process.argv[2] || 'http://127.0.0.1:5276');
  if (!['127.0.0.1', 'localhost'].includes(url.hostname)) throw new Error('Only an isolated local Vite mock is allowed');
  url.search = '?platform=windows&mock=demo';
  return { url, output: path.resolve(process.argv[3] || path.join(root, '.tmp/release-v306/ui')) };
}
function newReport(suite) {
  return { suite, started: new Date().toISOString(), assertions: 0, cases: [], failures: [], negativeControls: [],
    pageErrors: [], consoleErrors: [], requestFailures: [], externalRequests: [], screenshots: [] };
}
function check(report, ok, name, details = {}) {
  report.assertions++;
  if (!ok) {
    report.failures.push({ name, ...details });
    console.error(`FAIL ${name}: ${JSON.stringify(details).slice(0, 1800)}`);
  }
}
async function settle(page) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  await page.waitForTimeout(180);
}

// Runs before main.tsx mounts App. The native backend is never contacted.
async function bootstrap(mock, emit, fixture) {
  if (window.go || window.runtime) throw new Error('Refusing to replace native desktop bindings');
  const calls = { settings: [], save: [], fetch: [], keys: [], events: [], history: 0 };
  const tabs = await mock.ListTabs();
  const tabId = tabs.find(tab => tab.active)?.id;
  const overrides = {
    async Settings() {
      const settings = await mock.Settings();
      Object.assign(settings, { desktopLanguage: 'en', desktopTheme: 'light', desktopUIStyle: fixture.style,
        processDisplayMode: fixture.detailed ? 'detailed' : 'compact', activityIndicatorEnabled: true, checkUpdates: false });
      if (fixture.providerKinds) settings.providerKinds = fixture.providerKinds;
      if (fixture.providers) settings.providers = [...settings.providers, ...fixture.providers];
      calls.settings.push({ providerKinds: settings.providerKinds });
      return settings;
    },
    async History() { calls.history++; return structuredClone(fixture.history || []); },
    async HistoryForTab() { calls.history++; return structuredClone(fixture.history || []); },
    async SaveProvider(provider) {
      calls.save.push(structuredClone(provider));
      await mock.SaveProvider(provider);
      if (fixture.providers) fixture.providers = fixture.providers.filter(p => p.name !== provider.name);
    },
    async FetchProviderModels(provider) {
      calls.fetch.push(structuredClone(provider));
      return [provider.kind === 'anthropic' ? 'claude-fixture' : 'gpt-fixture', 'model-fixture-two'];
    },
    async SetProviderKey(env) { calls.keys.push({ env }); },
  };
  window.go = { main: { App: new Proxy(mock, { get(target, prop) {
    const method = overrides[prop] || target[prop];
    return typeof method === 'function' ? method.bind(target) : method;
  } }) } };
  window.__browserRegression = { calls, tabId, emit(event) {
    calls.events.push(structuredClone(event));
    emit({ ...event, tabId });
  } };
}

async function appPage(browser, report, { url, style = 'modern', history = [], detailed = false, providerKinds,
  providers, oldProviderDefault = false, width = 1366 } = {}) {
  const context = await browser.newContext({ viewport: { width, height: 900 }, deviceScaleFactor: 1, locale: 'en-US' });
  await context.addInitScript(style => {
    localStorage.setItem('orca-ui-style', style);
    localStorage.setItem('orca-lang', 'en');
    localStorage.setItem('orca-theme', 'light');
  }, style);
  const fixture = { style, history, detailed, providerKinds, providers };
  await context.route('**/*', async route => {
    const requestURL = new URL(route.request().url());
    if (requestURL.origin !== url.origin) {
      report.externalRequests.push(requestURL.href);
      return route.abort('blockedbyclient');
    }
    if (requestURL.pathname === '/src/lib/bridge.ts') {
      const response = await route.fetch();
      return route.fulfill({ response, body: `${await response.text()}\nexport const __regressionMock = getMock();\nexport const __regressionEmit = emit;\n` });
    }
    if (requestURL.pathname === '/src/main.tsx') {
      const response = await route.fetch();
      const setup = `const {__regressionMock, __regressionEmit} = await import('/src/lib/bridge.ts');\nawait (${bootstrap.toString()})(__regressionMock, __regressionEmit, ${JSON.stringify(fixture)});\n`;
      return route.fulfill({ response, body: setup + await response.text() });
    }
    if (oldProviderDefault && requestURL.pathname === '/src/components/SettingsPanel.tsx') {
      const response = await route.fetch();
      const source = await response.text();
      const old = source.replace('useState(initial?.kind || "openai")', 'useState(initial?.kind ?? kinds[0] ?? "openai")');
      if (source === old) throw new Error('Provider negative-control mutation did not match the served module');
      return route.fulfill({ response, body: old });
    }
    return route.continue();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on('pageerror', error => report.pageErrors.push({ style, message: error.message }));
  page.on('console', message => { if (message.type() === 'error') report.consoleErrors.push({ style, message: message.text() }); });
  page.on('requestfailed', request => report.requestFailures.push({ url: request.url(), error: request.failure()?.errorText }));
  await page.goto(url.href);
  await page.locator('.startup-splash').waitFor({ state: 'detached', timeout: 20000 });
  await page.locator('.transcript').waitFor();
  await page.waitForFunction(() => document.documentElement.dataset.uiStyle === window.localStorage.getItem('orca-ui-style'));
  await page.evaluate(() => document.fonts.ready);
  await settle(page);
  return { page, context };
}
async function panels(page, style, left, right) {
  await settle(page);
  const selectors = style === 'classic'
    ? ['.app-chrome__panel-toggle--left', '.app-chrome__panel-toggle--right']
    : ['.modern-chrome__sidebar-toggle', '.modern-chrome__workspace-actions button[aria-pressed]'];
  for (const [index, expanded] of [left, right].entries()) {
    const button = page.locator(selectors[index]).last();
    if ((await button.getAttribute('aria-pressed') === 'true') !== expanded) await button.click();
  }
  await settle(page);
  return { left: await page.locator(selectors[0]).last().getAttribute('aria-pressed') === 'true',
    right: await page.locator(selectors[1]).last().getAttribute('aria-pressed') === 'true' };
}
function historyFixture(turns = 9) {
  const messages = [];
  const long = 'D:/fixture/' + 'long-tool-path-without-breaks/'.repeat(18) + 'target.ts';
  for (let i = 0; i < turns; i++) {
    const turnId = `history-${i}`;
    messages.push({ role: 'user', turnId, content: `Question ${i + 1}: inspect the long tool output and preserve the reading position.` });
    for (let j = 0; j < 3; j++) {
      if (j === 2) messages.push({ role: 'assistant', turnId, content: `Visible progress boundary ${i + 1}. This real response must remain between the second and third tool.` });
      messages.push({ role: 'assistant', turnId, content: '', reasoning: `Hidden reasoning ${i + 1}.${j + 1}: inspect only fixture content.`,
        toolCalls: [{ id: `shell-history-${i}-${j}`, name: 'bash', arguments: JSON.stringify({ command: `type ${long} --section=${i}-${j}` }) }] });
      messages.push({ role: 'tool', turnId, toolCallId: `shell-history-${i}-${j}`, toolName: 'bash',
        content: (i === 3 && j === 2 ? 'Error: fixture command failed\n' : 'fixture output\n') + long + '\n' + 'unbroken_output_'.repeat(120) });
    }
    messages.push({ role: 'turn_stats', turnId, content: '', outcome: i === 3 ? 'failed' : 'success', elapsedMs: 1400, tokens: 200 });
  }
  messages.push({ role: 'user', turnId: 'live-turn', content: 'Question 10: keep reading earlier history while the final tool is running.' });
  return messages;
}
async function events(page, sequence) {
  await page.evaluate(sequence => sequence.forEach(event => window.__browserRegression.emit(event)), sequence);
  await settle(page);
}
async function startRunning(page) {
  await events(page, [
    { kind: 'turn_started', turnId: 'live-turn' },
    { kind: 'tool_dispatch', turnId: 'live-turn', tool: { id: 'shell-live-first', name: 'bash', args: '{"command":"fixture first"}', readOnly: false } },
    { kind: 'tool_result', turnId: 'live-turn', tool: { id: 'shell-live-first', name: 'bash', output: 'first tool completed', readOnly: false } },
    { kind: 'message', turnId: 'live-turn', messageId: 'hidden-live', text: '', reasoning: 'Settled live reasoning between adjacent tools.' },
    { kind: 'tool_dispatch', turnId: 'live-turn', tool: { id: 'shell-live-second', name: 'bash', args: JSON.stringify({ command: 'fixture-long-command-'.repeat(45) }), readOnly: false } },
    { kind: 'tool_progress', turnId: 'live-turn', tool: { id: 'shell-live-second', name: 'bash', output: 'RUNNING_LONG_OUTPUT_' + '0123456789'.repeat(160), readOnly: false } },
  ]);
}
async function readingPosition(page) {
  await page.mouse.move(700, 120);
  await page.evaluate(() => {
    const scroller = document.querySelector('.transcript');
    const target = document.querySelectorAll('.timeline-process-group')[4];
    const rail = document.querySelector('.jump-scroll');
    scroller.scrollTop += target.getBoundingClientRect().top - (rail.getBoundingClientRect().top + 60);
  });
  const rect = await page.locator('.transcript').boundingBox();
  await page.mouse.move(rect.x + rect.width / 2, rect.y + rect.height / 2);
  await page.mouse.wheel(0, -24);
  await settle(page);
}
async function geometry(page) {
  return page.evaluate(() => {
    const box = element => {
      if (!element) return null;
      const { left, right, top, bottom, width, height } = element.getBoundingClientRect();
      return { left, right, top, bottom, width, height };
    };
    const overlap = (a, b) => a && b && Math.min(a.right, b.right) > Math.max(a.left, b.left) + 0.5 && Math.min(a.bottom, b.bottom) > Math.max(a.top, b.top) + 0.5;
    const clip = (rect, element) => {
      const result = { ...rect };
      for (let el = element; el; el = el.parentElement) {
        const css = getComputedStyle(el), bounds = box(el);
        if (['auto', 'scroll', 'hidden', 'clip'].includes(css.overflowX)) {
          result.left = Math.max(result.left, bounds.left); result.right = Math.min(result.right, bounds.right);
        }
        if (['auto', 'scroll', 'hidden', 'clip'].includes(css.overflowY)) {
          result.top = Math.max(result.top, bounds.top); result.bottom = Math.min(result.bottom, bounds.bottom);
        }
      }
      return result.right > result.left && result.bottom > result.top ? result : null;
    };
    const scroller = document.querySelector('.transcript'), content = document.querySelector('.transcript-content');
    const rail = document.querySelector('.jump-bar'), scroll = document.querySelector('.jump-scroll');
    const css = getComputedStyle(scroller);
    const hitboxes = [...document.querySelectorAll('.jump-item')].map(el => clip(box(el), el.parentElement)).filter(Boolean);
    const dots = [...document.querySelectorAll('.jump-dot')].map(el => clip(box(el), el.parentElement)).filter(Boolean);
    const cards = [...content.querySelectorAll('.timeline-process-group,.process-card,.turn-stats-row,.msg__body')]
      .map(el => ({ kind: el.className, ...clip(box(el), el.parentElement) })).filter(rect => rect.width && overlap(rect, box(scroller)));
    const textRects = [];
    const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const node = walker.currentNode;
      if (!node.textContent.trim()) continue;
      const range = document.createRange(); range.selectNodeContents(node);
      for (const fragment of range.getClientRects()) {
        const rect = clip({ left: fragment.left, right: fragment.right, top: fragment.top, bottom: fragment.bottom }, node.parentElement);
        if (rect) textRects.push({ ...rect, text: node.textContent.slice(0, 65) });
      }
    }
    const collisions = [];
    for (const [kind, rects] of [['hitbox', hitboxes], ['dot', dots], ['rail-hit-area', scroll ? [box(scroll)] : []]]) {
      for (const rect of rects) for (const contentRect of [...cards, ...textRects]) {
        if (overlap(rect, contentRect)) collisions.push({ kind, rail: rect, content: contentRect });
      }
    }
    const styleSnapshot = selector => [...document.querySelectorAll(selector)].slice(0, 4).map(el => {
      const s = getComputedStyle(el);
      return { rect: box(el), styles: Object.fromEntries(['paddingLeft', 'paddingRight', 'width', 'maxWidth', 'height', 'backgroundColor', 'color', 'fontSize', 'lineHeight', 'borderRadius', 'overflowY', 'justifyContent'].map(key => [key, s[key]])) };
    });
    return { shell: box(document.querySelector('.transcript-shell')), scroller: box(scroller), content: box(content), rail: box(rail),
      hitboxes, dots, collisions: collisions.slice(0, 20), contentRectsChecked: cards.length + textRects.length,
      padding: [parseFloat(css.paddingLeft), parseFloat(css.paddingRight)], overflowY: css.overflowY, scrollbar: css.scrollbarWidth,
      scrollTop: scroller.scrollTop, scrollHeight: scroller.scrollHeight, clientHeight: scroller.clientHeight,
      horizontalOverflow: Math.max(document.documentElement.scrollWidth - innerWidth, scroller.scrollWidth - scroller.clientWidth),
      welcome: box(document.querySelector('.welcome')), jumpCount: document.querySelectorAll('.jump-item').length,
      baseline: Object.fromEntries(['.transcript-shell', '.transcript', '.transcript-content', '.jump-bar', '.jump-item', '.jump-dot', '.timeline-process-group', '.process-card', '.welcome'].map(selector => [selector, styleSnapshot(selector)])) };
  });
}
async function replaceCSS(page, css) {
  return page.evaluate(css => {
    const style = [...document.querySelectorAll('style[data-vite-dev-id]')].find(el => el.dataset.viteDevId.replaceAll('\\', '/').endsWith('/src/styles.css'));
    if (!style) throw new Error('Vite production stylesheet not found');
    const previous = style.textContent;
    style.textContent = css;
    return previous;
  }, css);
}
async function screenshot(page, report, output, name) {
  await page.screenshot({ path: path.join(output, name), animations: 'disabled' });
  report.screenshots.push(name);
}
function checkGeometry(report, measured, dimensions) {
  check(report, measured.contentRectsChecked > 10, 'Measures real visible transcript text/cards', dimensions);
  check(report, measured.collisions.length === 0, 'Rail hitboxes and visible dots do not overlap text/cards', { ...dimensions, collisions: measured.collisions });
  check(report, Math.abs(measured.scroller.right - measured.shell.right) <= 1 && measured.overflowY === 'auto', 'Scroller remains at the far right of the chat canvas', { ...dimensions, scroller: measured.scroller, shell: measured.shell });
  check(report, measured.horizontalOverflow <= 1, 'Long tool lines do not overflow the transcript/document', { ...dimensions, overflow: measured.horizontalOverflow });
  check(report, measured.padding.every(value => value === 48), 'Navigation reserves symmetric 48px gutters', { ...dimensions, padding: measured.padding });
  check(report, measured.rail.width === 32 && measured.hitboxes.every(rect => rect.width === 20)
    && measured.dots.every(rect => rect.height === 2 && rect.width <= 20.1), 'Modern rail/item/dot dimensions are bounded', dimensions);
}
async function finish(report, output, filename) {
  report.finished = new Date().toISOString();
  for (const key of ['pageErrors', 'consoleErrors', 'requestFailures', 'externalRequests']) check(report, report[key].length === 0, `No ${key}`, { entries: report[key] });
  await fs.writeFile(path.join(output, filename), JSON.stringify(report, null, 2));
  console.log(JSON.stringify({ suite: report.suite, cases: report.cases.length, assertions: report.assertions,
    failures: report.failures.length, negativeControls: report.negativeControls.length, screenshots: report.screenshots.length }));
  if (report.failures.length) process.exitCode = 1;
}

module.exports = { chromium, executablePath, configuration, newReport, check, appPage, panels, settle, screenshot, finish };

async function assertJump(page, report, index, input) {
  const marker = page.locator('.jump-item').nth(index);
  if (input === 'mouse') {
    const rect = await marker.boundingBox();
    // The real rail delegates pointer input to .jump-scroll, not the button.
    await page.mouse.click(rect.x + rect.width / 2, rect.y + rect.height / 2);
  } else {
    await marker.focus();
    await page.keyboard.press(input);
  }
  await settle(page);
  const result = await page.evaluate(index => {
    const transcript = document.querySelector('.transcript');
    const user = document.querySelectorAll('[id^="question-anchor-"]')[index];
    return { delta: user.getBoundingClientRect().top - transcript.getBoundingClientRect().top,
      scrollTop: transcript.scrollTop, bottom: transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight };
  }, index);
  check(report, Math.abs(result.delta - 12) <= 2 || result.bottom <= 4, `Rail ${input} reaches the intended question`, { index, ...result });
  return result;
}

async function run() {
  const { url, output } = configuration();
  const report = newReport('transcript-layout');
  await fs.mkdir(output, { recursive: true });
  const baselineCSS = execFileSync('git', ['show', '520c355b:desktop/frontend/src/styles.css'], { cwd: root, encoding: 'utf8', maxBuffer: 5e6 });
  report.baselineCommit = execFileSync('git', ['rev-parse', '520c355b'], { cwd: root, encoding: 'utf8' }).trim();
  // Reuse the other agent's focused real-React fixture instead of duplicating it.
  report.timelineRegression = execFileSync(process.execPath, [path.join(__dirname, 'transcript-timeline.browser.cjs')], { cwd: root, encoding: 'utf8', timeout: 120000 });
  console.log(report.timelineRegression.trim());
  const browser = await chromium.launch({ headless: true, executablePath });
  try {
    const { page, context } = await appPage(browser, report, { url, history: historyFixture() });
    await startRunning(page);
    check(report, await page.locator('.tool--running').count() === 1, 'Real controller events render one running tool');
    check(report, await page.locator('.tool--error').count() === 1, 'History decoder restores the failed tool');
    check(report, await page.locator('.timeline-process-group').count() === 19, 'History and live hidden reasoning merge adjacent tools without merging real text boundaries');
    check(report, await page.locator('.reasoning__body').count() === 0, 'Compact history hides reasoning');
    await page.locator('.tool--error .process-card__head').click();
    await page.locator('.tool--error .code code').first().waitFor();
    check(report, (await page.locator('.tool--error .code').allTextContents()).some(text => text.length > 1000), 'Expanded real tool output contains the unbroken long-line fixture');

    for (const state of ['running', 'failed']) {
      if (state === 'failed') {
        await events(page, [
          { kind: 'tool_result', turnId: 'live-turn', tool: { id: 'shell-live-second', name: 'bash', output: 'Error: live fixture failed', err: 'fixture failure', readOnly: false } },
          { kind: 'turn_done', turnId: 'live-turn', outcome: 'failed' },
        ]);
        check(report, await page.locator('.tool--running').count() === 0 && await page.locator('.tool--error').count() === 2, 'Failed live turn retains both error diagnostics and stops its spinner');
      }
      for (const width of widths) {
        await page.setViewportSize({ width, height: 900 });
        for (const left of [false, true]) for (const right of [false, true]) {
          const effectivePanels = await panels(page, 'modern', left, right);
          await readingPosition(page);
          const dimensions = { style: 'modern', state, width, left, right, effectivePanels };
          check(report, width < 1180 ? !effectivePanels.left && !effectivePanels.right : effectivePanels.left === left && effectivePanels.right === right,
            'Requested panels render, or both collapse at the compact breakpoint', dimensions);
          const idle = await geometry(page);
          report.cases.push({ ...dimensions, interaction: 'reading', ...idle });
          checkGeometry(report, idle, dimensions);
          const marker = await page.locator('.jump-item').nth(4).boundingBox();
          await page.mouse.move(marker.x + marker.width / 2, marker.y + marker.height / 2);
          await page.locator('.jump-preview').waitFor();
          await page.waitForTimeout(450);
          const hovered = await geometry(page);
          report.cases.push({ ...dimensions, interaction: 'hover', ...hovered });
          checkGeometry(report, hovered, { ...dimensions, hover: true });
          check(report, Math.abs(hovered.scrollTop - idle.scrollTop) <= 1, 'Rail hover does not interrupt reading position', dimensions);
          check(report, (await page.locator('.jump-preview').textContent()).includes('Question 5:'), 'Hover preview names the intended question', dimensions);
          if (state === 'running' && width === 1280 && left && right) {
            await screenshot(page, report, output, 'modern-1280-panels-rail-hover.png');
            const currentCSS = await replaceCSS(page, baselineCSS);
            await settle(page);
            await readingPosition(page);
            const oldMarker = await page.locator('.jump-item').nth(4).boundingBox();
            await page.mouse.move(oldMarker.x + oldMarker.width / 2, oldMarker.y + oldMarker.height / 2);
            await page.waitForTimeout(450);
            const before = await geometry(page);
            report.negativeControls.push({ kind: 'baseline-css', width, left, right, ...before });
            check(report, before.collisions.length > 0, 'Negative control: baseline CSS reproduces rail/content overlap', { collisions: before.collisions });
            await screenshot(page, report, output, 'baseline-520c355b-1280-overlap.png');
            await replaceCSS(page, currentCSS);
            await settle(page);
          }
        }
        console.log(`Measured ${state} at ${width}px with all four panel combinations`);
      }
      if (state === 'running') {
        await page.setViewportSize({ width: 1280, height: 900 });
        await panels(page, 'modern', true, true);
        for (const [input, index] of [['mouse', 1], ['Enter', 3], ['Space', 4]]) {
          await readingPosition(page);
          const before = await assertJump(page, report, index, input);
          const questionsBefore = await page.locator('.jump-item').count();
          await events(page, [
            { kind: 'tool_progress', turnId: 'live-turn', tool: { id: 'shell-live-second', name: 'bash', output: '\nadditional live output', readOnly: false } },
            { kind: 'text', turnId: 'live-turn', messageId: 'same-turn-stream', text: `Visible delta after ${input}. ` },
          ]);
          const after = await geometry(page);
          const text = await page.locator('[data-transcript-anchor="same-turn-stream-text"]').textContent();
          check(report, text.includes(`Visible delta after ${input}.`) && await page.locator('.tool--running').count() === 1,
            'The existing running turn actually received and rendered the stream delta', { input, text });
          check(report, questionsBefore === await page.locator('.jump-item').count(), 'Stream test does not submit a new user question', { input, questionsBefore });
          check(report, Math.abs(before.scrollTop - after.scrollTop) <= 1 && after.scrollHeight - after.scrollTop - after.clientHeight > 200,
            `Same-turn streaming preserves reading after rail ${input}`, { input, before: before.scrollTop, after: after.scrollTop });
          check(report, await page.locator('.transcript-follow').isVisible(), 'Follow-latest stays available while reading an ongoing stream', { input });
          await page.locator('.transcript-follow').click();
          await settle(page);
          const followed = await geometry(page);
          check(report, followed.scrollHeight - followed.scrollTop - followed.clientHeight <= 4, 'Follow-latest explicitly resumes bottom following', { input });
          report.cases.push({ scenario: 'same-turn-live-navigation', input, before, after: { scrollTop: after.scrollTop, scrollHeight: after.scrollHeight, clientHeight: after.clientHeight }, followed: followed.scrollTop });
        }
      }
    }
    await context.close();

    for (const scenario of ['welcome', 'single-question']) {
      const { page, context } = await appPage(browser, report, { url, history: scenario === 'welcome' ? [] : [{ role: 'user', content: 'Only one question' }] });
      for (const width of widths) {
        await page.setViewportSize({ width, height: 900 });
        for (const left of [false, true]) for (const right of [false, true]) {
          await panels(page, 'modern', left, right);
          const measured = await geometry(page), dimensions = { scenario, width, left, right };
          report.cases.push({ ...dimensions, ...measured });
          check(report, measured.jumpCount === 0 && measured.padding.every(padding => padding === 32), 'No rail means no reserved navigation gutter', { ...dimensions, padding: measured.padding });
          check(report, Math.abs(measured.scroller.right - measured.shell.right) <= 1, 'No-rail scroller still spans the chat canvas', dimensions);
          if (scenario === 'welcome') {
            const x = measured.welcome && Math.abs((measured.welcome.left + measured.welcome.right - measured.scroller.left - measured.scroller.right) / 2);
            const y = measured.welcome && Math.abs((measured.welcome.top + measured.welcome.bottom - measured.scroller.top - measured.scroller.bottom) / 2);
            check(report, measured.welcome && x <= 1 && y <= 24, 'Welcome remains centered without a navigation gutter', { ...dimensions, x, y });
          }
        }
      }
      await context.close();
    }
    for (const width of [760, 1366]) {
      const { page, context } = await appPage(browser, report, { url, style: 'classic', width, history: historyFixture() });
      for (const expanded of [false, true]) {
        await panels(page, 'classic', expanded, expanded);
        await readingPosition(page);
        // Wait for the real dot spring (400ms plus per-marker delay), preserving
        // an exact style comparison instead of tolerating a geometry difference.
        await page.waitForTimeout(650);
        const current = await geometry(page);
        const currentCSS = await replaceCSS(page, baselineCSS);
        await page.waitForTimeout(650);
        const baseline = await geometry(page);
        report.cases.push({ style: 'classic', width, left: expanded, right: expanded, current: current.baseline, baseline: baseline.baseline });
        check(report, JSON.stringify(current.baseline) === JSON.stringify(baseline.baseline), 'Classic geometry and computed styles equal 520c355b CSS', { width, expanded });
        await replaceCSS(page, currentCSS);
        await settle(page);
      }
      await context.close();
    }
  } catch (error) {
    check(report, false, 'Browser run completed', { error: error.stack });
  } finally {
    await browser.close();
    await finish(report, output, 'transcript-layout-results.json');
  }
}
if (require.main === module) run().catch(error => { console.error(error); process.exitCode = 1; });
