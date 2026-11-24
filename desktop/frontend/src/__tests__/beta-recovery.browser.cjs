// Real React rendering with an isolated synthetic bridge. No native or model calls.
const qa = require('./transcript-layout.browser.cjs');
const { betaPage, openConnections, dialogGeometry, geometry, labels } = require('./beta-connections.browser.cjs');
const fs = require('node:fs/promises');

(async () => {
  const { url, output } = qa.configuration();
  await fs.mkdir(output, { recursive: true });
  const report = qa.newReport('beta-web-recovery');
  const browser = await qa.chromium.launch({ headless: true, executablePath: qa.executablePath });
  try {
    for (const style of ['modern', 'classic']) for (const language of ['zh', 'en']) {
      const key = `${style}-${language}`;
      report.cases.push({ key, width: 760, source: 'synthetic bridge, real React UI' });
      const zh = language === 'zh';
      const l = labels(language);
      const recover = zh ? '已处理，重新检查' : 'Recheck after resolving';
      const enable = zh ? '启用网页 Agent 接入' : 'Enable web agent access';
      const reserve = zh ? '暂停备用对话补充' : 'Pause reserve conversations';
      const consent = zh ? '允许 Agent 向 DeepSeek' : 'Allow agent tasks';
      const { page, context } = await betaPage(browser, report, { url, style, language, width: 760, browserAvailable: true });
      const before = await geometry(page);
      await page.evaluate(() => {
        const f = window.__betaFixture;
        f.web.blocked = 'synthetic_recovery_required';
        f.web.sendAuthorized = true;
        f.web.settings.enabled = false;
        f.web.settings.reservePaused = false;
        f.recoverDelay = 500;
      });
      await openConnections(page, style, language, 760);
      await page.getByRole('button', { name: l.webSettings, exact: true }).click();
      const dialog = page.getByRole('dialog', { name: l.webSettings, exact: true });
      await dialog.locator('input[type="range"]').fill('4');
      await page.evaluate(() => { window.__betaFixture.holdWebStatus = true; });
      await page.waitForFunction(() => window.__betaFixture.webStatusHeld === true);
      await dialog.getByRole('button', { name: recover, exact: true }).click();
      qa.check(report, await dialog.getByLabel(enable, { exact: true }).isDisabled(), 'Recovery prevents editing an obsolete draft while in flight', { key });
      await page.keyboard.press('Escape');
      qa.check(report, await dialog.isVisible(), 'Recovery cannot be detached mid-request through Escape', { key });
      await page.waitForFunction(() => window.__betaFixture.web.blocked === '');
      await qa.settle(page);
      const stillOpen = await dialog.isVisible();
      qa.check(report, stillOpen, 'Successful recovery stays in settings for review', { key });
      if (!stillOpen) { await context.close(); continue; }
      qa.check(report, await dialog.getByLabel(enable, { exact: true }).isChecked() && await dialog.getByLabel(reserve, { exact: true }).isChecked(), 'Recovery retains the saved grant and pauses reserve conversations', { key });
      qa.check(report, await dialog.locator('input[type="range"]').inputValue() === '2', 'Recovery restores saved settings rather than an unsaved draft', { key });
      qa.check(report, await dialog.locator('[data-web-recovered]').count() === 1 && await dialog.getByLabel(consent, { exact: false }).count() === 0, 'Recovery confirms retained consent without asking again', { key });
      await page.keyboard.press('Escape');
      await dialog.waitFor({ state: 'detached' });
      await page.evaluate(() => { const f = window.__betaFixture; f.holdWebStatus = false; f.releaseWebStatus(); });
      await qa.settle(page);
      await page.getByRole('button', { name: l.webSettings, exact: true }).click();
      qa.check(report, await dialog.getByLabel(enable, { exact: true }).isChecked() && await dialog.getByLabel(reserve, { exact: true }).isChecked(), 'A stale pre-recovery status cannot overwrite the authoritative saved grant', { key });
      qa.check(report, await dialog.getByRole('button', { name: l.save, exact: true }).isEnabled() && await dialog.getByLabel(consent, { exact: false }).count() === 0, 'A previous grant does not require duplicate consent', { key });
      const measured = await dialogGeometry(page);
      qa.check(report, measured.overflow <= 1 && measured.right <= 760 && measured.bottom <= 900 && measured.unreachable.length === 0, 'Recovery notice and controls remain reachable without overflow', { key, measured });
      await qa.screenshot(page, report, output, `${key}-recovered.png`);
      await dialog.getByLabel(reserve, { exact: true }).uncheck();
      qa.check(report, await dialog.getByRole('button', { name: l.save, exact: true }).isDisabled(), 'Resuming reserve conversations still requires independent consent', { key });
      await dialog.getByLabel(reserve, { exact: true }).check();
      await dialog.getByRole('button', { name: l.save, exact: true }).click();
      await dialog.waitFor({ state: 'detached' });
      qa.check(report, await page.evaluate(() => {
        const f = window.__betaFixture;
        return f.web.settings.enabled && f.web.settings.reservePaused && f.calls.filter(c => c === 'recover').length === 1 && f.calls.filter(c => c === 'configure').length === 1 && f.web.sent === 7 && !f.calls.includes('open');
      }), 'Saved authorization is retained; recovery and Save never send messages', { key });
      qa.check(report, JSON.stringify(before) === JSON.stringify(await geometry(page)), 'Recovery leaves shell and Composer geometry unchanged', { key });

      await page.evaluate(() => { const f = window.__betaFixture; f.web.blocked = 'synthetic_recovery_required'; f.failRecover = true; });
      await page.waitForTimeout(1700);
      await page.getByRole('button', { name: l.webSettings, exact: true }).click();
      await dialog.getByRole('button', { name: recover, exact: true }).click();
      await dialog.getByRole('alert').waitFor();
      qa.check(report, await dialog.isVisible() && await dialog.locator('[data-web-recovered]').count() === 0 && !(await dialog.textContent()).includes('SYNTHETIC_PRIVATE_ERROR'), 'Failed recovery stays open with a safe error, not a success notice', { key });
      qa.check(report, await dialog.getByRole('button', { name: l.save, exact: true }).isDisabled(), 'Blocked access cannot be re-saved as enabled', { key });
      await qa.screenshot(page, report, output, `${key}-failed.png`);
      for (const code of ['web_recheck_timeout', 'web_recheck_busy', 'web_recheck_adapter', 'web_recheck_signed_out', 'web_recheck_challenge', 'web_recheck_unavailable']) {
        await page.evaluate(code => { window.__betaFixture.failRecover = code; }, code);
        await dialog.getByRole('button', { name: recover, exact: true }).click();
        await page.waitForFunction(() => !document.querySelector('.beta-connections-settings button:disabled') || !document.querySelector('.beta-connections-settings input:disabled'));
        const error = await dialog.getByRole('alert').textContent();
        qa.check(report, error && !error.includes('web_recheck_') && error !== (zh ? '请求未能完成，请重试。' : 'The request could not be completed. Please try again.'), 'Known recovery failures have localized, actionable categories', { key, code, error });
      }
      const callsBeforeOpen = await page.evaluate(() => ({ sent: window.__betaFixture.web.sent, changes: window.__betaFixture.calls.filter(c => ['configure', 'recover', 'webLogout'].includes(c)).length }));
      await dialog.getByRole('button', { name: zh ? '打开网页' : 'Open webpage', exact: true }).click();
      await page.locator('[data-testid="beta-webview"]').waitFor();
      await page.waitForFunction(() => window.__betaFixture.calls.includes('open') && window.__betaFixture.bounds.some(b => b.visible));
      await page.keyboard.press('Escape');
      await dialog.waitFor();
      qa.check(report, await page.evaluate(before => {
        const f = window.__betaFixture;
        return f.web.sendAuthorized && f.web.sent === before.sent && f.calls.filter(c => ['configure', 'recover', 'webLogout'].includes(c)).length === before.changes;
      }, callsBeforeOpen), 'Opening the existing webpage returns to settings without recovery, logout, consent changes or sends', { key });
      const openMeasured = await dialogGeometry(page);
      qa.check(report, openMeasured.overflow <= 1 && openMeasured.right <= 760 && openMeasured.unreachable.length === 0, 'The new webpage action fits the settings controls', { key, measured: openMeasured });
      await qa.screenshot(page, report, output, `${key}-open-web-return.png`);
      await page.evaluate(() => { window.__betaFixture.failRecover = false; });
      await dialog.getByRole('button', { name: recover, exact: true }).click();
      await dialog.locator('[data-web-recovered]').waitFor();
      await page.keyboard.press('Escape');
      qa.check(report, await page.evaluate(() => window.__betaFixture.web.settings.enabled && window.__betaFixture.web.settings.reservePaused && window.__betaFixture.calls.filter(c => c === 'configure').length === 1), 'Closing after recovery retains existing consent without resending', { key });

      await page.getByRole('button', { name: l.webSettings, exact: true }).click();
      await dialog.getByLabel(enable, { exact: true }).uncheck();
      await dialog.getByRole('button', { name: l.save, exact: true }).click();
      await dialog.waitFor({ state: 'detached' });
      await page.evaluate(() => { window.__betaFixture.web.blocked = 'synthetic_recovery_required'; });
      await page.waitForTimeout(1700);
      await page.getByRole('button', { name: l.webSettings, exact: true }).click();
      await dialog.getByRole('button', { name: recover, exact: true }).click();
      await dialog.locator('[data-web-recovered]').waitFor();
      qa.check(report, !await dialog.getByLabel(enable, { exact: true }).isChecked(), 'Recovery cannot restore explicitly revoked consent', { key });
      await dialog.getByLabel(enable, { exact: true }).check();
      qa.check(report, await dialog.getByRole('button', { name: l.save, exact: true }).isDisabled() && !await dialog.getByLabel(consent, { exact: false }).isChecked(), 'A revoked grant requires a new explicit consent', { key });
      await page.keyboard.press('Escape');
      qa.check(report, await page.evaluate(() => !window.__betaFixture.web.sendAuthorized && !window.__betaFixture.web.settings.enabled && window.__betaFixture.web.sent === 7), 'Discarding a new opt-in draft never authorizes or sends', { key });
      await context.close();
    }
  } catch (error) { report.failures.push({ name: 'Unhandled test error', message: error.stack }); }
  finally { await browser.close(); }
  await qa.finish(report, output, 'report.json');
})().catch(error => { console.error(error); process.exitCode = 1; });
