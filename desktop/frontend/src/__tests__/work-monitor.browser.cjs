// Offline, isolated browser fixture. No personal profile or native binding is used.
// NODE_PATH=<bundled node_modules> node work-monitor.browser.cjs http://127.0.0.1:5291 <output>
const fs = require('node:fs/promises');
const { chromium, executablePath, configuration, newReport, check, appPage, panels, settle, screenshot, finish } = require('./transcript-layout.browser.cjs');

async function installMonitorFixture(page) {
  await page.evaluate(() => {
    const target = window.go.main.App;
    const fixture = window.__monitorFixture = { subscriptions: 0, unsubscriptions: [], polls: 0, active: '', entries: [], seq: 0 };
    function add(kind, phase, data, id = '') {
      fixture.entries.push({ generation: fixture.active, seq: ++fixture.seq, tabId: window.__browserRegression.tabId, turnId: 'synthetic-turn', requestId: id, attemptId: kind === 'request' ? `${id}/attempt-1` : undefined, time: Date.now() - 8000 + fixture.seq * 20, kind, phase, data });
    }
    fixture.append = () => {
      for (let i = 0; i < 300; i++) add('response', '', { channel: 'decode', text: `synthetic fragment ${i}\n` });
    };
    const methods = {
      async MonitorSubscribe() {
        fixture.active = `generation-${++fixture.subscriptions}`; fixture.seq = 0; fixture.entries = [];
        add('phase', 'input');
        add('request', 'wait-first', { body: { model: 'offline-fixture', temperature: 0.5, messages: [{ role: 'system', content: 'synthetic system' }, { role: 'user', content: 'synthetic prompt '.repeat(100) }], tools: [{ name: 'fixture-tool', parameters: { type: 'object' } }] } }, 'synthetic-request');
        add('phase', 'reasoning'); add('response', '', { channel: 'reasoning', text: 'POST-HOOK synthetic reasoning\n'.repeat(30) });
        add('phase', 'tool'); add('tool', '', { id: 'tool-a', name: 'fixture', args: '{"test":true}', kind: 'tool_dispatch' });
        add('child', '', { id: 'child-a', running: true });
        add('phase', 'decode'); fixture.append();
        return { generation: fixture.active, entries: [], cursor: 0, bytes: 0, evicted: 0, dropped: 0, expired: false };
      },
      async MonitorSnapshot(generation, after) {
        fixture.polls++;
        const entries = fixture.entries.filter(e => e.seq > after && e.generation === generation);
        return { generation, entries, cursor: entries[entries.length - 1]?.seq || after, bytes: 1000, evicted: 0, dropped: 0, expired: generation !== fixture.active };
      },
      async MonitorUnsubscribe(generation) { fixture.unsubscriptions.push(generation); if (fixture.active === generation) fixture.active = ''; },
    };
    window.go.main.App = new Proxy(target, { get(t, p) { return methods[p] || t[p]; } });
    fixture.hidden = value => { Object.defineProperty(document, 'hidden', { configurable: true, get: () => value }); document.dispatchEvent(new Event('visibilitychange')); };
  });
}

async function run() {
  const { url, output } = configuration(); await fs.mkdir(output, { recursive: true });
  const report = newReport('work-monitor');
  const browser = await chromium.launch({ executablePath, headless: true });
  try {
    for (const style of ['modern', 'classic']) {
      const { page, context } = await appPage(browser, report, { url, style, width: 1366 });
      await installMonitorFixture(page); await panels(page, style, true, false);
      await page.getByRole('button', { name: 'Work monitor', exact: true }).click();
      await page.locator('.work-monitor__entry').first().waitFor();
      await settle(page);
      for (const [width, height] of [[1366, 900], [1280, 720], [1024, 768], [760, 700]]) {
        await page.setViewportSize({ width, height }); await settle(page);
        if (await page.locator('.sidebar--collapsed').count()) {
          check(report, await page.locator('.work-monitor').count() === 0 && await page.evaluate(() => window.__monitorFixture.active === ''), 'Responsive collapse unmounts and unsubscribes', { style, width });
          continue;
        }
        const geometry = await page.evaluate(() => {
          const el = s => document.querySelector(s), box = s => { const r = el(s).getBoundingClientRect(); return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, width: r.width, height: r.height }; };
          return { monitor: box('.work-monitor'), sidebar: box('.sidebar'), projects: box('.sidebar__section--projects'), nav: box('.sidebar__nav'), ribbon: box('.phase-drum__window'),
            overflow: el('.work-monitor__content').scrollWidth - el('.work-monitor__content').clientWidth,
            font: getComputedStyle(el('.work-monitor pre')).fontSize,
            colors: [...new Set([...document.querySelectorAll('.phase-drum__face')].map(e => getComputedStyle(e).backgroundColor))],
            nodes: document.querySelectorAll('.work-monitor__entry').length };
        });
        const detail = { style, width, height, geometry }; report.cases.push(detail);
        check(report, geometry.projects.bottom <= geometry.monitor.top + 1 && geometry.monitor.bottom <= geometry.nav.top + 1, 'Monitor between project list and bottom nav', detail);
        check(report, geometry.projects.height >= 79, 'Project list retains 80px', detail);
        check(report, geometry.nav.bottom <= geometry.sidebar.bottom + 1 && geometry.monitor.top >= geometry.sidebar.top, 'Monitor and navigation remain inside viewport', detail);
        check(report, geometry.ribbon.height === 6, 'Readonly ribbon is 6px', detail);
        check(report, geometry.monitor.left >= geometry.sidebar.left && geometry.monitor.right <= geometry.sidebar.right + 1, 'Same sidebar column', detail);
        check(report, geometry.overflow <= 1 && geometry.font === '12px', 'Raw text readable and no horizontal overflow', detail);
        check(report, geometry.colors.length <= 3 && geometry.nodes < 15, 'Three-color window and coalesced stream blocks', detail);
      }
      await page.setViewportSize({ width: 1366, height: 900 }); await settle(page);
      const resizer = page.getByRole('separator', { name: 'Resize monitor', exact: true });
      await resizer.focus();
      for (let i = 0; i < 30; i++) await resizer.press('ArrowUp');
      await settle(page);
      const resized = await page.evaluate(() => ({
        pane: document.querySelector('.work-monitor').getBoundingClientRect().height,
        tree: document.querySelector('.sidebar__section--projects').getBoundingClientRect().height,
        nav: document.querySelector('.sidebar__nav').getBoundingClientRect().bottom,
        sidebar: document.querySelector('.sidebar').getBoundingClientRect().bottom,
      }));
      check(report, resized.pane <= 585 && resized.tree >= 79 && resized.nav <= resized.sidebar, 'Resize cap preserves tree and nav', { style, resized });
      await resizer.dblclick(); await settle(page);
      const content = page.locator('.work-monitor__content');
      await content.evaluate(el => { el.scrollTop = 0; el.dispatchEvent(new Event('scroll', { bubbles: true })); });
      await page.getByRole('button', { name: 'Follow output', exact: true }).waitFor();
      await page.evaluate(() => window.__monitorFixture.append()); await page.waitForTimeout(400);
      check(report, await content.evaluate(el => el.scrollTop) < 10, 'Appending does not steal scroll position');
      await page.getByRole('button', { name: 'Follow output', exact: true }).click(); await settle(page);
      check(report, await content.evaluate(el => el.scrollHeight - el.scrollTop - el.clientHeight) <= 48, 'Independent output follow button reaches bottom');
      await screenshot(page, report, output, `work-monitor-${style}.png`);
      await page.evaluate(() => window.__monitorFixture.hidden(true)); await page.waitForTimeout(100);
      const before = await page.evaluate(() => ({ count: window.__monitorFixture.polls, closed: window.__monitorFixture.unsubscriptions.length }));
      await page.waitForTimeout(600);
      check(report, await page.evaluate(count => window.__monitorFixture.polls === count && window.__monitorFixture.active === '', before.count), 'Document hidden unregisters and stops polling');
      check(report, before.closed > 0, 'Hidden cleanup called unsubscribe');
      await page.evaluate(() => window.__monitorFixture.hidden(false)); await page.waitForTimeout(400);
      for (const height of [480, 180]) {
        await page.setViewportSize({ width: 1366, height }); await page.waitForTimeout(200);
        check(report, await page.locator('.work-monitor').count() === 0 && await page.locator('.sidebar--monitor-open').count() === 0
          && await page.evaluate(() => window.__monitorFixture.active === ''), 'Short viewport closes pane and subscription without changing closed baseline', { style, height });
        await page.setViewportSize({ width: 1366, height: 900 }); await settle(page);
        await page.getByRole('button', { name: 'Work monitor', exact: true }).click();
        await page.locator('.work-monitor__entry').first().waitFor();
      }
      await page.getByRole('button', { name: 'Close monitor', exact: true }).click(); await page.waitForTimeout(100);
      check(report, await page.evaluate(() => window.__monitorFixture.active === ''), 'Closing releases capture');
      await context.close();
    }
  } catch (error) { check(report, false, 'Browser run completed', { error: error.stack }); }
  finally { await browser.close(); await finish(report, output, 'work-monitor-results.json'); }
}
module.exports = { installMonitorFixture };
if (require.main === module) run().catch(e => { console.error(e); process.exitCode = 1; });
