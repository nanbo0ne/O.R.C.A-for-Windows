// Offline production-Composer fixture. Only synthetic files/events and a mock bridge.
// NODE_PATH=<bundled node_modules> node src/__tests__/composer-clipboard.browser.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { build } = require('esbuild');
const { chromium } = require('playwright');

const frontend = path.resolve(__dirname, '../..');
const output = path.join(frontend, '.tmp/composer-clipboard-evidence');
const fixtureSource = `
import React from 'react';
import {createRoot} from 'react-dom/client';
import {Composer} from './src/components/Composer';
import {Transcript} from './src/components/Transcript';
import {initialState, reducer} from './src/lib/useController';
import {LocaleProvider} from './src/lib/i18n';
import {ToastProvider} from './src/lib/toast';
import {uniqueClipboardFiles, clipboardImageFingerprint} from './src/lib/composerClipboard';
import './src/styles.css';
const root = createRoot(document.getElementById('root'));
const f = window.__composerFixture = {generation: 0, calls: [], saved: {}, nativePending: [], guideCalls: [], rejectGuide: false, attachmentRequests: []};
Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {
  read: async () => { throw new Error('Unexpected clipboard read: fixture only'); },
  readText: async () => { throw new Error('Unexpected clipboard readText: fixture only'); }
}});
f.image = async (name = 'diagram.png', type = 'image/png', color = '#000000') => {
  const canvas = document.createElement('canvas'); canvas.width = canvas.height = 12;
  const ctx = canvas.getContext('2d'); ctx.fillStyle = color; ctx.fillRect(0, 0, 12, 12);
  const blob = await new Promise(resolve => canvas.toBlob(resolve, type, 1));
  return new File([blob], name, {type, lastModified: 1});
};
f.bridge = async (name, args) => {
  f.calls.push(name);
  if (name === 'Commands' || name === 'Models' || name === 'ModelsForTab' || name === 'ReadClipboardFilePaths') return [];
  if (name === 'SavePastedImage' || name === 'SavePastedFile') {
    const p = '.orca/attachments/synthetic-' + Object.keys(f.saved).length + (name === 'SavePastedImage' ? '.png' : '.bin');
    f.saved[p] = args[name === 'SavePastedImage' ? 0 : 1]; return p;
  }
  if (name === 'AttachmentDataURL') {
    if (!Object.hasOwn(f.saved, args[0])) throw new Error('Non-synthetic attachment requested: ' + args[0]);
    f.attachmentRequests.push(args[0]); return f.saved[args[0]];
  }
  if (name === 'SaveClipboardImage') {
    const image = await f.image('native.png');
    const url = await new Promise(resolve => { const r = new FileReader(); r.onload = () => resolve(r.result); r.readAsDataURL(image); });
    const p = '.orca/attachments/native-' + f.calls.length + '.png'; f.saved[p] = url;
    if (f.delayNative) await new Promise(resolve => f.nativePending.push(resolve));
    return p;
  }
  throw new Error('Unexpected bridge call: ' + name);
};
f.key = key => document.querySelector('textarea').dispatchEvent(new KeyboardEvent('keydown', {key, ctrlKey: true, bubbles: true, cancelable: true}));
f.paste = (files, items = files, text = '') => {
  const event = new Event('paste', {bubbles: true, cancelable: true});
  Object.defineProperty(event, 'clipboardData', {value: {
    files, items: items.map(file => ({kind: 'file', type: file.type, getAsFile: () => file})),
    types: files.length ? ['Files'] : [], getData: () => text
  }});
  document.querySelector('textarea').dispatchEvent(event);
};
f.add = file => {
  const input = document.querySelector('input[type=file]'); const data = new DataTransfer(); data.items.add(file);
  input.files = data.files; input.dispatchEvent(new Event('change', {bubbles: true}));
};
f.uniqueClipboardFiles = uniqueClipboardFiles; f.fingerprint = clipboardImageFingerprint;
f.mount = (style, width = 800, running = false) => {
  f.generation++; f.calls = []; f.saved = {}; f.guideCalls = []; f.nativePending = []; f.delayNative = false; f.rejectGuide = false;
  document.documentElement.dataset.uiStyle = style;
  document.documentElement.dataset.theme = 'light';
  const noop = () => {};
  root.render(<LocaleProvider><ToastProvider><main style={{width: '100%', height: '100vh', display: 'flex', alignItems: 'flex-end', justifyContent: 'center', padding: 24}}>
    <section style={{width, maxWidth: '100%'}}><Composer key={f.generation} uiStyle={style} tabId={'synthetic-' + f.generation}
      running={running} turnStartAt={running ? Date.now() - 1000 : undefined} collaborationMode="default" toolApprovalMode="ask" askWorkflowEnabled={false} stepThinkingEnabled={false}
      promptMode="assistant" modelLabel="Synthetic offline" cwd="synthetic-workspace" ready
      onGuide={async (displayText, submitText) => { f.guideCalls.push({displayText, submitText}); if (f.rejectGuide) throw new Error('Synthetic guide rejected'); }}
      onSend={() => { throw new Error('Unexpected send in guide fixture'); }} onCancel={noop} onCycleMode={noop}
      onSetMode={noop} onSetCollaborationMode={noop} onSetToolApprovalMode={noop} onSetAskWorkflow={noop}
      onSetStepThinking={noop} onSetPromptMode={noop} onToggleYoloApprovalMode={noop} onSetGoal={noop}
      onClearGoal={noop} onSwitchModel={noop} onSetEffort={noop}/></section>
  </main></ToastProvider></LocaleProvider>);
};
f.renderGuidance = (style, state) => {
  document.documentElement.dataset.uiStyle = style;
  document.documentElement.dataset.theme = 'light';
  f.guidanceState = state;
  root.render(<LocaleProvider><ToastProvider><main style={{height: '100vh', display: 'flex', flexDirection: 'column', padding: 24}}>
    <Transcript items={state.items} running={false} onPrompt={() => {}} questionNavigator={false} activityIndicatorEnabled={false}/>
  </main></ToastProvider></LocaleProvider>);
};
f.consumeGuidance = (style, guide) => {
  f.attachmentRequests = [];
  const turnId = 'synthetic-guidance-turn';
  let state = reducer({...initialState, currentTurnId: turnId}, {type: 'history', messages: [
    {role: 'user', content: 'Start synthetic task', messageId: 'synthetic-start', turnId}
  ]});
  state = reducer(state, {type: 'steer_sent', text: guide.displayText, id: 'synthetic-item', turnId});
  state = reducer(state, {type: 'event', e: {kind: 'steer', text: guide.displayText, messageId: 'synthetic-saved', itemId: 'synthetic-item', turnId}});
  f.renderGuidance(style, state);
};
f.persistGuidance = style => {
  const messages = f.guidanceState.items.map(item => ({role: item.kind, content: item.text,
    messageId: item.messageId || item.id, itemId: item.itemId, turnId: item.turnId}));
  const stored = JSON.stringify({style, messages, saved: f.saved});
  sessionStorage.setItem('synthetic-guidance-reload', stored);
  return stored;
};
f.restoreGuidance = () => {
  const stored = JSON.parse(sessionStorage.getItem('synthetic-guidance-reload'));
  f.saved = stored.saved; f.attachmentRequests = [];
  f.renderGuidance(stored.style, reducer(initialState, {type: 'history', messages: stored.messages}));
};
`;

async function buildFixture() {
  const result = await build({
    stdin: { contents: fixtureSource, resolveDir: frontend, sourcefile: 'composer-synthetic-fixture.tsx', loader: 'tsx' },
    absWorkingDir: frontend, outfile: path.join(output, 'fixture.js'), bundle: true, write: false,
    format: 'iife', platform: 'browser', jsx: 'automatic', define: { 'process.env.NODE_ENV': '"development"' },
    loader: { '.woff': 'dataurl', '.woff2': 'dataurl', '.ttf': 'dataurl', '.png': 'dataurl' },
    plugins: [{ name: 'synthetic-bridge-only', setup(builder) {
      builder.onResolve({ filter: /(^|\/)bridge$/ }, () => ({ path: 'synthetic-bridge', namespace: 'fixture' }));
      builder.onLoad({ filter: /.*/, namespace: 'fixture' }, () => ({ contents:
        'export const app = new Proxy({}, {get: (_, name) => (...args) => window.__composerFixture.bridge(name, args)}); export const onFilesDropped = () => () => {}; export const onEvent = () => {throw new Error("Unexpected event subscription")}; export const onReady = onEvent; export const onRuntimeSwitchProgress = onEvent; export const onWelcomeSuggestions = onEvent; export const openExternal = () => {throw new Error("External navigation forbidden")};', loader: 'js' }));
    } }],
  });
  return new Map(result.outputFiles.map(file => ['/' + path.basename(file.path), file.text]));
}

async function fixturePage(browser, assets, report) {
  const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, locale: 'en-US', serviceWorkers: 'block' });
  await context.route('**/*', route => {
    const url = new URL(route.request().url());
    if (url.origin !== 'http://127.0.0.1:9876') { report.externalRequests.push(url.href); return route.abort(); }
    if (url.pathname === '/') return route.fulfill({ contentType: 'text/html', body: '<!doctype html><html><head><link rel="stylesheet" href="/fixture.css"></head><body><div id="root"></div><script src="/fixture.js"></script></body></html>' });
    const body = assets.get(url.pathname);
    if (body == null) { report.unexpectedRequests.push(url.pathname); return route.abort(); }
    return route.fulfill({ contentType: url.pathname.endsWith('.css') ? 'text/css' : 'application/javascript', body });
  });
  const page = await context.newPage();
  page.on('pageerror', error => report.pageErrors.push(error.message));
  await page.goto('http://127.0.0.1:9876');
  await page.waitForFunction(() => Boolean(window.__composerFixture));
  return { page, context };
}

async function mount(page, style, width = 800, running = false) {
  await page.evaluate(([s, w, r]) => window.__composerFixture.mount(s, w, r), [style, width, running]);
  await page.waitForFunction(() => Boolean(document.querySelector('textarea')));
  await page.waitForTimeout(60);
}
async function count(page, expected) {
  await page.waitForFunction(n => document.querySelectorAll('.composer-context__item').length === n
    && !document.querySelector('.composer-context__item--pending'), expected);
}

async function assertGuidanceImage(page) {
  await page.waitForFunction(() => {
    const images = document.querySelectorAll('.timeline-entry--steer .msg--user img');
    return images.length === 1 && images[0].complete && images[0].naturalWidth > 0;
  });
  const result = await page.evaluate(() => {
    const entry = document.querySelector('.timeline-entry--steer');
    const image = entry.querySelector('.msg--user img');
    return {
      count: document.querySelectorAll('.timeline-entry--steer').length,
      guidanceLabel: entry.querySelector('.steer__body')?.textContent,
      body: entry.querySelector('.msg__body')?.textContent,
      image: {alt: image.alt, width: image.naturalWidth, height: image.naturalHeight, dataURL: image.src.startsWith('data:image/png;')},
      attachmentRequests: window.__composerFixture.attachmentRequests,
      optimistic: window.__composerFixture.guidanceState.items.filter(item => item.kind === 'steer').some(item => item.optimistic),
    };
  });
  assert.equal(result.count, 1); assert.ok(result.guidanceLabel);
  assert.equal(result.body, 'Please use this diagram');
  assert.ok(!result.body.includes('<attachments') && !result.body.includes('<attachment') && !result.body.includes('@.orca/'));
  assert.deepEqual(result.image, {alt: 'guide-diagram.png', width: 12, height: 12, dataURL: true});
  assert.deepEqual(result.attachmentRequests, ['.orca/attachments/synthetic-0.png']);
  assert.equal(result.optimistic, false);
  return result;
}

async function run() {
  await fs.mkdir(output, { recursive: true });
  const report = { cases: [], negativeControls: [], pageErrors: [], externalRequests: [], unexpectedRequests: [], screenshots: [] };
  const assets = await buildFixture();
  const browser = await chromium.launch({ headless: true, executablePath: process.env.ORCA_BROWSER_EXECUTABLE || (process.platform === 'win32' ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : undefined) });
  try {
    for (const style of ['classic', 'modern']) {
      const { page, context } = await fixturePage(browser, assets, report);
      await mount(page, style);
      await page.evaluate(async () => {
        const f = window.__composerFixture; const a = await f.image();
        f.paste([a, new File([a], a.name, {type: a.type, lastModified: 2})], [a]);
      });
      await count(page, 1);
      await page.evaluate(async () => window.__composerFixture.paste([await window.__composerFixture.image()])); await count(page, 2);
      await page.evaluate(async () => window.__composerFixture.add(await window.__composerFixture.image())); await count(page, 3);
      report.cases.push({ style, case: 'duplicate FileList/items, deliberate repeat and explicit add', attachments: 3 });

      await mount(page, style);
      await page.evaluate(() => { const f = window.__composerFixture; f.delayNative = true; f.key('v'); });
      await page.waitForFunction(() => window.__composerFixture.nativePending.length === 1);
      await page.evaluate(async () => window.__composerFixture.paste([await window.__composerFixture.image()])); await count(page, 1);
      await page.evaluate(() => window.__composerFixture.nativePending.shift()()); await page.waitForTimeout(100); await count(page, 1);
      report.cases.push({ style, case: 'late browser event against pending native fallback', attachments: 1 });

      await mount(page, style);
      await page.evaluate(() => window.__composerFixture.key('v')); await count(page, 1);
      await page.evaluate(async () => window.__composerFixture.paste([await window.__composerFixture.image('image.jpg', 'image/jpeg')]));
      await page.waitForTimeout(100); await count(page, 1);
      await page.evaluate(() => window.__composerFixture.key('v')); await count(page, 2);
      report.cases.push({ style, case: 'completed native + delayed differently encoded event, then independent paste', attachments: 2 });

      await mount(page, style);
      await page.evaluate(() => window.__composerFixture.key('v')); await count(page, 1);
      await page.evaluate(async () => {
        const f = window.__composerFixture;
        f.paste([await f.image('a.png'), new File(['synthetic PDF'], 'b.pdf', {type: 'application/pdf'})]);
      }); await count(page, 2);
      assert.equal(await page.evaluate(() => window.__composerFixture.calls.filter(name => name === 'SavePastedFile').length), 1);
      report.cases.push({ style, case: 'late complete multi-format list replaces native preview', attachments: 2 });

      const pixels = await page.evaluate(async () => {
        const f = window.__composerFixture;
        const png = await f.image(); const jpeg = await f.image('image.jpg', 'image/jpeg'); const red = await f.image('red.png', 'image/png', '#ff0000');
        return { png: await f.fingerprint(png), jpeg: await f.fingerprint(jpeg), count: (await f.uniqueClipboardFiles([png, jpeg, red])).length };
      });
      assert.ok(pixels.png); assert.equal(pixels.png, pixels.jpeg); assert.equal(pixels.count, 2);
      report.cases.push({ style, case: 'real browser exact-pixel PNG/JPEG equivalence', ...pixels });

      await mount(page, style, 350);
      const trigger = page.locator(style === 'modern' ? '.composer-modern-access' : '.composer-approval-compact');
      await trigger.click();
      await page.locator('.composer-approval-menu').waitFor(); await page.waitForTimeout(180);
      const geometry = await page.evaluate(() => {
        const menu = document.querySelector('.composer-approval-menu');
        return { width: menu.getBoundingClientRect().width, rows: [...menu.querySelectorAll('[role=menuitemradio]')].map(el => el.getBoundingClientRect().height) };
      });
      assert.equal(geometry.width, 184); assert.deepEqual(geometry.rows, [34, 34, 34]);
      report.cases.push({ style, case: 'production permission popup geometry', ...geometry });
      const popupScreenshot = path.join(output, 'permission-' + style + '.png');
      await page.screenshot({ path: popupScreenshot }); report.screenshots.push(popupScreenshot);

      await mount(page, style, 800, true);
      await page.evaluate(async () => window.__composerFixture.paste([await window.__composerFixture.image('guide-diagram.png')])); await count(page, 1);
      await page.locator('textarea').fill('Please use this diagram');
      await page.evaluate(() => { window.__composerFixture.rejectGuide = true; window.__composerFixture.key('Enter'); });
      await page.waitForFunction(() => window.__composerFixture.guideCalls.length === 1);
      await page.getByText(/Synthetic guide rejected/).waitFor();
      assert.equal(await page.locator('textarea').inputValue(), 'Please use this diagram'); await count(page, 1);
      const guide = await page.evaluate(() => window.__composerFixture.guideCalls[0]);
      assert.match(guide.displayText, /@\[guide-diagram\.png\]\(\.orca\/attachments\/synthetic-0\.png\)/);
      assert.ok(!guide.displayText.includes('<attachments'));
      assert.match(guide.submitText, /<attachments count="1">/);
      assert.match(guide.submitText, /name="guide-diagram\.png" path="\.orca\/attachments\/synthetic-0\.png"/);
      assert.ok(guide.submitText.endsWith('@.orca/attachments/synthetic-0.png'));
      assert.ok(!guide.submitText.includes('@[guide-diagram.png]'));
      report.cases.push({ style, case: 'guide display/submit separation and draft retained on rejection', ...guide });
      const guideScreenshot = path.join(output, 'guide-rejected-' + style + '.png');
      await page.screenshot({ path: guideScreenshot }); report.screenshots.push(guideScreenshot);
      await page.evaluate(() => { window.__composerFixture.rejectGuide = false; window.__composerFixture.key('Enter'); });
      await page.waitForFunction(() => window.__composerFixture.guideCalls.length === 2 && document.querySelector('textarea').value === '');
      await count(page, 0);

      await page.evaluate(([s, g]) => window.__composerFixture.consumeGuidance(s, g), [style, guide]);
      report.cases.push({style, case: 'consumption event renders the real UserMessage image without manifest body', ...await assertGuidanceImage(page)});
      const consumedScreenshot = path.join(output, 'guidance-consumed-' + style + '.png');
      await page.screenshot({path: consumedScreenshot, animations: 'disabled'}); report.screenshots.push(consumedScreenshot);
      const stored = await page.evaluate(s => window.__composerFixture.persistGuidance(s), style);
      assert.ok(!stored.includes('<attachments'), 'persist only the consumed display text, not the provider manifest');
      assert.equal(JSON.parse(stored).messages.find(message => message.role === 'steer').content, guide.displayText);

      // Negative control: the same renderer exposes a leaked submit manifest;
      // the positive body assertion must catch display/submit routing regressions.
      await page.evaluate(([s, g]) => window.__composerFixture.consumeGuidance(s, {...g, displayText: g.submitText}), [style, guide]);
      await page.waitForFunction(() => document.querySelector('.timeline-entry--steer .msg__body')?.textContent.includes('<attachments'));
      await assert.rejects(() => assertGuidanceImage(page), {name: 'AssertionError'});
      report.negativeControls.push({style, case: 'provider manifest routed as display text is rejected'});

      await page.reload();
      await page.waitForFunction(() => Boolean(window.__composerFixture));
      await page.evaluate(() => window.__composerFixture.restoreGuidance());
      report.cases.push({style, case: 'persisted display survives page reload and re-fetches the real UserMessage image', ...await assertGuidanceImage(page)});
      const reloadedScreenshot = path.join(output, 'guidance-reloaded-' + style + '.png');
      await page.screenshot({path: reloadedScreenshot, animations: 'disabled'}); report.screenshots.push(reloadedScreenshot);
      await context.close();
    }
    assert.deepEqual(report.pageErrors, []); assert.deepEqual(report.externalRequests, []); assert.deepEqual(report.unexpectedRequests, []);
    report.passed = true;
  } catch (error) {
    report.error = error.stack; throw error;
  } finally {
    await browser.close(); await fs.writeFile(path.join(output, 'results.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify(report, null, 2));
  }
}
module.exports = { fixtureSource, buildFixture, fixturePage, mount, count };
if (require.main === module) run().catch(error => { console.error(error); process.exitCode = 1; });
