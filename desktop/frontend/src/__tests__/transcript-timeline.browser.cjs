// Run with Playwright available: node src/__tests__/transcript-timeline.browser.cjs
// Bundles the real components in memory; no app server, backend, or file output.
const assert = require('node:assert/strict');
const path = require('node:path');
const { build } = require('esbuild');
const { chromium } = require('playwright');

const root = path.resolve(__dirname, '../..');
const fixture = `
  import React from 'react';
  import { createRoot } from 'react-dom/client';
  import { flushSync } from 'react-dom';
  import { Transcript } from './src/components/Transcript';
  import { LocaleProvider } from './src/lib/i18n';
  import { initialState, reducer } from './src/lib/useController';

  const root = createRoot(document.getElementById('root'));
  let state = initialState;
  let mode = 'compact';
  let session = 0;
  function render() {
    flushSync(() => root.render(
      <LocaleProvider>
        <Transcript key={session} items={state.items} live={state.live} running={state.running}
          processDisplayMode={mode} onPrompt={() => {}} questionNavigator={false}
          activityIndicatorEnabled={false} followButton={false} />
      </LocaleProvider>
    ));
  }
  window.timeline = {
    reset(items, running = true) {
      session++;
      mode = 'compact';
      state = { ...initialState, items, running, turnActive: running, currentTurnId: 'fixture-turn', turnStartAt: Date.now() };
      render();
    },
    dispatch(action) {
      const previous = state.items;
      state = reducer(state, action);
      render();
      return { sameItems: state.items === previous, live: state.live };
    },
    append(items) { state = { ...state, items: [...state.items, ...items] }; render(); },
    mode(next) { mode = next; render(); },
  };
`;

const tool = (id, name, status = 'done', extra = {}) => ({ kind: 'tool', id, name, status, readOnly: name === 'read', args: '{}', ...extra });
const reasoning = (id, text) => ({ kind: 'assistant', id, text: '', reasoning: text, streaming: false });
const user = { kind: 'user', id: 'user', turnId: 'fixture-turn', text: 'Run the checks' };
const base = [
  user,
  tool('read', 'read'),
  reasoning('r1', 'inspect the result'),
  { kind: 'phase', id: 'p1', text: 'Checking' },
  tool('task', 'task', 'running'),
  tool('child1', 'read', 'done', { parentId: 'task' }),
  tool('child2', 'bash', 'done', { parentId: 'task' }),
  tool('todo', 'todo_write'),
  tool('plan', 'exit_plan_mode'),
  reasoning('r2', 'verify once more'),
  { kind: 'phase', id: 'p2', text: 'Checking' },
  tool('bash', 'bash'),
];
const toolLabel = (n) => `\u8fd0\u884c\u4e86 ${n} \u4e2a\u5de5\u5177`;

(async () => {
  const bundle = await build({
    stdin: { contents: fixture, loader: 'tsx', resolveDir: root },
    tsconfig: path.join(root, 'tsconfig.json'),
    bundle: true,
    write: false,
    format: 'iife',
    platform: 'browser',
    loader: { '.css': 'empty', '.png': 'dataurl' },
    define: { 'process.env.NODE_ENV': '"development"' },
    logLevel: 'silent',
  });
  const executablePath = process.env.ORCA_BROWSER_EXECUTABLE || (process.platform === 'win32' ? 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' : undefined);
  const browser = await chromium.launch({ headless: true, executablePath });
  try {
    const page = await browser.newPage({ locale: 'zh-CN', viewport: { width: 1200, height: 900 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
    await page.setContent('<!doctype html><html><body><div id="root"></div></body></html>');
    await page.addScriptTag({ content: bundle.outputFiles[0].text });
    const labels = () => page.locator('.timeline-process-group__label').allTextContents();
    const head = page.locator('.timeline-process-group__head').first();
    const send = (e) => page.evaluate(e => window.timeline.dispatch({ type: 'event', e }), e);

    await page.evaluate(items => window.timeline.reset(items), base);
    assert.deepEqual(await labels(), [toolLabel(3)], 'hidden reasoning must not create adjacent process boxes');
    assert.equal(await page.locator('.reasoning__body').count(), 0, 'compact mode hides reasoning');
    assert.equal(await page.locator('.timeline-process-group__details > .tool').count(), 3, 'only top-level tools contribute to the group');
    assert.equal(await page.locator('.tool__nested .tool').count(), 2, 'subcalls remain under their parent exactly once');
    assert.equal(await page.locator('.phase').count(), 2, 'repeated phase labels are not deduplicated');
    assert.equal(await head.locator('.process-card__spin').count(), 1, 'running activity retains both its count and status');
    assert.equal(await head.locator('.process-card__spin').getAttribute('aria-label'), '\u6b63\u5728\u8fd0\u884c\u5de5\u5177');

    await page.evaluate(() => window.timeline.mode('detailed'));
    assert.deepEqual(await page.locator('.reasoning__body').allTextContents(), ['inspect the result', 'verify once more']);
    assert.deepEqual(await labels(), [toolLabel(3)], 'detailed mode does not split the chronological group');
    await head.click();
    await send({ kind: 'message', messageId: 'r3', text: '', reasoning: 'appended reasoning' });
    await send({ kind: 'tool_dispatch', tool: tool('read2', 'read', 'running') });
    assert.equal(await head.getAttribute('aria-expanded'), 'false', 'appending activity preserves the collapsed state');
    assert.deepEqual(await labels(), [toolLabel(4)], 'the count grows while the group is collapsed');
    assert.equal(await head.locator('.process-card__spin').getAttribute('aria-label'), '\u6b63\u5728\u8bfb\u53d6\u5185\u5bb9');
    await head.click();
    assert.deepEqual(await page.locator('.reasoning__body').allTextContents(), ['inspect the result', 'verify once more', 'appended reasoning']);
    await head.click();
    await page.evaluate(() => window.timeline.mode('compact'));
    assert.equal(await head.getAttribute('aria-expanded'), 'false', 'display mode changes preserve explicit expand state');
    console.log('PASS grouped counts, nested calls, repeated phases, detailed reasoning, stable expand state');

    await send({ kind: 'reasoning', messageId: 'stream', text: 'streamed reasoning' });
    const liveEntry = page.locator('[data-transcript-anchor="stream-text"]');
    assert.equal(await liveEntry.count(), 1, 'an empty live placeholder mounts outside the closed process group');
    assert.equal(await liveEntry.locator('.msg__body').count(), 0, 'reasoning-only compact stream has no visible text yet');
    await page.evaluate(() => { window.placeholderNode = document.querySelector('[data-transcript-anchor="stream-text"]'); });
    const firstDelta = await send({ kind: 'text', messageId: 'stream', text: 'First visible ' });
    assert.equal(firstDelta.sameItems, true, 'the regression must stream without rebuilding placeholder items');
    await liveEntry.locator('.msg__body').waitFor({ state: 'visible' });
    await send({ kind: 'text', messageId: 'stream', text: 'stage.' });
    await page.waitForFunction(() => document.querySelector('[data-transcript-anchor="stream-text"] .msg__body')?.textContent.includes('First visible stage.'));
    assert.equal(await page.evaluate(() => window.placeholderNode === document.querySelector('[data-transcript-anchor="stream-text"]')), true);
    assert.equal(await head.getAttribute('aria-expanded'), 'false', 'live text never forces the process group open');
    assert.deepEqual(await labels(), [toolLabel(4)], 'live text elsewhere cannot replace the tool count with Answering');
    assert.equal(await page.locator('.reasoning__body').count(), 0);
    await page.evaluate(() => window.timeline.mode('detailed'));
    assert.deepEqual(await liveEntry.locator('.reasoning__body').allTextContents(), ['streamed reasoning'], 'detailed streaming reasoning stays visible outside the closed group');
    await page.evaluate(() => window.timeline.mode('compact'));
    await send({ kind: 'message', messageId: 'stream', text: 'First visible stage.' });
    await send({ kind: 'tool_dispatch', tool: tool('later', 'bash', 'running') });
    assert.deepEqual(await labels(), [toolLabel(4), toolLabel(1)]);
    assert.deepEqual(await page.locator('.timeline-entry').evaluateAll(entries => entries.map(entry => entry.className)), [
      'timeline-entry timeline-entry--user', 'timeline-entry timeline-entry--process',
      'timeline-entry timeline-entry--assistant', 'timeline-entry timeline-entry--process',
    ], 'committing text leaves a real chronological boundary');
    await send({ kind: 'message', messageId: 'repeat', text: 'First visible stage.' });
    await send({ kind: 'tool_dispatch', tool: tool('last', 'bash', 'running') });
    assert.deepEqual(await labels(), [toolLabel(4), toolLabel(1), toolLabel(1)], 'repeated reply text still splits phases');
    await send({ kind: 'turn_done', outcome: 'cancelled' });
    assert.deepEqual(await labels(), [toolLabel(4), toolLabel(1), toolLabel(1)], 'cancellation keeps chronological groups');
    assert.equal(await head.getAttribute('aria-expanded'), 'false', 'cancellation preserves expand state');
    assert.equal(await page.locator('.timeline-entry--completed').count(), 0);
    assert.equal(await page.locator('.tool--stopped').count(), 2, 'cancelled tools in open groups retain their stopped status');
    console.log('PASS pre-commit text mount, context-only streaming, visible reply boundaries, cancellation');

    for (const outcome of ['failed', 'cancelled', 'interrupted']) {
      const items = [user, tool('failed', 'bash', 'error', { error: 'failed check' }), reasoning('r', 'retry the check'),
        tool('stopped', 'read', 'stopped'), { kind: 'turn_stats', id: 'stats', turnId: 'fixture-turn', success: false, outcome }];
      await page.evaluate(items => window.timeline.reset(items, false), items);
      assert.deepEqual(await labels(), [toolLabel(2)], `${outcome} retains one grouped diagnostic timeline`);
      assert.equal(await page.locator('.tool--error').count(), 1);
      assert.equal(await page.locator('.tool--stopped').count(), 1);
      assert.equal(await page.locator('.timeline-entry--completed').count(), 0);
    }
    console.log('PASS failure, cancellation, and interruption diagnostics');

    await page.evaluate(items => window.timeline.reset(items), [user, reasoning('only-reasoning', 'no tools yet')]);
    assert.deepEqual(await labels(), [], 'compact reasoning-only activity must not render an empty Thinking box');
    assert.equal(await page.locator('.timeline-entry--process').count(), 0, 'hidden reasoning must not leave an empty process entry');
    await page.evaluate(() => window.timeline.mode('detailed'));
    assert.deepEqual(await page.locator('.reasoning__body').allTextContents(), ['no tools yet']);
    await page.evaluate(() => window.timeline.mode('compact'));
    await send({ kind: 'tool_dispatch', tool: tool('new-tool', 'read', 'running') });
    assert.deepEqual(await labels(), [toolLabel(1)], 'reasoning joins subsequent tool activity without a second box');
    await head.click();
    await send({ kind: 'text', messageId: 'empty-stream', text: '' });
    assert.equal(await page.locator('[data-transcript-anchor="empty-stream-text"]').count(), 1, 'a fully empty live consumer still mounts');
    const emptyDelta = await send({ kind: 'text', messageId: 'empty-stream', text: 'Text without prior reasoning' });
    assert.equal(emptyDelta.sameItems, true);
    await page.waitForFunction(() => document.querySelector('[data-transcript-anchor="empty-stream-text"] .msg__body')?.textContent.includes('Text without prior reasoning'));
    assert.equal(await head.getAttribute('aria-expanded'), 'false');
    console.log('PASS hidden reasoning-only activity and empty-placeholder streaming');

    const completed = [
      user, tool('first', 'read'), reasoning('r1', 'first reasoning'),
      { kind: 'assistant', id: 'stage1', text: 'Stage reply', reasoning: '', streaming: false },
      tool('second', 'bash'), reasoning('r2', 'second reasoning'),
      { kind: 'assistant', id: 'stage2', text: 'Stage reply', reasoning: '', streaming: false },
      { kind: 'assistant', id: 'final', text: 'Final answer', reasoning: 'final reasoning', streaming: false, final: true },
      { kind: 'turn_stats', id: 'stats', turnId: 'fixture-turn', success: true, outcome: 'success' },
    ];
    await page.evaluate(items => window.timeline.reset(items, false), completed);
    const completedHead = page.locator('.turn-process-panel > button');
    assert.equal(await completedHead.getAttribute('aria-expanded'), 'false');
    assert.equal(await page.locator('.completed-turn__details').count(), 0);
    assert.equal(await page.locator('.timeline-entry--completed > .msg .msg__body').textContent(), 'Final answer');
    await completedHead.click();
    assert.deepEqual(await page.locator('.process-progress-row').allTextContents(), ['Stage reply', 'Stage reply']);
    assert.equal(await page.locator('.timeline-process-group').count(), 0, 'completed details stay flat');
    assert.equal(await page.locator('.reasoning__body').count(), 0);
    await page.evaluate(() => window.timeline.mode('detailed'));
    assert.equal(await completedHead.getAttribute('aria-expanded'), 'true');
    assert.deepEqual(await page.locator('.reasoning__body').allTextContents(), ['first reasoning', 'second reasoning', 'final reasoning']);
    await page.evaluate(() => window.timeline.append([
      { kind: 'user', id: 'next-user', turnId: 'next', text: 'Next question' },
      { kind: 'turn_stats', id: 'next-stats', turnId: 'next', success: false, outcome: 'cancelled' },
    ]));
    assert.equal(await completedHead.getAttribute('aria-expanded'), 'true', 'later turns cannot reset a completed turn expansion');
    assert.deepEqual(errors, [], 'browser must not report render errors or duplicate keys');
    console.log('PASS completed folding, flat ordered details, final answer, later-turn isolation');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
