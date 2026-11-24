const qa = require('./transcript-layout.browser.cjs');
const { betaPage, openConnections, dialogGeometry, geometry, labels } = require('./beta-connections.browser.cjs');
const fs = require('node:fs/promises');

(async () => {
  const { url, output } = qa.configuration();
  await fs.mkdir(output, { recursive: true });
  const report = qa.newReport('beta-account-switch');
  const browser = await qa.chromium.launch({ headless: true, executablePath: qa.executablePath });
  try {
    for (const style of ['modern', 'classic']) for (const language of ['zh', 'en']) for (const width of [760, 1366]) {
      const key = `${style}-${language}-${width}`;
      report.cases.push({ key, source: 'synthetic bridge, real React clicks' });
      const zh = language === 'zh', l = labels(language);
      const adopt = zh ? '使用当前登录' : 'Use current login';
      const open = zh ? '打开网页' : 'Open webpage';
      const { page, context } = await betaPage(browser, report, { url, style, language, width, browserAvailable: true });
      const before = await geometry(page);
      await page.evaluate(() => {
        const f = window.__betaFixture;
        f.web.blocked = 'send_unconfirmed'; f.web.browser.blocked = 'identity_changed';
        f.web.settings.enabled = false; f.web.sendAuthorized = true;
        f.switchDelay = 500; f.switchError = 'web_recheck_busy';
      });
      await openConnections(page, style, language, width);
      await page.getByRole('button', { name: l.webSettings, exact: true }).click();
      const dialog = page.getByRole('dialog', { name: l.webSettings, exact: true });
      qa.check(report, await dialog.getByRole('button', { name: adopt, exact: true }).isVisible(), 'Identity boundary offers current-login adoption, not forced logout', { key });
      await dialog.locator('input[type="range"]').fill('4');
      await dialog.getByRole('button', { name: open, exact: true }).click();
      await page.locator('[data-testid="beta-webview"]').waitFor();
      await page.waitForFunction(() => window.__betaFixture.bounds.some(b => b.visible));
      await page.keyboard.press('Escape');
      await dialog.waitFor();
      qa.check(report, await dialog.locator('input[type="range"]').inputValue() === '4', 'Opening the webpage preserves an unsaved settings draft', { key });
      await dialog.getByRole('button', { name: adopt, exact: true }).click();
      qa.check(report, await dialog.getByRole('button', { name: adopt, exact: true }).isDisabled() && await dialog.getByRole('button', { name: open, exact: true }).isDisabled(), 'Account transition serializes actions', { key });
      await dialog.getByRole('alert').waitFor();
      qa.check(report, await dialog.locator('[data-web-recovered]').count() === 0 && await page.evaluate(() => window.__betaFixture.web.blocked === 'send_unconfirmed'), 'Failed drain cannot display switch success or clear the old boundary', { key });
      await page.evaluate(() => { window.__betaFixture.switchError = ''; });
      await dialog.getByRole('button', { name: adopt, exact: true }).click();
      await dialog.locator('[data-web-recovered]').waitFor();
      qa.check(report, (await dialog.locator('[data-web-recovered]').textContent()).includes(zh ? '新建对话' : 'new conversation'), 'Success explains the fresh-context boundary', { key });
      qa.check(report, await page.evaluate(() => {
        const f = window.__betaFixture;
        return f.web.sendAuthorized && f.web.settings.enabled && f.web.settings.reservePaused && f.web.sent === 7 && f.calls.filter(c => c === 'useCurrentLogin').length === 2 && !f.calls.some(c => ['recover', 'webLogout', 'configure', 'logout', 'login'].includes(c));
      }), 'Only explicit adoption runs, without logout, reauthorization, model sends or other-account changes', { key });
      const measured = await dialogGeometry(page);
      qa.check(report, measured.overflow <= 1 && measured.right <= width && measured.unreachable.length === 0, 'Switch notice and actions fit the existing dialog', { key, measured });
      qa.check(report, JSON.stringify(before) === JSON.stringify(await geometry(page)), 'New account controls do not move the shell or Composer', { key });
      await qa.screenshot(page, report, output, `${key}.png`);
      await context.close();
    }
  } catch (error) { report.failures.push({ name: 'Unhandled test error', message: error.stack }); }
  finally { await browser.close(); }
  await qa.finish(report, output, 'report.json');
})().catch(error => { console.error(error); process.exitCode = 1; });
