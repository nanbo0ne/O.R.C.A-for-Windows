// Headless, synthetic bridge only. No native or live account/model requests.
const qa = require('./transcript-layout.browser.cjs');
const fs = require('node:fs/promises');
const path = require('node:path');

async function geometry(page) {
  return page.evaluate(() => Object.fromEntries([
    '.app-chrome', '.sidebar', '.transcript', '.composer', '.topicbar',
    '.modern-chrome__menus', '.modern-chrome__workspace-actions',
    '.app-chrome__actions', '.app-chrome__tools', '.app-chrome__status',
  ].map(selector => {
    const r = document.querySelector(selector)?.getBoundingClientRect();
    return [selector, r ? { x: r.x, y: r.y, width: r.width, height: r.height } : null];
  })));
}

async function betaPage(browser, report, options, behavior = {}) {
  return qa.appPage({ newContext: async config => {
    const context = await browser.newContext(config);
    await context.addInitScript(behavior => {
      let binding;
      Object.defineProperty(window, 'go', { configurable: true, get: () => binding, set(value) {
        binding = value;
        const api = value.main.App;
        const roots = new Map();
        const syntheticRoot = value => {
          if (!roots.has(value)) roots.set(value, `C:/SyntheticProject${roots.size + 1}`);
          return roots.get(value);
        };
        const syntheticNames = value => {
          if (Array.isArray(value)) return value.map(syntheticNames);
          if (!value || typeof value !== 'object') return value;
          const copy = Object.fromEntries(Object.entries(value).map(([key, item]) => [key, syntheticNames(item)]));
          for (const key of ['root', 'workspaceRoot', 'cwd']) if (typeof copy[key] === 'string' && copy[key]) copy[key] = syntheticRoot(copy[key]);
          if (copy.workspaceName) copy.workspaceName = 'SyntheticProject';
          if (copy.topicTitle) copy.topicTitle = 'TestConversation';
          if (copy.kind === 'project') copy.label = 'SyntheticProject';
          if (copy.kind === 'topic') copy.label = 'TestConversation';
          return copy;
        };
        for (const name of ['ListTabs', 'ListProjectTree', 'MetaForTab', 'Meta']) {
          const original = api[name]?.bind(api);
          if (original) api[name] = async (...args) => syntheticNames(await original(...args));
        }
        const f = window.__betaFixture = {
          calls: [], bounds: [], viewports: [], inflight: 0, maxInflight: 0, modelReads: 0, modelPicks: 0, ...behavior,
          account: { state: 'signed_out', authenticated: false, displayName: '', error: '' },
          wallet: { currency: 'CNY', cash: '900719925474099312345678.123456', held: '0.000001', available: '9223372036854.775807', gifts: [{ groups: 'Research / group 42', available: '8.000001' }], error: '' },
          receipts: [
            { requestId: 'request-settled', turnId: 'turn-fixture', purpose: 'main', status: 'settled', cost: '0.000001', currency: 'CNY' },
            { requestId: 'request-pending', turnId: 'turn-fixture', purpose: 'browser', status: 'pending', cost: '', currency: 'CNY' },
            { requestId: 'request-unknown', turnId: 'turn-fixture', purpose: 'review', status: 'unknown', cost: '', currency: 'CNY' },
          ],
          turns: [
            { turnId: 'turn-fixture', status: 'pending', currency: 'CNY', scope: 'retained_requests', cost: '', settledSubtotal: '0.000001', requests: 3, settledRequests: 1 },
            { turnId: 'turn-exact', status: 'settled', currency: 'CNY', scope: 'retained_requests', cost: '9007199254.740994', settledSubtotal: '9007199254.740994', requests: 2, settledRequests: 2 },
            { turnId: 'turn-estimated', status: 'estimated', currency: 'CNY', scope: 'retained_requests', cost: '1.000006', settledSubtotal: '0.000001', requests: 2, settledRequests: 1 },
            { turnId: 'turn-unknown', status: 'unknown', currency: 'CNY', scope: 'retained_requests', cost: '', settledSubtotal: '', requests: 1, settledRequests: 0 },
          ],
          web: { sendAuthorized: false, settings: { enabled: false, rpm: 2, reservePaused: true, openingRounds: 1, openings: ['Synthetic opening', 'Synthetic second'], thinking: true, search: false }, browser: { ready: true, busy: false }, pending: 3, ready: 2, preparing: false, sent: 7 },
        };
        const mark = name => f.calls.push(name);
        const originalModels = api.ModelsForTab.bind(api);
        const originalSetModel = api.SetModelForTab.bind(api);
        api.ModelsForTab = async (...args) => { f.modelReads++; return originalModels(...args); };
        api.SetModelForTab = async (...args) => { f.modelPicks++; return originalSetModel(...args); };
        api.BetaCapabilities = async () => ({ enabled: !f.disabled, account: !f.webOnly, web: !f.accountOnly, browser: true });
        api.OrcaAccountStatus = async () => {
          mark('accountStatus'); f.inflight++; f.maxInflight = Math.max(f.maxInflight, f.inflight);
          try { if (f.slowStatus) await new Promise(resolve => setTimeout(resolve, 1800)); return structuredClone(f.account); }
          finally { f.inflight--; }
        };
        api.OrcaAccountLogin = async () => { mark('login'); if (f.account.needsSignOut) throw new Error('sign out of the current account before signing in again'); if (f.failLogin) throw new Error('SYNTHETIC_PRIVATE_ERROR'); f.account.state = 'signing_in'; };
        api.OrcaAccountCancelLogin = async () => { mark('cancelLogin'); f.account.state = 'signed_out'; };
        api.OrcaAccountLogout = async () => { mark('logout'); f.account = { state: 'signed_out', authenticated: false, displayName: '', error: '' }; };
        api.OrcaAccountWallet = async () => { mark('wallet'); if (f.walletDelay) await new Promise(resolve => setTimeout(resolve, f.walletDelay)); if (f.failWallet) throw new Error('network unavailable SYNTHETIC_PRIVATE_ERROR'); return structuredClone(f.wallet); };
        api.OrcaAccountReceipts = async () => { mark('receipts'); return structuredClone(f.receipts); };
        api.OrcaAccountTurnSettlements = async () => { mark('turns'); return structuredClone(f.turns); };
        api.WebBetaStatus = async () => {
          mark('webStatus');
          const snapshot = structuredClone(f.web);
          if (f.holdWebStatus) {
            f.holdWebStatus = false;
            f.webStatusHeld = true;
            await new Promise(resolve => { f.releaseWebStatus = resolve; });
          }
          return snapshot;
        };
        api.WebBetaOpen = async () => { mark('open'); if (f.openDelay) await new Promise(resolve => setTimeout(resolve, f.openDelay)); if (f.failOpen) throw new Error('SYNTHETIC_PRIVATE_ERROR'); return structuredClone(f.web); };
        api.WebBetaBounds = async bounds => { if (f.boundsDelay) await new Promise(resolve => setTimeout(resolve, f.boundsDelay)); f.bounds.push(structuredClone(bounds)); };
        api.WebBetaConfigure = async settings => { mark('configure'); f.web.settings = structuredClone(settings); f.web.sendAuthorized = settings.enabled; };
        api.WebBetaRecover = async () => {
          mark('recover');
          if (f.recoverDelay) await new Promise(resolve => setTimeout(resolve, f.recoverDelay));
          if (f.failRecover) throw new Error(typeof f.failRecover === 'string' ? f.failRecover : 'SYNTHETIC_PRIVATE_ERROR');
          f.web.blocked = ''; f.web.browser.blocked = ''; f.web.error = '';
          f.web.settings.enabled = f.web.sendAuthorized; f.web.settings.reservePaused = true;
        };
        api.WebBetaUseCurrentLogin = async () => {
          mark('useCurrentLogin');
          if (f.switchDelay) await new Promise(resolve => setTimeout(resolve, f.switchDelay));
          if (f.switchError) throw new Error(f.switchError);
          f.web.blocked = ''; f.web.browser.blocked = ''; f.web.error = '';
          f.web.settings.enabled = f.web.sendAuthorized; f.web.settings.reservePaused = true;
        };
        api.WebBetaLogout = async () => { mark('webLogout'); f.web.browser.ready = false; f.web.settings.enabled = false; f.web.sendAuthorized = false; };
        const state = { supported: true, tabs: [{ id: 'beta-fixture', title: 'Synthetic browser', url: 'about:blank', loading: false, canGoBack: false, canGoForward: false, zoom: 1 }], activeTabId: 'beta-fixture', busy: false, downloads: [], uploads: [] };
        api.GetBrowserState = async () => structuredClone(state);
        api.BrowserCommand = async () => structuredClone(state);
        api.SetBrowserViewport = async (_owner, viewport) => f.viewports.push(structuredClone(viewport));
      } });
    }, behavior);
    return context;
  } }, report, options);
}

const labels = language => language === 'zh'
  ? { connections: '账号与连接', settings: '设置', more: '更多', browser: '浏览器', account: 'ORCA 账号', web: 'DeepSeek 网页（实验）', login: '登录', logout: '退出登录', cancel: '取消登录', webSettings: 'DeepSeek 网页设置', save: '保存', retry: '重试' }
  : { connections: 'Account & connections', settings: 'Settings', more: 'More', browser: 'Browser', account: 'ORCA account', web: 'DeepSeek Web (experimental)', login: 'Log in', logout: 'Log out', cancel: 'Cancel login', webSettings: 'DeepSeek Web settings', save: 'Save', retry: 'Retry' };

async function openConnections(page, style, language, width) {
  const l = labels(language);
  if (style === 'modern' || width >= 1200) await page.locator('[data-beta-entry]').click();
  else {
    if (style === 'modern') {
      await page.locator('.modern-chrome__menu summary').filter({ hasText: l.settings }).click();
      await page.getByRole('menuitem', { name: l.connections }).click();
    } else {
      await page.locator('.app-chrome__command').click();
      await page.locator('.palette__input').fill('beta');
      await page.getByRole('option', { name: l.connections }).click();
    }
  }
  await page.getByRole('dialog', { name: l.connections, exact: true }).waitFor();
  await page.locator('.beta-connections-row').first().getByRole('button').first().waitFor();
}

async function dialogGeometry(page) {
  return page.evaluate(() => {
    const section = [...document.querySelectorAll('.beta-connections-dialog')].findLast(node => !node.closest('[inert]'));
    const r = section.getBoundingClientRect();
    const unreachable = [...section.querySelectorAll('button:not(:disabled),input,select,textarea')].filter(node => {
      const b = node.getBoundingClientRect();
      if (!b.width || !b.height) return false;
      const hit = document.elementFromPoint(b.x + b.width / 2, b.y + b.height / 2);
      return !hit || !(hit === node || node.contains(hit));
    }).map(node => node.getAttribute('aria-label') || node.textContent);
    return { x:r.x, y:r.y, right:r.right, bottom:r.bottom, width:r.width, height:r.height, overflow: section.scrollWidth - section.clientWidth, unreachable, viewportOverflow: document.documentElement.scrollWidth - innerWidth };
  });
}

async function main() {
  const { url } = qa.configuration();
  const output = path.resolve(process.argv[3] || '.tmp/beta-connections-qa');
  const baselineOnly = process.argv.includes('--baseline');
  const baselineFile = process.env.ORCA_BETA_BASELINE;
  const baseline = baselineFile ? JSON.parse(await fs.readFile(baselineFile, 'utf8')) : null;
  await fs.mkdir(output, { recursive: true });
  const report = qa.newReport('beta-connections-synthetic');
  const browser = await qa.chromium.launch({ headless: true, executablePath: qa.executablePath });
  try {
    for (const style of ['modern', 'classic']) for (const language of ['en', 'zh']) for (const width of [760, 1024, 1366, 1920]) {
      if (process.env.ORCA_BETA_CASE && `${style}-${language}-${width}` !== process.env.ORCA_BETA_CASE) continue;
      const options = { url, style, language, width, browserAvailable: true };
      const { page, context } = baselineOnly ? await qa.appPage(browser, report, options) : await betaPage(browser, report, options);
      const key = `${style}-${language}-${width}`;
      const initial = await geometry(page);
      report.cases.push({ key, geometry: initial });
      await qa.screenshot(page, report, output, `${key}-shell.png`);
      if (!baselineOnly) {
        const before = baseline?.cases.find(item => item.key === key)?.geometry;
        if (baseline) qa.check(report, JSON.stringify(before) === JSON.stringify(initial), 'Existing shell, Composer and controls match pre-edit geometry exactly', { key, before, after: initial });
        qa.check(report, await page.locator('[data-beta-entry]').isVisible() === (style === 'modern' || width >= 1200), 'Beta entry follows the four Modern menus; Classic compact fallback remains', { key });
        if (style === 'modern') qa.check(report, await page.evaluate(() => {
          const menus = document.querySelector('.modern-chrome__menus')?.getBoundingClientRect();
          const entry = document.querySelector('.modern-chrome__beta-entry')?.getBoundingClientRect();
          return !!menus && !!entry && Math.abs(entry.left - menus.right) <= 1 && entry.width >= 28;
        }), 'Account entry is immediately right of the four menus', { key });
        const l = labels(language);
        await page.locator('.app-chrome').getByRole('button', { name: l.browser, exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.viewports.some(v => v.visible));
        await openConnections(page, style, language, width);
        await page.waitForFunction(() => !document.querySelector('.beta-connections-row button')?.disabled);
        await page.waitForFunction(() => window.__betaFixture.viewports.at(-1)?.visible === false);
        qa.check(report, await page.locator('[data-testid="beta-webview"]').count() === 0, 'Primary dialog contains no native web view', { key });
        qa.check(report, await page.evaluate(() => !window.__betaFixture.calls.some(c => ['open', 'configure', 'recover', 'wallet'].includes(c))), 'Primary open is status-only with no sends or mutations', { key });
        const measured = await dialogGeometry(page);
        qa.check(report, measured.x >= 0 && measured.y >= 0 && measured.right <= width && measured.bottom <= 900 && measured.overflow <= 1 && measured.viewportOverflow <= 1 && measured.unreachable.length === 0, 'Primary dialog is contained and controls are click-reachable', { key, measured });
        await qa.screenshot(page, report, output, `${key}-connections.png`);
        const account = page.locator('.beta-connections-row').filter({ has: page.getByRole('heading', { name: l.account, exact: true }) });
        await account.getByRole('button', { name: language === 'zh' ? '使用ORCA账号登录' : 'Sign in with ORCA', exact: true }).click();
        await account.getByRole('button', { name: l.cancel, exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.calls.includes('cancelLogin'));
        const modelReadsBeforeLogin = await page.evaluate(() => window.__betaFixture.modelReads);
        await page.evaluate(() => { window.__betaFixture.account = { state: 'signed_in', authenticated: true, displayName: 'Synthetic account', error: '' }; });
        await page.waitForFunction(before => window.__betaFixture.modelReads > before, modelReadsBeforeLogin);
        qa.check(report, await page.evaluate(() => window.__betaFixture.modelPicks === 0), 'Successful login refreshes model metadata without switching models', { key });
        const accountDetailsName = language === 'zh' ? '账号余额与结算记录' : 'Account balance & receipts';
        await page.getByRole('button', { name: accountDetailsName }).click();
        await page.locator('[data-money="cash"]').filter({ hasText: '900719925474099312345678.123456' }).waitFor();
        qa.check(report, await page.locator('[data-money="cash"]').textContent() === 'CNY 900719925474099312345678.123456', 'Large server decimal strings are displayed exactly without Number rounding', { key });
        qa.check(report, (await page.locator('.beta-account-gifts').textContent()).includes(language === 'zh' ? '适用分组：Research / group 42' : 'Groups: Research / group 42'), 'Gift credit retains explicit group scope separate from cash', { key });
        qa.check(report, (await page.locator('[data-receipt-id="request-pending"]').textContent()).includes(language === 'zh' ? '待结算' : 'Pending') && (await page.locator('[data-receipt-id="request-pending"] td').last().textContent()) === (language === 'zh' ? '未知' : 'Unknown'), 'Pending receipt is translated and an absent cost is not zero', { key });
        qa.check(report, await page.evaluate(() => window.__betaFixture.viewports.at(-1)?.visible === false), 'Ordinary native browser remains hidden under account details', { key });
        const accountGeometry = await dialogGeometry(page);
        qa.check(report, accountGeometry.overflow <= 1 && accountGeometry.right <= width && accountGeometry.bottom <= 900 && accountGeometry.unreachable.length === 0, 'Account balance and receipt layout is contained and usable', { key, accountGeometry });
        await qa.screenshot(page, report, output, `${key}-account.png`);
        await page.locator('.beta-account-turns summary').click();
        await page.locator('[data-turn-id="turn-fixture"]').scrollIntoViewIfNeeded();
        qa.check(report, await page.locator('.beta-account-turns summary').textContent() === (language === 'zh' ? '可见请求按回合汇总' : 'Retained requests by turn'), 'Turn summaries explicitly describe the retained request scope', { key });
        qa.check(report, await page.locator('[data-turn-id="turn-fixture"] [data-turn-cost]').textContent() === '' && await page.locator('[data-turn-id="turn-fixture"] [data-turn-subtotal]').textContent() === 'CNY 0.000001', 'Pending turn cost stays blank while its settled subtotal is displayed separately', { key });
        qa.check(report, await page.locator('[data-turn-id="turn-exact"] [data-turn-cost]').textContent() === 'CNY 9007199254.740994', 'Server-deduplicated turn cost retains exact decimals without frontend aggregation', { key });
        qa.check(report, (await page.locator('[data-turn-id="turn-estimated"]').textContent()).includes(language === 'zh' ? '估算' : 'Estimated') && await page.locator('[data-turn-id="turn-estimated"] [data-turn-cost]').textContent() === 'CNY 1.000006', 'Estimated retained-request cost is labeled distinctly from settled cost', { key });
        qa.check(report, await page.locator('[data-turn-id="turn-unknown"] [data-turn-cost]').textContent() === '' && await page.locator('[data-turn-id="turn-unknown"] [data-turn-subtotal]').textContent() === '', 'Unknown turn cost and missing subtotal remain blank rather than zero', { key });
        await qa.screenshot(page, report, output, `${key}-turns.png`);
        await page.locator('.beta-account-turns summary').focus();
        await page.keyboard.press('Tab');
        qa.check(report, await page.evaluate(() => document.activeElement?.classList.contains('beta-account-turn-table')), 'Retained-turn disclosure remains in the explicit dialog focus order', { key });
        await page.keyboard.press('Escape');
        await account.getByRole('button', { name: l.logout, exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.calls.includes('logout'));
        const web = page.locator('.beta-connections-row').filter({ has: page.getByRole('heading', { name: l.web, exact: true }) });
        await web.getByRole('button', { name: l.login, exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.bounds.at(-1)?.visible === true);
        qa.check(report, await page.locator('.beta-connections-backdrop[inert]').count() === 1, 'Nested login makes only its parent dialog inert', { key });
        const loginGeometry = await dialogGeometry(page);
        const viewport = await page.evaluate(() => { const r = document.querySelector('[data-testid="beta-webview"]').getBoundingClientRect(); return { x:r.x, y:r.y, width:r.width, height:r.height, sent:window.__betaFixture.bounds.at(-1) }; });
        qa.check(report, viewport.sent.x === Math.round(viewport.x) && viewport.sent.y === Math.round(viewport.y) && viewport.sent.width === Math.round(viewport.width) && viewport.sent.height === Math.round(viewport.height) && viewport.width > 100 && viewport.height > 100 && loginGeometry.unreachable.length === 0, 'Nested native host receives contained physical-pixel bounds', { key, viewport, loginGeometry });
        await page.keyboard.press('Control+k');
        qa.check(report, await page.locator('.palette').count() === 0, 'Global palette shortcut cannot escape modal', { key });
        await page.keyboard.press('Tab');
        qa.check(report, await page.evaluate(() => document.activeElement?.closest('.beta-connections-dialog--web') !== null), 'Tab remains in nested dialog', { key });
        await qa.screenshot(page, report, output, `${key}-login.png`);
        await page.keyboard.press('Escape');
        await page.waitForFunction(() => window.__betaFixture.bounds.at(-1)?.visible === false);
        await qa.settle(page);
        qa.check(report, await page.getByRole('dialog', { name: l.connections, exact: true }).count() === 1, 'Escape closes nested login only', { key });
        qa.check(report, await page.evaluate(() => window.__betaFixture.viewports.at(-1)?.visible === false), 'Ordinary browser remains hidden while primary stays open', { key });
        qa.check(report, await web.getByRole('button', { name: l.login, exact: true }).evaluate(node => node === document.activeElement), 'Closing nested login restores focus to login button', { key });
        await web.getByRole('button', { name: l.webSettings }).click();
        await page.getByRole('dialog', { name: l.webSettings, exact: true }).waitFor();
        qa.check(report, await page.locator('[data-testid="beta-webview"]').count() === 0, 'Settings dialog does not mount a native page', { key });
        await page.locator('.beta-connections-settings input[type="range"]').fill('4');
        const settingsGeometry = await dialogGeometry(page);
        qa.check(report, settingsGeometry.x >= 0 && settingsGeometry.right <= width && settingsGeometry.bottom <= 900 && settingsGeometry.overflow <= 1 && settingsGeometry.unreachable.length === 0, 'Settings controls remain contained and click-reachable', { key, settingsGeometry });
        await qa.screenshot(page, report, output, `${key}-settings.png`);
        await page.getByRole('button', { name: l.save, exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.calls.includes('configure'));
        qa.check(report, await page.evaluate(() => window.__betaFixture.web.settings.rpm === 4 && !window.__betaFixture.web.settings.enabled), 'Settings changes do not implicitly enable sending', { key });
        await page.keyboard.press('Escape');
        await page.locator('.beta-connections-backdrop').waitFor({ state: 'detached' });
        await page.waitForFunction(() => window.__betaFixture.viewports.at(-1)?.visible === true);
        await qa.settle(page);
        const restoredFocus = await page.evaluate(() => ({ active: document.activeElement?.outerHTML.slice(0, 350), rootInert: document.querySelector('#root')?.inert, focused: document.activeElement !== document.body, remaining: document.querySelectorAll('.beta-connections-backdrop').length }));
        qa.check(report, restoredFocus.focused && !restoredFocus.rootInert, 'Closing primary restores shell focus and interaction', { key, restoredFocus });
        qa.check(report, await page.evaluate(() => window.__betaFixture.web.pending === 3 && window.__betaFixture.web.sent === 7 && !window.__betaFixture.calls.includes('webLogout')), 'Closing panels preserves queue, sent count and login', { key });
      }
      await context.close();
    }
    if (!baselineOnly) {
      for (const style of ['modern', 'classic']) {
        const { page, context } = await betaPage(browser, report, { url, style, width: 1024, browserAvailable: true }, { disabled: true });
        qa.check(report, await page.locator('[data-beta-entry]').count() === 0, 'Release channel hides new entry entirely', { style });
        await context.close();
      }
      const options = { url, style: 'modern', language: 'en', width: 1024, browserAvailable: true };
      {
        const { page, context } = await betaPage(browser, report, options, { slowStatus: true, failLogin: true });
        await openConnections(page, 'modern', 'en', 1024);
        const account = page.locator('.beta-connections-row').first();
        await account.getByRole('button', { name: 'Sign in with ORCA', exact: true }).click();
        await page.getByRole('alert').waitFor();
        qa.check(report, !(await page.textContent('body')).includes('SYNTHETIC_PRIVATE_ERROR'), 'Backend error details never leak into the UI');
        await page.waitForTimeout(2200);
        qa.check(report, await page.evaluate(() => window.__betaFixture.maxInflight === 1), 'Slow status polls remain single-flight');
        await page.evaluate(() => { window.__betaFixture.failLogin = false; });
        await account.getByRole('button', { name: 'Sign in with ORCA', exact: true }).click();
        await account.getByRole('button', { name: 'Cancel login' }).waitFor();
        await page.evaluate(() => { window.__betaFixture.account = { state: 'error', authenticated: false, displayName: '', error: 'SYNTHETIC_PRIVATE_ERROR' }; });
        await page.getByRole('alert').waitFor();
        qa.check(report, !(await page.textContent('body')).includes('SYNTHETIC_PRIVATE_ERROR') && await account.getByRole('button', { name: 'Sign in with ORCA', exact: true }).isEnabled(), 'Async signing-in errors stay local and allow retry');
        await account.getByRole('button', { name: 'Sign in with ORCA', exact: true }).click();
        await account.getByRole('button', { name: 'Cancel login' }).waitFor();
        await page.keyboard.press('Escape');
        await page.locator('.beta-connections-backdrop').waitFor({ state: 'detached' });
        qa.check(report, await page.evaluate(() => window.__betaFixture.account.state === 'signing_in'), 'Closing the primary panel does not cancel an authorized async login');
        await context.close();
      }
      {
        const { page, context } = await betaPage(browser, report, options, { failOpen: true });
        await openConnections(page, 'modern', 'en', 1024);
        await page.locator('.beta-connections-row').last().getByRole('button', { name: 'Log in', exact: true }).click();
        await page.getByRole('button', { name: 'Retry' }).waitFor();
        qa.check(report, await page.evaluate(() => !window.__betaFixture.bounds.some(v => v.visible)), 'Failed open never presents a native view');
        await page.evaluate(() => { window.__betaFixture.failOpen = false; });
        await page.getByRole('button', { name: 'Retry' }).click();
        await page.waitForFunction(() => window.__betaFixture.bounds.at(-1)?.visible === true);
        await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')); });
        await page.waitForFunction(() => window.__betaFixture.bounds.at(-1)?.visible === false);
        await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: false }); document.dispatchEvent(new Event('visibilitychange')); });
        await page.waitForFunction(() => window.__betaFixture.bounds.at(-1)?.visible === true);
        qa.check(report, await page.evaluate(() => window.__betaFixture.calls.filter(c => c === 'open').length === 2), 'Visibility changes do not reopen or send requests');
        await page.keyboard.press('Escape');
        await page.evaluate(() => { window.__betaFixture.openDelay = 800; window.__betaFixture.boundsDelay = 80; });
        await page.locator('.beta-connections-row').last().getByRole('button', { name: 'Log in', exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.calls.filter(c => c === 'open').length === 3);
        await page.keyboard.press('Escape');
        await page.waitForTimeout(1100);
        qa.check(report, await page.evaluate(() => window.__betaFixture.bounds.at(-1)?.visible === false), 'Late native open completion remains hidden after close');
        await page.keyboard.press('Escape');
        await context.close();
      }
      {
        const { page, context } = await betaPage(browser, report, options);
        await openConnections(page, 'modern', 'en', 1024);
        await page.getByRole('button', { name: 'DeepSeek Web settings' }).click();
        await page.getByLabel('Enable web agent access').check();
        qa.check(report, await page.getByRole('button', { name: 'Save', exact: true }).isDisabled(), 'Enabling agent access requires explicit consent');
        await page.getByLabel('Pause reserve conversations').uncheck();
        await page.getByLabel('Allow agent tasks', { exact: false }).check();
        qa.check(report, await page.getByRole('button', { name: 'Save', exact: true }).isDisabled(), 'Reserve openings require separate explicit consent');
        await page.getByLabel('Allow the opening messages', { exact: false }).check();
        await page.getByRole('button', { name: 'Save', exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.calls.includes('configure'));
        qa.check(report, await page.evaluate(() => window.__betaFixture.web.settings.enabled && !window.__betaFixture.web.settings.reservePaused && !window.__betaFixture.calls.includes('open')), 'Explicit settings save enables only the consented settings without opening a page');
        await page.getByRole('button', { name: 'DeepSeek Web settings' }).click();
        await page.getByRole('button', { name: 'Log out of DeepSeek', exact: true }).click();
        await page.waitForFunction(() => window.__betaFixture.calls.includes('webLogout'));
        qa.check(report, await page.evaluate(() => !window.__betaFixture.calls.includes('logout')), 'DeepSeek logout never logs out the ORCA account');
        await page.keyboard.press('Escape');
        await context.close();
      }
      for (const behavior of [{ accountOnly: true }, { webOnly: true }]) {
        const { page, context } = await betaPage(browser, report, options, behavior);
        await openConnections(page, 'modern', 'en', 1024);
        qa.check(report, await page.locator('.beta-connections-row').count() === 1, 'Individual capabilities gate account and web independently', behavior);
        await page.waitForTimeout(1600);
        qa.check(report, await page.evaluate(behavior => !window.__betaFixture.calls.includes(behavior.accountOnly ? 'webStatus' : 'accountStatus'), behavior), 'Unavailable capability is never queried', behavior);
        await context.close();
      }
      {
        const { page, context } = await betaPage(browser, report, options);
        await openConnections(page, 'modern', 'en', 1024);
        await page.evaluate(() => { window.__betaFixture.account = { state: 'signed_in', authenticated: true, displayName: 'Wallet fixture', error: '' }; });
        await page.getByRole('button', { name: 'Account balance & receipts' }).click();
        await page.locator('[data-money="cash"]').filter({ hasText: '900719925474099312345678.123456' }).waitFor();
        await page.waitForTimeout(4500);
        qa.check(report, await page.evaluate(() => window.__betaFixture.calls.filter(c => c === 'wallet').length === 1), 'Repeated receipt polls reuse wallet within the ten-second interval');
        await page.evaluate(() => { const f = window.__betaFixture; f.receipts[1].status = 'settled'; f.receipts[1].cost = '1.000001'; f.wallet.available = '9223372036853.775806'; });
        await page.locator('[data-money="available"]').filter({ hasText: '9223372036853.775806' }).waitFor();
        qa.check(report, await page.evaluate(() => window.__betaFixture.calls.filter(c => c === 'wallet').length === 2), 'New settlement refreshes wallet before the periodic interval');
        await page.evaluate(() => { window.__betaFixture.failWallet = true; });
        await page.getByRole('button', { name: 'Refresh account', exact: true }).click();
        await page.locator('[data-money="cash"]').filter({ hasText: 'Unknown' }).waitFor();
        qa.check(report, await page.locator('.beta-account-gifts').count() === 0 && !(await page.textContent('body')).includes('SYNTHETIC_PRIVATE_ERROR'), 'Failed wallet refresh cannot present stale balances or gifts as current');
        await page.evaluate(() => { window.__betaFixture.account.state = 'logging_out'; });
        await page.getByRole('dialog', { name: 'Account balance & receipts' }).waitFor({ state: 'detached' });
        qa.check(report, await page.locator('.beta-connections-row').first().getByRole('button', { name: 'Log out', exact: true }).isDisabled(), 'Logging-out state blocks account actions and removes private balance details');
        await page.evaluate(() => { window.__betaFixture.account = { state: 'error', authenticated: false, displayName: '', error: 'orca account: authorization rejected; access_token=SYNTHETIC_PRIVATE_ERROR' }; });
        await page.getByRole('alert').filter({ hasText: 'Authorization was rejected' }).waitFor();
        qa.check(report, !(await page.textContent('body')).includes('SYNTHETIC_PRIVATE_ERROR'), 'Specific sign-in failure category is visible without raw private error details');
        await context.close();
      }
      for (const scenario of [
        { style: 'modern', language: 'en', width: 760 },
        { style: 'classic', language: 'zh', width: 760 },
        { style: 'modern', language: 'zh', width: 1024 },
        { style: 'classic', language: 'en', width: 1024 },
      ]) {
        const { style, language, width } = scenario;
        const key = `receipt-safety-${style}-${language}-${width}`;
        const { page, context } = await betaPage(browser, report, { url, browserAvailable: true, ...scenario });
        const before = await geometry(page);
        const warning = language === 'zh' ? '部分结算记录未能保存到本机，重启后可能缺失。' : 'Some receipt records could not be saved locally and may be missing after restart.';
        const unknown = language === 'zh' ? '未知' : 'Unknown';
        const detailsName = language === 'zh' ? '账号余额与结算记录' : 'Account balance & receipts';
        await openConnections(page, style, language, width);
        await page.evaluate(() => {
          const f = window.__betaFixture;
          f.account = { state: 'signed_in', authenticated: true, displayName: 'Synthetic account', error: '' };
          f.receipts[0].cost = '900719925474099312345678.123456';
          f.receipts[1].cost = '12345.123456';
          f.receipts[2].cost = '98765.654321';
          f.receipts.push({ requestId: 'request-estimated', turnId: 'turn-fixture', purpose: 'review', status: 'estimated', cost: '1.000006', currency: 'CNY' });
          f.receipts.push({ requestId: 'request-malformed', turnId: 'turn-fixture', purpose: 'review', status: 'unknown', cost: 'SYNTHETIC_PRIVATE_COST', currency: 'CNY' });
        });
        await page.getByRole('button', { name: detailsName }).waitFor();
        await page.evaluate(() => { window.__betaFixture.account.receiptError = 'session expired C:/SYNTHETIC_PRIVATE_CONFIG access_token=SYNTHETIC_PRIVATE_ERROR'; });
        const account = page.locator('.beta-connections-row').first();
        await account.locator('[data-receipt-warning]').waitFor();
        qa.check(report, await account.locator('[data-receipt-warning]').textContent() === warning && !(await page.textContent('body')).includes('SYNTHETIC_PRIVATE'), 'Receipt persistence failure uses only a fixed localized public warning', { key });
        qa.check(report, await account.getByRole('button', { name: labels(language).logout, exact: true }).isEnabled() && (await account.getByRole('status').textContent()) === (language === 'zh' ? '已登录' : 'Signed in'), 'Receipt storage warning never changes the signed-in state or becomes a sign-out error', { key });
        const primary = await dialogGeometry(page);
        qa.check(report, primary.overflow <= 1 && primary.right <= width && primary.bottom <= 900 && primary.unreachable.length === 0, 'Receipt warning stays contained in the new account section', { key, primary });
        await qa.screenshot(page, report, output, `${key}-warning.png`);
        await page.getByRole('button', { name: detailsName }).click();
        const details = page.getByRole('dialog', { name: detailsName, exact: true });
        await details.locator('[data-receipt-id="request-malformed"]').waitFor();
        qa.check(report, await details.locator('[data-receipt-warning]').textContent() === warning, 'Account details exposes the same receipt-specific storage warning', { key });
        const hiddenCosts = await Promise.all(['request-pending', 'request-unknown', 'request-malformed'].map(id => page.locator(`[data-receipt-id="${id}"] td`).last().textContent()));
        qa.check(report, hiddenCosts.every(cost => cost === unknown) && !(await details.textContent()).includes('SYNTHETIC_PRIVATE'), 'Pending and unknown receipts suppress both numeric and malformed nonempty costs', { key, hiddenCosts });
        qa.check(report, await page.locator('[data-receipt-id="request-settled"] td').last().textContent() === 'CNY 900719925474099312345678.123456' && await page.locator('[data-receipt-id="request-estimated"] td').last().textContent() === 'CNY 1.000006', 'Settled and estimated receipts preserve exact authoritative amounts', { key });
        const measured = await dialogGeometry(page);
        qa.check(report, measured.overflow <= 1 && measured.right <= width && measured.bottom <= 900 && measured.unreachable.length === 0, 'Receipt warning and costs preserve usable account-details geometry', { key, measured });
        await qa.screenshot(page, report, output, `${key}-details.png`);
        await page.evaluate(() => { window.__betaFixture.account.receiptError = ''; });
        await page.waitForFunction(() => document.querySelectorAll('[data-receipt-warning]').length === 0);
        qa.check(report, await details.isVisible() && await page.evaluate(() => window.__betaFixture.account.authenticated && !window.__betaFixture.calls.includes('logout')), 'Successful journal recovery clears both warnings without closing details or signing out', { key });
        await qa.screenshot(page, report, output, `${key}-cleared.png`);
        await page.keyboard.press('Escape');
        qa.check(report, await account.getByRole('button', { name: labels(language).logout, exact: true }).isEnabled() && await page.locator('[data-receipt-warning]').count() === 0, 'Primary account remains usable after warning recovery', { key });
        qa.check(report, JSON.stringify(await geometry(page)) === JSON.stringify(before), 'Receipt warnings and recovery leave all existing shell and Composer controls unchanged', { key });
        const callsBeforeExpiry = await page.evaluate(() => ({ login: window.__betaFixture.calls.filter(c => c === 'login').length, logout: window.__betaFixture.calls.filter(c => c === 'logout').length }));
        await page.evaluate(() => { window.__betaFixture.account = { state: 'error', authenticated: false, needsSignOut: true, displayName: '', error: 'session expired' }; });
        await account.getByRole('alert').filter({ hasText: language === 'zh' ? '账号登录已过期' : 'Your account session expired' }).waitFor();
        qa.check(report, await account.getByRole('button').count() === 1 && await account.getByRole('button', { name: labels(language).logout, exact: true }).isEnabled(), 'Expired account with retained controllers keeps one usable logout button', { key });
        qa.check(report, await page.getByRole('button', { name: detailsName }).count() === 0, 'Expired retained-controller state does not expose private account details', { key });
        const expiredGeometry = await dialogGeometry(page);
        qa.check(report, expiredGeometry.overflow <= 1 && expiredGeometry.right <= width && expiredGeometry.bottom <= 900 && expiredGeometry.unreachable.length === 0, 'Expiry recovery uses the same contained account dialog layout', { key, expiredGeometry });
        await qa.screenshot(page, report, output, `${key}-expired.png`);
        await account.getByRole('button', { name: labels(language).logout, exact: true }).click();
        const signIn = account.getByRole('button', { name: language === 'zh' ? '使用ORCA账号登录' : 'Sign in with ORCA', exact: true });
        await signIn.waitFor();
        qa.check(report, await page.evaluate(before => window.__betaFixture.calls.filter(c => c === 'logout').length === before.logout + 1 && window.__betaFixture.calls.filter(c => c === 'login').length === before.login && !window.__betaFixture.account.needsSignOut, callsBeforeExpiry), 'Expiry recovery dispatches logout, not a rejected login, and clears retained-controller state', { key });
        await signIn.click();
        await account.getByRole('button', { name: labels(language).cancel, exact: true }).waitFor();
        qa.check(report, await page.evaluate(before => window.__betaFixture.calls.filter(c => c === 'login').length === before.login + 1 && window.__betaFixture.account.state === 'signing_in', callsBeforeExpiry), 'Signing in becomes available after explicit expiry cleanup', { key });
        await qa.screenshot(page, report, output, `${key}-relogin.png`);
        await account.getByRole('button', { name: labels(language).cancel, exact: true }).click();
        await signIn.waitFor();
        qa.check(report, JSON.stringify(await geometry(page)) === JSON.stringify(before), 'Expiry/logout/relogin leaves all old shell and Composer geometry unchanged', { key });
        await context.close();
      }
    }
  } catch (error) { report.failures.push({ name: 'Unhandled test error', message: error.stack }); console.error(error); }
  finally { await browser.close(); }
  await qa.finish(report, output, baselineOnly ? 'baseline.json' : 'report.json');
}

module.exports = { betaPage, openConnections, dialogGeometry, geometry, labels };
if (require.main === module) main().catch(error => { console.error(error); process.exitCode = 1; });
