// NODE_PATH=<bundled node_modules> node modern-polish.browser.cjs <local Vite URL> <evidence directory>
const fs = require('node:fs/promises');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const { createHash } = require('node:crypto');
const qa = require('./transcript-layout.browser.cjs');

const root = path.resolve(__dirname, '../../../..');
const viewportWidths = [760, 820, 1024, 1366, 1920];
const composerWidths = [720, 580, 460, 380, 320];
const baselineRef = 'b86e04f8';

function history(turns) {
  return Array.from({ length: turns }, (_, i) => [
    { role: 'user', turnId: `polish-${i}`, content: `Question ${i + 1}: inspect fixture section ${i + 1}.` },
    { role: 'assistant', turnId: `polish-${i}`, content: (`Fixture answer ${i + 1}. The transcript must retain the full chat pane.\n\n`).repeat(3) },
    { role: 'turn_stats', turnId: `polish-${i}`, content: '', outcome: 'success', elapsedMs: 1400, tokens: 200 },
  ]).flat();
}

// Injected into the existing mock module before qa's bootstrap mounts React.
// Only backend fixtures change; production components, props and CSS are served as-is.
function installFixture(mock, emitEvent, listenerCount, fixture) {
  // Vite may serve both a timestamped and bare bridge module after HMR. Share
  // fixture state and deliver events to each module's isolated listener set.
  const state = window.__modernPolish ||= { calls: [], running: false, emitters: [], subscriptions: [], deliveries: [], backend: [],
    effort: fixture.effort || { supported: true, current: 'auto', default: 'high', levels: ['auto', 'low', 'high', 'max'] } };
  state.emitters.push(emitEvent);
  state.subscriptions.push(listenerCount);
  const { calls } = state;
  const listTabs = mock.ListTabs.bind(mock);
  mock.ListTabs = async () => {
    const tabs = (await listTabs()).map(tab => tab.active ? { ...tab, scope: 'project', running: state.running } : tab);
    state.backend.push({ method: 'ListTabs', active: tabs.filter(tab => tab.active).map(({ id, running, paused }) => ({ id, running, paused })) });
    if (state.backend.length > 30) state.backend.shift();
    return tabs;
  };
  const metaForTab = mock.MetaForTab.bind(mock);
  mock.MetaForTab = async tabId => ({ ...await metaForTab(tabId), scope: 'project' });
  const settings = mock.Settings.bind(mock);
  mock.Settings = async () => {
    const result = await settings();
    // qa's bootstrap pins English with Object.assign. Keep this fixture's
    // requested language when that assignment runs, without editing the helper.
    Object.defineProperty(result, 'desktopLanguage', { enumerable: true,
      get: () => fixture.locale || 'en', set() {} });
    return result;
  };
  for (const method of ['PauseTab', 'ResumeTab', 'SwitchModelForTab']) {
    const original = mock[method];
    if (typeof original !== 'function') continue;
    mock[method] = async (...args) => {
      calls.push({ method, args });
      return original.apply(mock, args);
    };
  }
  mock.Effort = mock.EffortForTab = async () => structuredClone(state.effort);
  mock.SetEffortForTab = async (...args) => {
    calls.push({ method: 'SetEffortForTab', args });
    state.effort = { ...state.effort, current: args[1] };
  };
  mock.RequestCancelTab = async (...args) => {
    calls.push({ method: 'RequestCancelTab', args });
    // Keep cancellation pending until the test explicitly emits turn_done.
    return { accepted: true, turnId: 'polish-live' };
  };
  mock.ApproveTab = async (...args) => { calls.push({ method: 'ApproveTab', args }); };
  for (const method of ['Submit', 'SubmitToTab', 'SubmitDisplay', 'SubmitDisplayToTab']) {
    mock[method] = async (...args) => { calls.push({ method, args }); };
  }
}

async function fixturePage(browser, report, options, fixture = {}) {
  // Intercept the helper's fulfilled bridge response to extend its mock without
  // duplicating its bootstrap, network guard or exported testing bindings.
  const fixtureBrowser = { async newContext(settings) {
    const context = await browser.newContext({ ...settings, serviceWorkers: 'block' });
    return new Proxy(context, { get(target, key) {
      if (key === 'newPage') return async () => {
        const page = await target.newPage();
        // Install before app timers and timestamped controller state exist.
        // Moving Date.now backwards after mount mixes real and fixed clocks.
        if (options.style === 'classic') await page.clock.setFixedTime(new Date('2026-09-17T00:00:00Z'));
        return page;
      };
      if (key === 'route') return (pattern, handler, routeOptions) => target.route(pattern, route => {
        const bridge = new URL(route.request().url()).pathname === '/src/lib/bridge.ts';
        if (!bridge) return handler(route);
        return handler(new Proxy(route, { get(original, method) {
          if (method === 'fulfill') return response => original.fulfill({ ...response,
            body: `${response.body}\n(${installFixture.toString()})(__regressionMock, __regressionEmit, () => listeners.size, ${JSON.stringify(fixture)});\n` });
          const value = original[method];
          return typeof value === 'function' ? value.bind(original) : value;
        } }));
      }, routeOptions);
      const value = target[key];
      return typeof value === 'function' ? value.bind(target) : value;
    } });
  } };
  const result = await qa.appPage(fixtureBrowser, report, options);
  await waitForController(result.page);
  const identity = await result.page.evaluate(async () => {
    const active = (await window.go.main.App.ListTabs()).find(tab => tab.active);
    const meta = await window.go.main.App.MetaForTab(active.id);
    return { tabId: active.id, tabScope: active.scope, metaScope: meta.scope, locale: document.documentElement.lang.split('-')[0],
      intentLabel: document.querySelector('.composer-action-trigger')?.getAttribute('aria-label') };
  });
  report.cases.push({ scenario: 'fixture-identity', ...identity });
  qa.check(report, identity.tabScope === 'project' && identity.metaScope === 'project' && identity.locale === (fixture.locale || 'en'),
    'Fixture is an ordinary project conversation in the requested locale', { ...identity, requestedLocale: fixture.locale || 'en' });
  return result;
}

async function emit(page, ...events) {
  await waitForController(page);
  await page.evaluate(events => events.forEach(event => {
    if (event.kind === 'turn_started') window.__modernPolish.running = true;
    if (event.kind === 'turn_done') window.__modernPolish.running = false;
    window.__browserRegression.calls.events.push(structuredClone(event));
    window.__modernPolish.deliveries.push({ kind: event.kind, listeners: window.__modernPolish.subscriptions.map(count => count()) });
    for (const send of new Set(window.__modernPolish.emitters)) send({ ...event, tabId: window.__browserRegression.tabId });
  }), events);
  await qa.settle(page);
  if (events.some(event => event.kind === 'turn_started') && !events.some(event => event.kind === 'turn_done')) {
    await page.locator('.composer-runstatus__pause').waitFor({ state: 'visible' });
  }
}

async function waitForController(page) {
  await page.waitForFunction(() => window.__modernPolish?.subscriptions.some(count => count() > 0) &&
    window.__browserRegression?.calls.history > 0 &&
    document.querySelector('.transcript:not(.transcript--hydrating)') &&
    document.querySelector('.composer__input'));
}

async function runtimeSnapshot(page) {
  return page.evaluate(async () => {
    const fixture = window.__modernPolish;
    const active = (await window.go.main.App.ListTabs()).find(tab => tab.active);
    const meta = active && await window.go.main.App.MetaForTab(active.id);
    const pause = document.querySelector('.composer-runstatus__pause');
    return { fixtureRunning: fixture.running, active: active && { id: active.id, running: active.running, paused: active.paused },
      meta: meta && { ready: meta.ready, paused: meta.paused, scope: meta.scope },
      pause: pause && { label: pause.getAttribute('aria-label'), disabled: pause.disabled },
      primary: document.querySelector('.composer-runstatus__primary, .composer__btn--send')?.className,
      listeners: fixture.subscriptions.map(count => count()), calls: fixture.calls, deliveries: fixture.deliveries,
      backend: fixture.backend, historyLoads: window.__browserRegression.calls.history,
      transcriptTail: document.querySelector('.transcript')?.textContent.slice(-1200),
      focus: document.activeElement?.outerHTML.slice(0, 240) };
  });
}

async function checkClassicState(page, report, dimensions) {
  const runtime = await runtimeSnapshot(page);
  const running = dimensions.state !== 'idle';
  const expectedPaused = dimensions.state === 'paused';
  const valid = Boolean(runtime.pause) === running && runtime.active?.running === running &&
    Boolean(runtime.meta?.paused) === expectedPaused;
  qa.check(report, valid, 'Classic comparison exercises the requested runtime state', { ...dimensions, runtime });
  if (!valid) throw new Error(`Classic ${dimensions.state} fixture lost its runtime state: ${JSON.stringify(runtime)}`);
  return runtime;
}

async function measure(page) {
  return page.evaluate(() => {
    const rect = el => {
      if (!el) return null;
      const { left, right, top, bottom, width, height } = el.getBoundingClientRect();
      return { left, right, top, bottom, width, height, cx: (left + right) / 2, cy: (top + bottom) / 2 };
    };
    const shown = el => {
      if (!el || !el.getClientRects().length) return false;
      const css = getComputedStyle(el), r = rect(el);
      return css.display !== 'none' && css.visibility === 'visible' && Number(css.opacity) > 0 && r.width > 0 && r.height > 0;
    };
    const describe = el => {
      if (!el) return null;
      const r = rect(el), css = getComputedStyle(el);
      const hit = document.elementFromPoint(r.cx, r.cy);
      return { ...r, visible: shown(el), text: el.textContent.trim().slice(0, 160), label: el.getAttribute('aria-label'),
        disabled: Boolean(el.disabled), hit: el === hit || el.contains(hit), className: el.className,
        gridColumn: css.gridColumnStart, gridRow: css.gridRowStart, position: css.position };
    };
    const one = selector => describe(document.querySelector(selector));
    const card = document.querySelector('.composer-card');
    const status = document.querySelector('.composer-modern-status');
    const statusLabel = document.querySelector('.composer-modern-status__text');
    let statusText = null;
    if (shown(statusLabel)) {
      const range = document.createRange();
      range.selectNodeContents(statusLabel);
      const ink = range.getBoundingClientRect(), clip = rect(statusLabel), track = rect(status);
      const left = Math.max(ink.left, clip.left, track.left), right = Math.min(ink.right, clip.right, track.right);
      if (right > left) statusText = { ...describe(statusLabel), left, right, width: right - left, cx: (left + right) / 2 };
    }
    const actions = document.querySelector('.composer-card__actions--modern');
    const selectors = {
      intent: '.composer-action-trigger', access: '.composer-modern-access',
      effort: '.composer-modern-parameter--effort .effortsw__trigger',
      model: '.composer-modern-parameter--model .modelsw__trigger',
      pause: '.composer-runstatus__pause', primary: '.composer-runstatus__primary, .composer__btn--send',
    };
    const controls = Object.fromEntries(Object.entries(selectors).map(([name, selector]) => [name, one(selector)]));
    const overlaps = [];
    const visible = Object.entries(controls).filter(([, box]) => box?.visible);
    if (statusText) visible.push(['status', statusText]);
    for (let i = 0; i < visible.length; i++) for (let j = i + 1; j < visible.length; j++) {
      const [a, ra] = visible[i], [b, rb] = visible[j];
      if (Math.min(ra.right, rb.right) - Math.max(ra.left, rb.left) > 0.75 &&
        Math.min(ra.bottom, rb.bottom) - Math.max(ra.top, rb.top) > 0.75) overlaps.push([a, b]);
    }
    const actionOrder = actions ? [...actions.querySelectorAll('button')].map(child => {
      return Object.entries(selectors).find(([, selector]) => child.matches(selector))?.[0] || child.className;
    }) : [];
    const scroller = document.querySelector('.transcript');
    const railScroll = document.querySelector('.jump-scroll');
    return {
      main: one('.main'), shell: one('.transcript-shell'), transcript: one('.transcript'),
      composer: one('.composer-wrap'), card: describe(card), shelves: one('.footer-shelves'),
      meta: one('.composer-meta--modern'), actions: describe(actions), status: describe(status), statusText, controls, overlaps,
      locale: document.documentElement.lang.split('-')[0],
      statusDirectChild: Boolean(status && status.parentElement === card),
      statusOutsideActions: Boolean(status && actions && !actions.contains(status)),
      cardDisplay: getComputedStyle(card).display, columns: getComputedStyle(card).gridTemplateColumns,
      areas: getComputedStyle(card).gridTemplateAreas, actionOrder,
      shelvesEmpty: document.querySelector('.footer-shelves').childElementCount === 0,
      rail: one('.jump-bar'), railScroll: describe(railScroll), jumpCount: document.querySelectorAll('.jump-item').length,
      railOverflow: railScroll ? railScroll.scrollHeight - railScroll.clientHeight : 0,
      transcriptOverflowY: getComputedStyle(scroller).overflowY,
      horizontalOverflow: Math.max(document.documentElement.scrollWidth - innerWidth, scroller.scrollWidth - scroller.clientWidth),
      todo: one('.todobar'), queue: one('.queued-prompts'), approval: one('.footer-shelves .prompt-shelf'),
    };
  });
}

function assertLayout(report, m, dimensions) {
  const check = (ok, name, extra = {}) => qa.check(report, ok, name, { locale: m.locale, ...dimensions, ...extra });
  for (const name of ['composer', 'shelves']) {
    const box = m[name];
    if (name === 'shelves' && m.shelvesEmpty && !box.visible) continue;
    check(box && box.width > 0 && box.width <= 884.75 && Math.abs(box.cx - m.main.cx) <= 1,
      `Modern ${name} is centered in main and at most 884px`, { box, main: m.main });
  }
  check(Math.abs(m.transcript.left - m.main.left) <= 1 && Math.abs(m.transcript.right - m.main.right) <= 1 &&
    Math.abs(m.shell.width - m.main.width) <= 1 && m.transcriptOverflowY === 'auto',
  'Transcript keeps the full pane and its own scroll viewport', { transcript: m.transcript, main: m.main });
  check(m.horizontalOverflow <= 1, 'No document or transcript horizontal overflow', { overflow: m.horizontalOverflow });
  check(m.controls.effort?.visible, 'Modern effort stays visible in every state and width', { effort: m.controls.effort });
  check(m.controls.model?.visible && m.controls.intent?.visible && m.controls.access?.visible && m.controls.primary?.visible,
    'Bottom action controls stay visible');
  check(m.cardDisplay === 'grid' && m.columns.split(' ').length === 3 && m.statusDirectChild && m.statusOutsideActions &&
    ['2', 'status'].includes(m.status?.gridColumn) && ['3', 'actions'].includes(m.actions?.gridColumn),
  'Status owns the independent middle grid track', { columns: m.columns, areas: m.areas, status: m.status,
    statusDirectChild: m.statusDirectChild, statusOutsideActions: m.statusOutsideActions });
  const expectedOrder = dimensions.state === 'idle' ? ['effort', 'model', 'primary'] : ['effort', 'model', 'pause', 'primary'];
  check(JSON.stringify(m.actionOrder) === JSON.stringify(expectedOrder),
    'Right action controls are effort, model, pause when running, then primary', { actual: m.actionOrder, expected: expectedOrder });
  const row = Object.entries(m.controls).filter(([, control]) => control?.visible);
  if (m.statusText) row.push(['status', m.statusText]);
  const spread = Math.max(...row.map(([, c]) => c.cy)) - Math.min(...row.map(([, c]) => c.cy));
  check(spread <= 1.5, 'All bottom controls share a single center line', { spread, centers: row.map(([name, c]) => [name, c.cy]) });
  check(m.overlaps.length === 0, 'Bottom controls and center status do not overlap', { overlaps: m.overlaps });
  check(row.every(([, c]) => c.left >= m.card.left - 1 && c.right <= m.card.right + 1 && c.top >= m.card.top && c.bottom <= m.card.bottom + 1),
    'Bottom controls fit inside the composer card', { card: m.card, row });
  check(row.filter(([name, c]) => name !== 'status' && !c.disabled).every(([, c]) => c.hit),
    'Enabled control centers receive pointer input', { missed: row.filter(([name, c]) => name !== 'status' && !c.disabled && !c.hit) });
  const ordered = expectedOrder.map(name => m.controls[name]);
  check(ordered.every((c, i) => c?.visible && (!i || c.left >= ordered[i - 1].right - 0.75)),
    'Rendered right actions follow their keyboard order without overlap');
  if (dimensions.composerWidth) check(Math.abs(m.composer.width - dimensions.composerWidth) <= 1,
    'Composer fixture actually reached the requested width', { actual: m.composer.width });
  for (const name of ['queue', 'approval']) if (m[name]?.visible) {
    check(m[name].left >= m.shelves.left - 1 && m[name].right <= m.shelves.right + 1 && m[name].bottom <= m.composer.top + 1,
      `${name} stays in the centered shelf above the composer`, { box: m[name], shelves: m.shelves });
  }
}

async function constrainComposer(page, width) {
  await page.evaluate(width => {
    const composer = document.querySelector('.composer-wrap');
    if (width === null) composer.style.removeProperty('width');
    else composer.style.width = `${width}px`;
  }, width);
  await qa.settle(page);
}

async function matrix(page, report, output, state, locale) {
  await constrainComposer(page, null);
  for (const width of viewportWidths) {
    await page.setViewportSize({ width, height: 900 });
    for (const left of [false, true]) for (const right of [false, true]) {
      const effective = await qa.panels(page, 'modern', left, right);
      const dimensions = { scenario: 'viewport', state, locale, width, left, right, effective };
      qa.check(report, width < 1180 ? !effective.left && !effective.right : effective.left === left && effective.right === right,
        'Panel fixture follows the compact breakpoint', dimensions);
      const measured = await measure(page);
      report.cases.push({ ...dimensions, measured });
      assertLayout(report, measured, dimensions);
      if ((state === 'running' && (width === 760 || width === 1366 || width === 1920) && left && right) ||
        (state === 'draft' && width === 1366 && !left && !right)) {
        await qa.screenshot(page, report, output, `modern-${locale}-${state}-${width}-${left ? 'panels' : 'closed'}.png`);
      }
    }
  }
  await page.setViewportSize({ width: 1366, height: 900 });
  await qa.panels(page, 'modern', false, false);
  for (const composerWidth of composerWidths) {
    await constrainComposer(page, composerWidth);
    const dimensions = { scenario: 'composer-container', state, locale, width: 1366, composerWidth };
    const measured = await measure(page);
    report.cases.push({ ...dimensions, measured });
    assertLayout(report, measured, dimensions);
    if (composerWidth === 320 || (composerWidth === 720 && state === 'running')) {
      await qa.screenshot(page, report, output, `modern-${locale}-${state}-composer-${composerWidth}.png`);
    }
  }
  await constrainComposer(page, null);
  console.log(`Modern ${locale} ${state}: five viewports x four panel combinations, five composer widths`);
}

async function keyboardActivate(page, selector, key = 'Enter') {
  const control = page.locator(selector);
  const pause = selector === '.composer-runstatus__pause';
  const previous = pause ? await control.getAttribute('aria-label') : null;
  const callCount = pause ? await page.evaluate(() => window.__modernPolish.calls.filter(call => ['PauseTab', 'ResumeTab'].includes(call.method)).length) : 0;
  await control.focus();
  await page.keyboard.press(key);
  if (pause) await page.waitForFunction(({ previous, callCount }) => {
    const control = document.querySelector('.composer-runstatus__pause');
    const calls = window.__modernPolish.calls.filter(call => ['PauseTab', 'ResumeTab'].includes(call.method));
    return calls.length === callCount + 1 && control && control.getAttribute('aria-label') !== previous;
  }, { previous, callCount });
  await qa.settle(page);
}

async function jump(page, report, index, input) {
  const marker = page.locator('.jump-item').nth(index);
  await marker.scrollIntoViewIfNeeded();
  if (input === 'mouse') {
    const box = await marker.boundingBox();
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
  } else {
    await marker.focus();
    await page.keyboard.press(input);
  }
  await page.waitForFunction(index => {
    const transcript = document.querySelector('.transcript');
    const target = [...document.querySelectorAll('[id^="question-anchor-"]')].find(el => el.textContent.startsWith(`Question ${index + 1}:`));
    if (!target) return false;
    const delta = target.getBoundingClientRect().top - transcript.getBoundingClientRect().top;
    const expected = Math.min(transcript.scrollHeight - transcript.clientHeight, Math.max(0, transcript.scrollTop + delta - 12));
    return Math.abs(transcript.scrollTop - expected) <= 2;
  }, index, { timeout: 2500 }).catch(() => {});
  const result = await page.evaluate(index => {
    const transcript = document.querySelector('.transcript');
    const target = [...document.querySelectorAll('[id^="question-anchor-"]')].find(el => el.textContent.startsWith(`Question ${index + 1}:`));
    const delta = target ? target.getBoundingClientRect().top - transcript.getBoundingClientRect().top : null;
    const expected = target ? Math.min(transcript.scrollHeight - transcript.clientHeight, Math.max(0, transcript.scrollTop + delta - 12)) : null;
    return { delta, expected, scrollTop: transcript.scrollTop, targetText: target?.textContent,
      focused: document.activeElement === document.querySelectorAll('.jump-item')[index] };
  }, index);
  qa.check(report, result.expected !== null && Math.abs(result.scrollTop - result.expected) <= 2 && result.targetText.includes(`Question ${index + 1}:`),
    `Rail ${input} reaches its intended question`, { index, ...result });
  if (input !== 'mouse') qa.check(report, result.focused, 'Keyboard jump keeps focus on its marker', { index, input });
  report.cases.push({ scenario: 'jump', input, index, ...result });
}

async function railScenario(browser, report, url, output) {
  const sizes = [];
  for (const turns of [3, 48]) {
    const { page, context } = await fixturePage(browser, report, { url, history: history(turns) });
    try {
      await qa.panels(page, 'modern', false, false);
      await page.waitForTimeout(700); // Let the actual marker spring finish.
      const m = await measure(page);
      report.cases.push({ scenario: 'rail-size', turns, measured: m });
      qa.check(report, m.jumpCount === turns, 'History fixture produces every question marker', { turns, actual: m.jumpCount });
      qa.check(report, m.rail && Math.abs(m.rail.cy - m.shell.cy) <= 1 && Math.abs(m.railScroll.cy - m.shell.cy) <= 1,
        'Navigation rail and its content are vertically centered in transcript-shell', { turns, rail: m.rail, scroll: m.railScroll, shell: m.shell });
      qa.check(report, m.rail?.height > 0 && m.rail.height <= 240.75 && m.rail.height <= m.shell.height - 31,
        'Rail fits the shell and never exceeds 240px', { turns, rail: m.rail });
      sizes.push(m.rail?.height);
      if (turns === 3) qa.check(report, m.rail?.height < 160 && m.railOverflow <= 1,
        'Short history uses its content height without a fixed 240px track', { height: m.rail?.height, overflow: m.railOverflow });
      else qa.check(report, m.railOverflow > 0, 'Long history scrolls inside the capped rail', { overflow: m.railOverflow });
      for (const [input, index] of turns === 3 ? [['mouse', 0], ['Enter', 1], ['Space', 0]] : [['mouse', 12], ['Enter', 24], ['Space', 40]]) {
        await jump(page, report, index, input);
      }
      await qa.screenshot(page, report, output, `modern-rail-${turns}-questions.png`);
    } finally { await context.close(); }
  }
  qa.check(report, sizes[1] > sizes[0] + 20 && sizes[1] <= 240.75, 'Rail grows with history until its 240px cap', { sizes });
}

async function tabToMenu(page, menuSelector) {
  // Native tab navigation must reach a real option; never focus it by script.
  for (let i = 0; i < 40; i++) {
    if (await page.evaluate(selector => Boolean(document.activeElement?.closest(selector)), menuSelector)) return true;
    await page.keyboard.press('Tab');
  }
  return false;
}

async function menus(page, report, output, locale) {
  for (const [name, trigger, menu] of [
    ['effort', '.effortsw__trigger', '.effortsw__menu'],
    ['model', '.composer-modern-parameter--model .modelsw__trigger', '.modelsw__menu:not(.effortsw__menu)'],
    ['access', '.composer-modern-access', '.composer-approval-menu'],
    ['intent', '.composer-action-trigger', '.composer-intent-menu'],
  ]) {
    const visible = await page.locator(trigger).isVisible();
    qa.check(report, visible, `${name} menu trigger is visible`);
    if (!visible) continue;
    await keyboardActivate(page, trigger);
    const popup = page.locator(menu);
    await popup.waitFor({ state: 'visible' });
    const reached = await tabToMenu(page, menu);
    qa.check(report, reached, `${name} menu options are reachable with keyboard Tab`);
    const box = await popup.boundingBox();
    const viewport = page.viewportSize();
    qa.check(report, box && box.x >= 0 && box.y >= 0 && box.x + box.width <= viewport.width + 1 && box.y + box.height <= viewport.height + 1,
      `${name} menu stays inside the viewport`, { box, viewport });
    if (name === 'effort') {
      await page.keyboard.press('End');
      qa.check(report, await page.evaluate(() => document.activeElement?.textContent.trim() === 'max'), 'Effort End focuses the last option', { locale });
      await page.keyboard.press('Home');
      qa.check(report, await page.evaluate(() => document.activeElement?.textContent.trim() === 'auto'), 'Effort Home focuses the first option', { locale });
      await page.keyboard.press('ArrowDown');
      qa.check(report, await page.evaluate(() => document.activeElement?.textContent.trim() === 'low'), 'Effort ArrowDown advances focus', { locale });
      await page.keyboard.press('Enter');
      await qa.settle(page);
      const calls = await page.evaluate(() => window.__modernPolish.calls.filter(call => call.method === 'SetEffortForTab'));
      qa.check(report, calls.length === 1 && calls[0].args[1] === 'low' && (await page.locator(trigger).innerText()).includes('low'),
        'Keyboard effort selection updates the controller and visible value', { calls });
      qa.check(report, await page.locator(trigger).evaluate(el => el === document.activeElement),
        'Effort selection returns focus to its trigger');
    } else {
      await page.keyboard.press('Escape');
      await qa.settle(page);
    }
    qa.check(report, !(await popup.isVisible()) && await page.locator(trigger).getAttribute('aria-expanded') === 'false',
      `${name} menu closes and updates trigger semantics`);
    // After dismissal, Tab must still reach an enabled composer action.
    let focusRecovered = false;
    const tabBudget = Math.min(250, await page.locator('button, input, textarea, a[href], [tabindex]').count() + 2);
    for (let i = 0; i < tabBudget; i++) {
      focusRecovered = await page.evaluate(() => Boolean(document.activeElement?.closest('.composer-card')) && document.activeElement?.tagName === 'BUTTON');
      if (focusRecovered) break;
      await page.keyboard.press('Tab');
    }
    qa.check(report, focusRecovered, `${name} menu dismissal leaves keyboard controls reachable`);
  }
  await qa.screenshot(page, report, output, `modern-${locale}-menu-keyboard.png`);
}

async function modernStates(browser, report, url, output, locale) {
  const { page, context } = await fixturePage(browser, report, { url, history: history(6) }, { locale });
  try {
    await matrix(page, report, output, 'idle', locale);
    await emit(page, { kind: 'turn_started', turnId: 'polish-live' },
      { kind: 'text', turnId: 'polish-live', messageId: 'polish-response', text: 'Local fixture is running.' });
    qa.check(report, await page.locator('.composer-runstatus__pause').isVisible(), 'Running fixture exposes pause');
    await matrix(page, report, output, 'running', locale);
    await keyboardActivate(page, '.composer-runstatus__pause', 'Space');
    qa.check(report, /resume|\u7ee7\u7eed|\u6062\u590d/i.test(await page.locator('.composer-runstatus__pause').getAttribute('aria-label')),
      'Space pauses through the real controller and changes the button to Resume');
    await matrix(page, report, output, 'paused', locale);
    await keyboardActivate(page, '.composer-runstatus__pause', 'Enter');
    qa.check(report, /pause|\u6682\u505c/i.test(await page.locator('.composer-runstatus__pause').getAttribute('aria-label')),
      'Enter resumes through the real controller');
    await page.locator('.composer__input').fill('Keep this draft while the current local fixture runs.');
    await qa.settle(page);
    qa.check(report, await page.locator('.composer-runstatus__primary--send').isEnabled(), 'Running draft changes primary action to Send');
    await matrix(page, report, output, 'draft', locale);
    await keyboardActivate(page, '.composer-runstatus__primary--send');
    await page.locator('.queued-prompts').waitFor();
    qa.check(report, (await page.locator('.queued-prompts').innerText()).includes('Keep this draft') && await page.locator('.composer__input').inputValue() === '',
      'Keyboard Send queues a running draft and clears the composer');
    await emit(page,
      { kind: 'tool_dispatch', turnId: 'polish-live', tool: { id: 'polish-todos', name: 'todo_write', readOnly: false,
        args: JSON.stringify({ todos: [{ content: 'Inspect composer geometry', activeForm: 'Inspecting composer geometry', status: 'in_progress' },
          { content: 'Verify keyboard controls', status: 'pending' }] }) } },
      { kind: 'tool_result', turnId: 'polish-live', tool: { id: 'polish-todos', name: 'todo_write', readOnly: false, output: 'Saved local fixture tasks' } },
      { kind: 'approval_request', turnId: 'polish-live', approval: { id: 'polish-approval', tool: 'bash', subject: 'Inspect a synthetic fixture only' } });
    qa.check(report, await page.locator('.todobar').isVisible() && await page.locator('.footer-shelves .prompt-shelf').isVisible(),
      'Controller events render real Todo and approval UI alongside the queue');
    qa.check(report, await page.locator('.composer__input').isDisabled() && await page.locator('.effortsw__trigger').isVisible(),
      'Approval disables composing while preserving the Modern effort control');
    for (const width of viewportWidths) {
      await page.setViewportSize({ width, height: 900 });
      await qa.panels(page, 'modern', true, true);
      const measured = await measure(page), dimensions = { scenario: 'queue-todo-approval', state: 'running', locale, width };
      report.cases.push({ ...dimensions, measured });
      assertLayout(report, measured, dimensions);
      qa.check(report, measured.todo?.visible && measured.queue?.visible && measured.approval?.visible && measured.shell.height > 100,
        'All three auxiliary surfaces leave a usable transcript', dimensions);
      if (width === 760 || width === 820) await qa.screenshot(page, report, output, `modern-${locale}-queue-todo-approval-${width}.png`);
    }
    await qa.screenshot(page, report, output, `modern-${locale}-queue-todo-approval-1920.png`);
    // Approval business shortcuts are outside this layout regression. Dismiss
    // through its real Deny action before continuing the pause/stop workflow.
    await page.locator('.footer-shelves .prompt-shelf__actions button').last().click();
    await qa.settle(page);
    await page.locator('.queued-prompts__remove').click();
    await keyboardActivate(page, '.composer-runstatus__primary--stop', 'Enter');
    qa.check(report, await page.locator('.composer-runstatus__primary--stop').isDisabled() &&
      await page.locator('.composer-runstatus__primary--stop').getAttribute('aria-busy') === 'true',
    'Keyboard Stop holds a disabled busy cancellation state until acknowledgement completes');
    await matrix(page, report, output, 'cancel', locale);
    await emit(page, { kind: 'turn_done', turnId: 'polish-live', outcome: 'cancelled' });
    qa.check(report, !(await page.locator('.composer-runstatus__pause').count()) && await page.locator('.composer__btn--send').isDisabled(),
      'Cancelled turn returns to the idle Send action');
    await menus(page, report, output, locale);
    const calls = await page.evaluate(() => window.__modernPolish.calls);
    report.cases.push({ scenario: 'controller-calls', locale, calls });
    for (const method of ['PauseTab', 'ResumeTab', 'RequestCancelTab']) {
      qa.check(report, calls.filter(call => call.method === method).length === 1, `${method} is invoked exactly once by the keyboard workflow`, { calls });
    }
    qa.check(report, !calls.some(call => call.method.startsWith('Submit')), 'Queue workflow never submits a new backend turn');
  } finally { await context.close(); }
}

async function unknownEffort(browser, report, url, output, locale) {
  for (const effort of [
    { supported: false, current: '', default: '', levels: [] },
    { supported: true, current: 'auto', default: '', levels: [] },
  ]) {
    const { page, context } = await fixturePage(browser, report, { url, history: history(3) }, { effort, locale });
    try {
      await qa.panels(page, 'modern', false, false);
      for (const composerWidth of composerWidths) {
        await constrainComposer(page, composerWidth);
        const measured = await measure(page), dimensions = { scenario: 'unknown-effort', state: 'idle', locale, composerWidth, supported: effort.supported };
        report.cases.push({ ...dimensions, measured });
        assertLayout(report, measured, dimensions);
      }
      const trigger = page.locator('.effortsw__trigger');
      qa.check(report, await trigger.isVisible() && /model default|\u6a21\u578b\u9ed8\u8ba4/i.test(await trigger.innerText()),
        'Unknown effort displays Model default instead of disappearing', { effort });
      if (!(await trigger.isVisible())) continue;
      await keyboardActivate(page, '.effortsw__trigger');
      const menu = page.locator('.effortsw__menu');
      await menu.waitFor({ state: 'visible' });
      qa.check(report, await menu.getByRole('option').count() === 0, 'Unknown effort does not invent supported levels');
      const reached = await tabToMenu(page, '.effortsw__menu');
      qa.check(report, reached, 'Provider configuration action is keyboard reachable');
      const configure = menu.getByRole('button');
      qa.check(report, await configure.count() === 1, 'Unknown effort offers one provider settings action');
      if (await configure.count() === 1) {
        for (let i = 0; i < 10 && !(await configure.evaluate(el => el === document.activeElement)); i++) await page.keyboard.press('Tab');
        await page.keyboard.press('Enter');
        const settings = page.locator('.settings-modal');
        await settings.waitFor();
        qa.check(report, (await settings.locator('.settings-center__navitem--active span').innerText()) === (locale === 'zh' ? '\u6a21\u578b' : 'Models'),
          'Unknown effort configuration opens the Models and providers settings section', { locale });
        // The existing providers settings target opens Models; Access is its
        // provider subtab. Verify the real route through that existing UI.
        await settings.getByRole('button', { name: locale === 'zh' ? '\u63a5\u5165' : 'Access', exact: true }).click();
        await settings.locator('.provider-access-grid').waitFor();
        qa.check(report, await page.locator('.settings-modal .provider-access-grid').isVisible(),
          'Provider configuration is reachable from the unknown effort settings action', { locale });
        await qa.screenshot(page, report, output, `modern-${locale}-unknown-effort-${effort.supported ? 'empty' : 'unsupported'}-settings.png`);
      }
    } catch (error) {
      qa.check(report, false, 'Unknown effort workflow completes', { effort, locale, error: error.stack });
      await qa.screenshot(page, report, output, `modern-${locale}-unknown-effort-${effort.supported ? 'empty' : 'unsupported'}-failure.png`);
    } finally { await context.close(); }
  }
}

async function replaceCSS(page, css) {
  return page.evaluate(css => {
    const style = [...document.querySelectorAll('style[data-vite-dev-id]')].find(el =>
      el.dataset.viteDevId.replaceAll('\\', '/').endsWith('/src/styles.css'));
    if (!style) throw new Error('Vite stylesheet missing');
    const previous = style.textContent;
    style.textContent = css;
    return previous;
  }, css);
}

async function classicSnapshot(page) {
  return page.evaluate(() => {
    const selectors = ['.main', '.transcript-shell', '.transcript', '.jump-bar', '.jump-scroll', '.jump-item',
      '.footer-shelves', '.composer-wrap', '.composer-card', '.composer-meta', '.composer-meta__control',
      '.composer-card__actions', '.composer-runstatus', '.composer-runstatus__pause', '.composer-runstatus__primary',
      '.composer__btn--send', '.composer-card .modelsw__trigger', '.composer-action-trigger', '.composer-enhanced__button'];
    const properties = ['display', 'position', 'gridTemplateColumns', 'gridTemplateAreas', 'gridColumn', 'gridRow',
      'alignItems', 'justifyContent', 'gap', 'padding', 'margin', 'width', 'maxWidth', 'height', 'minWidth',
      'backgroundColor', 'color', 'fontSize', 'lineHeight', 'borderRadius', 'overflowX', 'overflowY'];
    return Object.fromEntries(selectors.map(selector => [selector, [...document.querySelectorAll(selector)].map(el => {
      const { x, y, width, height } = el.getBoundingClientRect(), css = getComputedStyle(el);
      return { rect: { x, y, width, height }, styles: Object.fromEntries(properties.map(key => [key, css[key]])) };
    })]));
  });
}

async function classicBaseline(browser, report, url, output, baselineCSS, locale) {
  const { page, context } = await fixturePage(browser, report, { url, style: 'classic', history: history(6) }, { locale });
  try {
    for (const state of ['idle', 'running', 'paused', 'draft', 'cancel']) {
      if (state === 'running') await emit(page, { kind: 'turn_started', turnId: 'polish-live' });
      if (state === 'paused') await keyboardActivate(page, '.composer-runstatus__pause');
      if (state === 'draft') {
        await keyboardActivate(page, '.composer-runstatus__pause');
        await page.locator('.composer__input').fill('Classic fixture draft');
      }
      if (state === 'cancel') {
        await page.locator('.composer__input').fill('');
        await keyboardActivate(page, '.composer-runstatus__primary--stop');
      }
      for (const width of [760, 1366, 1920]) {
        await page.setViewportSize({ width, height: 900 });
        for (const expanded of [false, true]) {
          await qa.panels(page, 'classic', expanded, expanded);
          await page.waitForTimeout(650);
          const dimensions = { state, locale, width, expanded };
          const runtime = await checkClassicState(page, report, dimensions);
          const current = await classicSnapshot(page);
          const currentCSS = await replaceCSS(page, baselineCSS);
          try {
            await page.waitForTimeout(650);
            await checkClassicState(page, report, dimensions);
            const baseline = await classicSnapshot(page);
            const differences = Object.keys(current).filter(key => JSON.stringify(current[key]) !== JSON.stringify(baseline[key]));
            report.cases.push({ scenario: 'classic-baseline', state, locale, width, expanded, runtime, current, baseline, differences });
            qa.check(report, differences.length === 0, `Classic geometry and computed styles match ${baselineRef}`, { state, locale, width, expanded, differences });
            if (state === 'running' && width === 1366 && expanded) await qa.screenshot(page, report, output, `classic-${locale}-b86e04f8-css-running.png`);
          } finally { await replaceCSS(page, currentCSS); }
          if (state === 'running' && width === 1366 && expanded) {
            await qa.settle(page);
            await qa.screenshot(page, report, output, `classic-${locale}-current-css-running.png`);
          }
        }
      }
    }
    qa.check(report, await page.locator('.composer-modern-status, .composer-card__actions--modern').count() === 0,
      'Classic keeps its original composer structure');
  } finally { await context.close(); }
  const unknown = await fixturePage(browser, report, { url, style: 'classic', history: history(3) },
    { effort: { supported: false, current: '', default: '', levels: [] }, locale });
  try {
    qa.check(report, await unknown.page.locator('.effortsw__trigger').count() === 0,
      'Classic unsupported effort retains its original hidden behavior');
  } finally { await unknown.context.close(); }
}

async function sourceFingerprint() {
  const files = ['styles.css', 'components/Composer.tsx', 'components/EffortSwitcher.tsx', 'App.tsx'];
  const hashes = {};
  for (const file of files) hashes[file] = createHash('sha256').update(await fs.readFile(path.join(root, 'desktop/frontend/src', file))).digest('hex');
  return hashes;
}

async function run() {
  const { url, output: configuredOutput } = qa.configuration();
  if (!process.argv[2]) url.port = '5287';
  const output = process.argv[3] ? configuredOutput : path.join(root, '.tmp/modern-polish-20260917');
  const report = qa.newReport('modern-polish');
  report.notCovered = ['Native backend/provider execution; all browser data and operations are mocked.',
    'Approval business shortcuts; approval is exercised as a layout fixture only.'];
  await fs.mkdir(output, { recursive: true });
  report.baselineCommit = execFileSync('git', ['rev-parse', baselineRef], { cwd: root, encoding: 'utf8' }).trim();
  const baselineCSS = execFileSync('git', ['show', `${baselineRef}:desktop/frontend/src/styles.css`], { cwd: root, encoding: 'utf8', maxBuffer: 5e6 });
  report.sourceAtStart = await sourceFingerprint();
  const browser = await qa.chromium.launch({ headless: true, executablePath: qa.executablePath });
  try {
    // Separate scenarios still run when an in-progress product change breaks one.
    for (const [name, scenario] of [
      ['rail', () => railScenario(browser, report, url, output)],
      ...['en', 'zh'].flatMap(locale => [
        [`modern-states-${locale}`, () => modernStates(browser, report, url, output, locale)],
        [`unknown-effort-${locale}`, () => unknownEffort(browser, report, url, output, locale)],
        [`classic-baseline-${locale}`, () => classicBaseline(browser, report, url, output, baselineCSS, locale)],
      ]),
    ]) {
      try { await scenario(); }
      catch (error) { qa.check(report, false, `${name} scenario completes`, { error: error.stack }); }
    }
  } finally {
    await browser.close();
    report.sourceAtEnd = await sourceFingerprint();
    qa.check(report, JSON.stringify(report.sourceAtStart) === JSON.stringify(report.sourceAtEnd),
      'Production files remained stable throughout this browser run', { start: report.sourceAtStart, end: report.sourceAtEnd });
    await qa.finish(report, output, 'modern-polish-results.json');
    const groups = new Map();
    for (const { name, ...details } of report.failures) {
      if (!groups.has(name)) groups.set(name, { name, count: 0, examples: [] });
      const group = groups.get(name);
      group.count++;
      if (group.examples.length < 2) group.examples.push(details);
    }
    await fs.writeFile(path.join(output, 'modern-polish-summary.json'), JSON.stringify({ suite: report.suite,
      cases: report.cases.length, assertions: report.assertions, failures: report.failures.length,
      screenshots: report.screenshots, failureGroups: [...groups.values()],
      pageErrors: report.pageErrors, consoleErrors: report.consoleErrors, externalRequests: report.externalRequests }, null, 2));
  }
}

module.exports = { fixturePage, emit, measure, classicBaseline };
if (require.main === module) run().catch(error => { console.error(error); process.exitCode = 1; });
