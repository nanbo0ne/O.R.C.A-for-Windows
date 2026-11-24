// Isolated rendering regression. No native bindings, model or personal data.
const { chromium } = require('playwright');
const { spawn } = require('node:child_process');
const fs = require('node:fs/promises');
const path = require('node:path');
const { appPage, events, settle, newReport, check } = require('./transcript-layout.browser.cjs');

async function run() {
  const frontend = path.resolve(__dirname, '../..');
  const output = path.resolve(process.argv[2] || path.join(frontend, '.tmp/process-terminal'));
  const report = newReport('process-terminal-label');
  await fs.mkdir(output, { recursive: true });
  let browser;
  const server = spawn(process.execPath, [path.join(frontend, 'node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '0'],
    { cwd: frontend, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
  try {
    const url = await new Promise((resolve, reject) => {
      let log = '';
      const timer = setTimeout(() => reject(new Error('Isolated Vite startup timed out')), 15000);
      server.once('error', error => { clearTimeout(timer); reject(error); });
      server.once('exit', code => { clearTimeout(timer); reject(new Error(`Vite exited: ${code}`)); });
      server.stderr.on('data', data => { log += data; });
      server.stdout.on('data', data => {
        log += data.toString().replace(/\u001b\[[0-9;]*m/g, '');
        const match = log.match(/http:\/\/127\.0\.0\.1:\d+/);
        if (match) { clearTimeout(timer); resolve(new URL(`${match[0]}/?platform=windows&mock=demo`)); }
      });
    });
    browser = await chromium.launch({ headless: true, executablePath: process.env.ORCA_BROWSER_EXECUTABLE || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe' });
    for (const style of ['modern', 'classic']) for (const language of ['zh', 'en']) {
      const { page, context } = await appPage(browser, report, { url, style, language,
        history: [{ role: 'user', content: 'Synthetic protocol check.' }] });
      try {
        await events(page, [{ kind: 'turn_started', turnId: 'synthetic-protocol' },
          { kind: 'notice', level: 'info', text: 'Synthetic pending status.' }]);
        const group = page.locator('.timeline-process-group').last();
        const label = group.locator('.timeline-process-group__label');
        check(report, (await label.innerText()) === (language === 'zh' ? '\u6b63\u5728\u601d\u8003' : 'Thinking'), 'Live heading remains live', { style, language });
        await events(page, [{ kind: 'notice', level: 'warn', text: 'Synthetic missing explicit turn record.' },
          { kind: 'turn_done', turnId: 'synthetic-protocol', outcome: 'failed', success: false }]);
        const text = await label.innerText();
        check(report, text === (language === 'zh' ? '\u5de5\u4f5c\u8fc7\u7a0b' : 'Work details'), 'Failed turn has a settled heading', { style, language, text });
        check(report, await group.locator('.process-card__spin').count() === 0, 'Failed turn has no live spinner', { style, language });
        const button = group.locator('.timeline-process-group__head');
        await button.click();
        check(report, await button.getAttribute('aria-expanded') === 'false', 'Failure details can collapse', { style, language });
        await button.click();
        await group.locator('.notice--warn .process-card__head').click();
        await settle(page);
        check(report, (await group.innerText()).includes('Synthetic missing explicit turn record.'), 'Failure evidence survives toggle', { style, language });
        const screenshot = path.join(output, `${style}-${language}-failed.png`);
        await page.screenshot({ path: screenshot });
        report.screenshots.push(screenshot);
        report.cases.push({ style, language, text });
      } finally { await context.close(); }
    }
  } catch (error) { check(report, false, 'Run completed', { error: error.stack }); }
  finally {
    if (browser) await browser.close();
    if (server.exitCode === null) {
      const closed = new Promise(resolve => server.once('exit', resolve));
      server.kill();
      await closed;
    }
    await fs.writeFile(path.join(output, 'results.json'), JSON.stringify(report, null, 2));
  }
  console.log(JSON.stringify({ assertions: report.assertions, cases: report.cases.length, failures: report.failures,
    pageErrors: report.pageErrors, externalRequests: report.externalRequests }));
  if (report.failures.length || report.pageErrors.length || report.externalRequests.length) process.exitCode = 1;
}
run().catch(error => { console.error(error); process.exitCode = 1; });
