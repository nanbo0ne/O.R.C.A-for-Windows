// Synthetic bridge fixture only. Screenshots measure React layout, never a native WebView render.
const qa = require('./transcript-layout.browser.cjs');
const fs = require('node:fs/promises');
const path = require('node:path');

(async () => {
  const { url } = qa.configuration();
  const output = path.resolve(process.argv[3] || path.join(__dirname, '../../../../.tmp/browser-panel-qa'));
  await fs.mkdir(output, { recursive: true });
  const report = qa.newReport('browser-panel-synthetic-layout');
  const browser = await qa.chromium.launch({ headless: true, executablePath: qa.executablePath });
  try {
    for (const style of ['modern', 'classic']) {
      const { page, context } = await qa.appPage(browser, report, { url, style, width: 1366, browserAvailable: true, slowNativeDialogs: true, pickExportPath: 'C:/downloads/file.txt' });
      await page.evaluate(() => {
        const bridge = window.go.main.App;
        const fixture = window.__browserFixture = {
          state: { supported: true, tabs: [{ id: 'fixture-tab', title: 'Fixture page', url: 'http://localhost:5173', loading: false, canGoBack: true, canGoForward: false, zoom: 1 }], activeTabId: 'fixture-tab', busy: false,
            downloads: [{ id: 'download-fixture', tabId: 'fixture-tab', url: 'http://localhost:5173/file.txt', filename: 'file.txt', path: '', status: 'pending', bytesReceived: 0, totalBytes: 120, requiresApproval: true }],
            uploads: [{ id: 'upload-fixture', tabId: 'fixture-tab', url: 'http://localhost:5173/form', origin: 'http://localhost:5173', documentId: 'doc', generation: 1, frameId: 'frame', backendNodeId: 5, multiple: false, accept: '.txt', status: 'pending', requiresApproval: true }] },
          calls: [], viewports: [], inflight: 0, maxInflight: 0,
        };
        if (document.documentElement.dataset.uiStyle === 'modern') { fixture.state.tabs = []; fixture.state.activeTabId = ''; }
        bridge.GetBrowserState = async () => {
          fixture.inflight++; fixture.maxInflight = Math.max(fixture.maxInflight, fixture.inflight);
          await new Promise(resolve => setTimeout(resolve, 1250));
          fixture.inflight--; return structuredClone(fixture.state);
        };
        bridge.BrowserCommand = async (_owner, command) => {
          fixture.calls.push(structuredClone(command));
          if (command.op === 'upload-approve') await new Promise(resolve => setTimeout(resolve, 300));
          if (command.op === 'approve') fixture.state.approval = undefined;
          if (command.op === 'new' && fixture.state.tabs.length === 0) {
            fixture.state.tabs = [{ id: 'fixture-tab', title: 'Fixture page', url: 'about:blank', loading: false, canGoBack: false, canGoForward: false, zoom: 1 }];
            fixture.state.activeTabId = 'fixture-tab';
          }
          if (command.op === 'select') fixture.state.activeTabId = command.tabId;
          return structuredClone(fixture.state);
        };
        bridge.SetBrowserViewport = async (_owner, viewport) => fixture.viewports.push({ ...viewport });
      });
      await page.locator('.app-chrome [aria-label="Browser"]').click();
      await page.locator('.browser-panel').waitFor();
      await page.waitForFunction(() => document.querySelector('.browser-panel')?.getAttribute('data-native-visible') === 'true', { timeout: 8000 }).catch(() => {});
      qa.check(report, await page.evaluate(() => window.__browserFixture.viewports.some(v => v.visible)), 'Supported native host becomes visible for the unobstructed panel', { style, diagnostics: await page.evaluate(() => ({ viewports: window.__browserFixture.viewports, calls: window.__browserFixture.calls, state: window.__browserFixture.state, nativeVisible: document.querySelector('.browser-panel')?.getAttribute('data-native-visible') })) });
      await page.waitForTimeout(2700);
      const expectedBootstrap = style === 'modern' ? 'new' : 'select';
      qa.check(report, await page.evaluate(expected => window.__browserFixture.calls[0]?.op === expected, expectedBootstrap), 'Opening the supported panel creates a blank tab or reactivates the restored tab once', { style, expectedBootstrap });
      qa.check(report, await page.getByRole('region', { name: 'Downloads' }).count() === 1, 'Download requests render in the panel', { style });
      qa.check(report, await page.getByRole('button', { name: 'Choose save location' }).count() === 1, 'Pending download exposes explicit save approval', { style });
      qa.check(report, await page.getByRole('button', { name: 'Choose file to upload' }).count() === 1, 'File chooser request exposes an explicit controlled selection action', { style });
      qa.check(report, await page.locator('.browser-panel input[type=file]').count() === 0, 'Browser panel never injects a DOM file picker control', { style });
      const bounds = await page.evaluate(() => {
        const box = selector => { const r = document.querySelector(selector).getBoundingClientRect(); return { x:r.x,y:r.y,width:r.width,height:r.height,right:r.right,bottom:r.bottom }; };
        const view = box('.browser-panel__viewport'), dock = box('.workbench-dock__body');
        return { view, dock, overflow: document.documentElement.scrollWidth - innerWidth,
          lastViewport: window.__browserFixture.viewports.at(-1), maxInflight: window.__browserFixture.maxInflight };
      });
      report.cases.push({ style, bounds });
      qa.check(report, bounds.view.width > 200 && bounds.view.height > 100, 'Native host placeholder has measurable dock geometry', { style, bounds });
      qa.check(report, bounds.view.x >= bounds.dock.x && bounds.view.right <= bounds.dock.right + 1 && bounds.view.bottom <= bounds.dock.bottom + 1, 'Browser viewport stays inside dock body', { style, bounds });
      qa.check(report, bounds.overflow <= 1, 'Browser dock does not create horizontal page overflow', { style, bounds });
      qa.check(report, bounds.maxInflight === 1, 'State polling remains single-flight during slow responses', { style, bounds });
      const computed = await page.evaluate(() => {
        const read = selector => {
          const style = getComputedStyle(document.querySelector(selector));
          return { background: style.backgroundColor, color: style.color, border: style.borderColor, borderBottom: style.borderBottomColor };
        };
        return { panel: read('.browser-panel'), toolbar: read('.browser-panel__toolbar'), address: read('.browser-panel__address'), download: read('.browser-panel__download') };
      });
      const painted = value => Boolean(value && value !== 'transparent' && value !== 'rgba(0, 0, 0, 0)' && !/rgba\([^)]*,\s*0\s*\)$/.test(value));
      qa.check(report, painted(computed.panel.background) && painted(computed.panel.color), 'Browser panel has resolved theme background and foreground tokens', { style, computed });
      qa.check(report, painted(computed.toolbar.borderBottom) && painted(computed.address.background) && painted(computed.address.border) && painted(computed.download.border), 'Browser toolbar, address field and transfer card have visible computed borders/surfaces', { style, computed });
      await qa.screenshot(page, report, output, `${style}-1366.png`);

      await page.getByRole('button', { name: 'Zoom in' }).click();
      await page.waitForFunction(() => window.__browserFixture.calls.some(c => c.op === 'zoom'));
      qa.check(report, await page.evaluate(() => window.__browserFixture.calls.find(c => c.op === 'zoom').value) === '1.1', 'Zoom sends a numeric factor', { style });
      await page.keyboard.press('Control+T');
      await page.waitForFunction(() => window.__browserFixture.calls.some(c => c.op === 'new'));
      await page.getByRole('button', { name: 'Deny download' }).click();
      await page.waitForFunction(() => window.__browserFixture.calls.some(c => c.op === 'download-deny' && c.value === 'download-fixture' && c.tabId === 'fixture-tab'));
      await page.getByRole('button', { name: 'Choose save location' }).click();
      await page.waitForFunction(() => window.__browserRegression.calls.dialogs.includes('open'));
      await page.waitForTimeout(100);
      qa.check(report, await page.evaluate(() => window.__browserFixture.viewports.at(-1)?.visible === false), 'Native view hides while the Wails save dialog is open', { style, dialogEvents: await page.evaluate(() => window.__browserRegression.calls.dialogs), lastViewport: await page.evaluate(() => window.__browserFixture.viewports.at(-1)) });
      await page.waitForFunction(() => window.__browserFixture.calls.some(c => c.op === 'download-approve'));
      qa.check(report, await page.evaluate(() => {
        const command = window.__browserFixture.calls.find(c => c.op === 'download-approve');
        const destination = JSON.parse(command.value);
        return destination.id === 'download-fixture' && destination.path === 'C:/downloads/file.txt' && command.tabId === 'fixture-tab';
      }), 'Download approval uses the user-selected absolute destination and owning tab', { style });
      await page.getByRole('button', { name: 'Choose file to upload' }).click();
      await page.waitForTimeout(100);
      qa.check(report, await page.evaluate(() => window.__browserFixture.viewports.at(-1)?.visible === false), 'Native view hides while the Wails upload dialog is open', { style, lastViewport: await page.evaluate(() => window.__browserFixture.viewports.at(-1)) });
      await page.waitForFunction(() => window.__browserFixture.calls.some(c => c.op === 'upload-approve' && c.value === 'upload-fixture' && c.tabId === 'fixture-tab'));
      await page.locator('.browser-panel [aria-label="Clear browser data"]').click();
      await page.getByRole('alertdialog').waitFor();
      await page.waitForFunction(() => window.__browserFixture.viewports.at(-1)?.visible === false);
      await page.getByRole('alertdialog').getByRole('button', { name: 'Cancel' }).click();
      await context.close();
    }
    const narrow = await qa.appPage(browser, report, { url, style: 'modern', width: 1024, browserAvailable: true });
    await narrow.page.evaluate(() => {
      const bridge = window.go.main.App;
      const fixture = window.__browserFixture = { state: { supported: true, tabs: [{ id: 'narrow-tab', title: 'Native surface', url: 'about:blank', loading: false, canGoBack: false, canGoForward: false, zoom: 1 }], activeTabId: 'narrow-tab', busy: false, downloads: [], uploads: [] }, calls: [], viewports: [], inflight: 0, maxInflight: 0 };
      bridge.GetBrowserState = async () => structuredClone(fixture.state);
      bridge.BrowserCommand = async (_owner, command) => { fixture.calls.push(structuredClone(command)); return structuredClone(fixture.state); };
      bridge.SetBrowserViewport = async (_owner, viewport) => fixture.viewports.push({ ...viewport });
    });
    await narrow.page.locator('.app-chrome [aria-label="Browser"]').click();
    await narrow.page.locator('.browser-panel').waitFor();
    await narrow.page.waitForFunction(() => window.__browserFixture.viewports.some(v => v.visible), { timeout: 8000 });
    const narrowBounds = await narrow.page.evaluate(() => {
      const rect = selector => { const r = document.querySelector(selector).getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height, right: r.right, bottom: r.bottom }; };
      return { panel: rect('.browser-panel'), dock: rect('.workbench-dock__body'), overflow: document.documentElement.scrollWidth - innerWidth };
    });
    report.cases.push({ style: 'modern', width: 1024, bounds: narrowBounds });
    qa.check(report, narrowBounds.overflow <= 1 && narrowBounds.panel.right <= narrowBounds.dock.right + 1, 'Browser panel remains inside the dock at a narrow viewport', narrowBounds);
    await qa.screenshot(narrow.page, report, output, 'modern-1024.png');
    await narrow.context.close();

    for (const style of ['modern', 'classic']) for (const language of ['en', 'zh']) for (const width of [760, 1024, 1366, 1920]) {
      const { page, context } = await qa.appPage(browser, report, { url, style, language, width, browserAvailable: true });
      await page.evaluate(() => {
        const bridge = window.go.main.App;
        const fixture = window.__browserFixture = {
          state: { supported: true, tabs: [{ id: 'matrix-tab', title: 'Matrix', url: 'about:blank', loading: false, canGoBack: false, canGoForward: false, zoom: 1 }], activeTabId: 'matrix-tab', busy: false, downloads: [], uploads: [] },
          calls: [], viewports: [],
        };
        bridge.GetBrowserState = async () => structuredClone(fixture.state);
        bridge.BrowserCommand = async (_owner, command) => { fixture.calls.push(structuredClone(command)); return structuredClone(fixture.state); };
        bridge.SetBrowserViewport = async (_owner, viewport) => fixture.viewports.push({ ...viewport });
      });
      const browserName = language === 'zh' ? '浏览器' : 'Browser';
      const openerSelector = style === 'modern'
        ? `.modern-chrome__workspace-actions button[aria-label="${browserName}"]`
        : '.app-chrome__browser-toggle';
      const opener = page.locator(openerSelector);
      // Wait for Playwright's stable/visible/receives-events checks before the
      // independent center hit-test. Never bypass a native/DOM obstruction.
      await opener.click({ trial: true });
      const openerHitTest = await opener.evaluate((element, selector) => {
        const rect = element.getBoundingClientRect();
        const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
        return { reachable: Boolean(hit && (hit === element || element.contains(hit)) && hit.closest(selector) === element),
          hit: hit?.outerHTML.slice(0, 1200), rect: rect.toJSON(),
          overlays: [...document.querySelectorAll('[role="dialog"], [role="menu"], .startup-splash, .tooltip')].filter(node => node.getBoundingClientRect().width > 0).map(node => node.outerHTML.slice(0, 300)),
          animations: document.getAnimations().filter(animation => animation.playState === 'running').map(animation => ({ target: animation.effect?.target?.className, time: animation.currentTime })) };
      }, openerSelector);
      await opener.click();
      await page.locator('.browser-panel').waitFor();
      await page.waitForFunction(() => document.querySelector('.browser-panel')?.getAttribute('data-native-visible') === 'true', { timeout: 8000 });
      const geometry = await page.evaluate(() => {
        const rect = selector => { const r = document.querySelector(selector).getBoundingClientRect(); return { x: r.x, y: r.y, width: r.width, height: r.height, right: r.right, bottom: r.bottom }; };
        const activeTab = document.querySelector('.browser-panel [role="tab"][aria-selected="true"]');
        const activeStyle = activeTab ? getComputedStyle(activeTab.parentElement) : null;
        return {
          panel: rect('.browser-panel'), viewport: rect('.browser-panel__viewport'), dock: rect('.workbench-dock__body'),
          overflow: document.documentElement.scrollWidth - innerWidth,
          nativeVisible: document.querySelector('.browser-panel')?.getAttribute('data-native-visible') === 'true',
          lastViewport: window.__browserFixture.viewports.at(-1), activeBackground: activeStyle?.backgroundColor,
        };
      });
      const contained = geometry.panel.x >= geometry.dock.x - 1 && geometry.panel.right <= geometry.dock.right + 1
        && geometry.viewport.x >= geometry.panel.x - 1 && geometry.viewport.right <= geometry.panel.right + 1
        && geometry.viewport.bottom <= geometry.panel.bottom + 1;
      qa.check(report, geometry.nativeVisible && geometry.viewport.width > 100 && geometry.viewport.height > 100 && contained, 'Browser native viewport fits within the dock at every style/language/width', { style, language, width, geometry });
      qa.check(report, geometry.overflow <= 1 && geometry.panel.x >= 0 && geometry.panel.right <= width + 1, 'Browser panel does not create horizontal overflow or escape the app viewport', { style, language, width, geometry });
      qa.check(report, geometry.lastViewport?.visible === true && geometry.lastViewport.width > 100 && geometry.lastViewport.height > 100, 'Native viewport receives the measured visible geometry', { style, language, width, geometry });
      qa.check(report, Boolean(geometry.activeBackground && geometry.activeBackground !== 'rgba(0, 0, 0, 0)'), 'Selected tab has a resolved subtle background', { style, language, width, geometry });
      qa.check(report, openerHitTest.reachable, 'Browser entry button center remains reachable above overlays', { style, language, width, openerHitTest });
      report.cases.push({ style, language, width, geometry });
      await qa.screenshot(page, report, output, `${style}-${language}-${width}.png`);
      await context.close();
    }

    for (const style of ['modern', 'classic']) for (const taskState of ['running', 'awaiting_approval']) {
      const { page, context } = await qa.appPage(browser, report, { url, style, width: 1366, browserAvailable: true });
      await page.evaluate(taskState => {
        const fixture = window.__browserFixture = {
          state: { supported: true, tabs: [{ id: 'leased-tab', title: 'Active task', url: 'about:blank', loading: false, canGoBack: false, canGoForward: false, zoom: 1 }], activeTabId: 'leased-tab', busy: true, taskState, downloads: [], uploads: [] },
          calls: [], viewports: [],
        };
        if (taskState === 'awaiting_approval') fixture.state.approval = { id: 'synthetic-consent', origin: 'http://localhost:3272', reason: 'Synthetic fixture, no website content.' };
        window.go.main.App.GetBrowserState = async () => structuredClone(fixture.state);
        window.go.main.App.BrowserCommand = async (_owner, command) => {
          fixture.calls.push(structuredClone(command));
          throw new Error('browser profile is controlled by an active task; take over first');
        };
        window.go.main.App.SetBrowserViewport = async (_owner, viewport) => fixture.viewports.push({ ...viewport });
      }, taskState);
      await page.locator('.app-chrome [aria-label="Browser"]').click();
      await page.locator('.browser-panel').waitFor();
      await page.waitForTimeout(2400);
      const result = await page.evaluate(() => ({ calls: window.__browserFixture.calls, viewports: window.__browserFixture.viewports, text: document.querySelector('.browser-panel').textContent }));
      qa.check(report, result.calls.length === 0, 'Opening an active task never sends new/select or repeats rejected activation', { style, taskState, result });
      qa.check(report, !result.text.includes('take over first'), 'Active-task panel opens without a spurious ownership error', { style, taskState });
      qa.check(report, result.viewports.some(v => v.visible) === (taskState === 'running'), 'Consent remains covered while an authorized running task can be observed', { style, taskState, result });
      if (taskState === 'awaiting_approval') {
        qa.check(report, await page.getByRole('button', { name: 'Allow', exact: true }).count() === 1, 'Opening a pending task displays consent without granting it', { style });
      }
      report.cases.push({ style, taskState });
      await qa.screenshot(page, report, output, `${style}-task-${taskState}.png`);
      await context.close();
    }

    const blocked = await qa.appPage(browser, report, { url, style: 'modern', width: 1024, browserAvailable: true });
    await blocked.page.evaluate(() => {
      const shield = document.createElement('div');
      shield.id = 'synthetic-click-obstruction';
      shield.style.cssText = 'position:fixed;inset:0;z-index:2147483647;background:rgba(255,0,0,.1)';
      document.body.append(shield);
    });
    let rejected = false;
    try { await blocked.page.locator('.app-chrome [aria-label="Browser"]').click({ trial: true, timeout: 700 }); }
    catch (error) { rejected = /intercepts pointer events/.test(error.message); }
    qa.check(report, rejected && await blocked.page.locator('.browser-panel').count() === 0, 'Negative control rejects a permanently obstructed entry without bypassing actionability');
    await blocked.context.close();

    const gated = await qa.appPage(browser, report, { url, style: 'classic', width: 1024, browserAvailable: false });
    await gated.page.waitForTimeout(250);
    qa.check(report, await gated.page.getByRole('button', { name: 'Browser' }).count() === 0 && await gated.page.getByRole('tab', { name: 'Browser' }).count() === 0, 'Browser entry points stay hidden when Preview capability is unavailable');
    await gated.context.close();
  } finally { await browser.close(); }
  await qa.finish(report, output, 'report.json');
})().catch(error => { console.error(error); process.exitCode = 1; });
