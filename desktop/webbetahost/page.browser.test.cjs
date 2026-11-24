// Offline DOM regressions. All URLs are fulfilled locally; no website requests.
const { chromium } = require('playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');

const site = fs.readFileSync(path.join(__dirname, 'site.js'), 'utf8');
const adapter = fs.readFileSync(path.join(__dirname, 'page.js'), 'utf8').replaceAll('__ORCA_DOCUMENT_ID__', 'fixture-generation');
const dispatch = fs.readFileSync(path.join(__dirname, 'dispatch.js'), 'utf8')
  .replace('__ORCA_COMMAND__', JSON.stringify({ opening:true, id: 'wrapper-send', text: 'Synthetic request', thinking: false, search: false }))
  .replace('__ORCA_GENERATION__', JSON.stringify('fixture-generation'));
const sendPath = 'M8.3125 0.980206';
const stopPath = 'M2 4.88C2 3.68009';

async function fixture(browser, scenario) {
  const context = await browser.newContext();
  await context.route('**/*', route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><html><body></body></html>' }));
  const page = await context.newPage();
  await page.goto('https://chat.deepseek.com/');
  await page.evaluate(({ scenario, sendPath, stopPath }) => {
    window.events = []; window.sends = 0;
    window.chrome = { webview: { postMessage: raw => window.events.push(JSON.parse(raw)) } };
    const attr = scenario.checked ? 'aria-checked' : 'aria-pressed';
    document.body.innerHTML = `<div id="composer"><textarea placeholder="DeepSeek"></textarea><input type="file"><div class="ds-button--circle" role="button"><svg><path d="${sendPath}" /></svg></div></div><button class="ds-toggle-button" ${attr}="true">深度思考</button><button class="ds-toggle-button" ${attr}="false">智能搜索</button><div id="responses"></div>`;
    for (const button of document.querySelectorAll('.ds-toggle-button')) button.onclick = () => button.setAttribute(attr, String(button.getAttribute(attr) !== 'true'));
    const primary = document.querySelector('.ds-button--circle');
    const icon = path => {
      if (scenario.attribute) primary.querySelector('path').setAttribute('d', path);
      else primary.innerHTML = `<svg><path d="${path}" /></svg>`;
    };
    const response = () => {
      const node = document.createElement('div'); node.className = 'ds-message';
      node.innerHTML = '<div class="ds-assistant-message-main-content">Synthetic final.</div>';
      document.querySelector('#responses').append(node); return node;
    };
    if (scenario.stale) response();
    primary.onclick = () => {
      if (primary.querySelector('path').getAttribute('d') === stopPath) { icon(sendPath); return; }
      window.sends++;
      document.querySelector('textarea').value = '';
      if (scenario.unknownIcon) icon('M0 0L1 1');
      else if (!scenario.noBusy) icon(stopPath);
      const finish = () => {
        if (!scenario.stale) {
          const node = response();
          if (scenario.replace) setTimeout(() => { node.remove(); response(); }, 180);
          if (scenario.growing) {
            for (const delay of [100, 300, 500, 700]) setTimeout(() => {
              node.querySelector('.ds-assistant-message-main-content').textContent += ' more';
              window.lastGrowth = performance.now();
            }, delay);
          }
        }
        if (scenario.ambiguous) response();
        icon(sendPath);
      };
      if (scenario.fast || (scenario.noBusy && !scenario.unknownIcon)) finish(); else setTimeout(finish, 150);
    };
  }, { scenario, sendPath, stopPath });
  await page.addScriptTag({ content: site });
  await page.addScriptTag({ content: adapter });
  if (!scenario.noRun) await page.evaluate(() => { window.run = window.__orcaWebBeta.run({ opening:true, id: 'owned-send', text: 'Synthetic request', thinking: false, search: false }); });
  return { page, context };
}

(async () => {
  const browser = await chromium.launch({ headless: true, ...(process.env.ORCA_TEST_BROWSER_CHANNEL ? { channel: process.env.ORCA_TEST_BROWSER_CHANNEL } : {}) });
  let failures = 0;
  try {
    for (const [name, setup, expected] of [
      ['missing adapter wrapper', 'missing', 'adapter_missing'],
      ['generation mismatch wrapper', 'stale', 'adapter_generation_mismatch'],
      ['rejected dispatch wrapper', 'reject', 'dispatch_rejected'],
      ['successful wrapper', 'normal', null],
      ['stop before wrapper dispatch', 'revoked', 'dispatch_revoked'],
    ]) {
      const { page, context } = await fixture(browser, { noRun: true });
      try {
        if (setup === 'missing' || setup === 'reject') {
          await page.goto('https://chat.deepseek.com/fixture');
          await page.evaluate(kind => {
            window.events = []; window.sends = 0;
            window.chrome = { webview: { postMessage: raw => window.events.push(JSON.parse(raw)) } };
            if (kind === 'reject') window.__orcaWebBeta = { generation: 'fixture-generation', run: () => Promise.reject(new Error('SECRET BODY MUST NOT LEAK')) };
          }, setup);
        }
        if (setup === 'stale') await page.evaluate(() => window.__orcaWebBeta.rebind('other-generation'));
        if (setup === 'revoked') await page.evaluate(() => window.__orcaWebBeta.stop('wrapper-send'));
        await page.addScriptTag({ content: dispatch });
        await page.waitForTimeout(1200);
        const result = await page.evaluate(() => ({ events: window.events, sends: window.sends }));
        assert.equal(result.sends, expected ? 0 : 1);
        assert.ok(!JSON.stringify(result.events).includes('SECRET'));
        if (expected) assert.ok(result.events.some(e => e.kind === 'error' && e.code === expected));
        else {
          const phases = result.events.filter(e => e.kind === 'phase').map(e => e.code);
          assert.deepEqual(phases.slice(0, 5), ['dispatch_ack', 'modes_checked', 'uploads_ready', 'input_set', 'send_attempted']);
          assert.ok(phases.includes('generation_observed') && phases.includes('response_seen'));
          assert.equal(result.events.filter(e => e.kind === 'done').length, 1);
        }
        console.log(`PASS ${name}`);
      } catch (error) { failures++; console.error(`FAIL ${name}: ${error.message}`); }
      finally { await context.close(); }
    }
    for (const name of ['duplicate document injection', 'stop before delayed dispatch', 'rebind cancels old dispatch without relabeling events']) {
      const { page, context } = await fixture(browser, { noRun: true });
      try {
        if (name === 'duplicate document injection') {
          await page.addScriptTag({ content: adapter.replaceAll('fixture-generation', 'next-generation') });
          await page.evaluate(() => { window.events = []; window.__orcaWebBeta.recheck('check'); });
          const events = await page.evaluate(() => window.events);
          assert.equal(events.find(e => e.kind === 'rechecked')?.generation, 'next-generation');
        } else if (name === 'stop before delayed dispatch') {
          await page.evaluate(async () => {
            await window.__orcaWebBeta.stop('late-send');
            await window.__orcaWebBeta.run({ id: 'late-send', text: 'Synthetic request', thinking: false, search: false });
          });
          assert.equal(await page.evaluate(() => window.sends), 0);
        } else {
          await page.evaluate(() => { window.run = window.__orcaWebBeta.run({ opening:true, id: 'old-send', text: 'Synthetic request', thinking: false, search: false }); });
          await page.addScriptTag({ content: adapter.replaceAll('fixture-generation', 'next-generation') });
          await page.evaluate(() => window.run);
          const events = await page.evaluate(() => window.events);
          assert.ok(events.some(e => e.id === 'old-send' && e.kind === 'error' && e.generation === 'fixture-generation'));
          assert.ok(!events.some(e => e.id === 'old-send' && e.generation === 'next-generation'));
          assert.equal(await page.evaluate(() => window.sends), 0);
        }
        console.log(`PASS ${name}`);
      } catch (error) { failures++; console.error(`FAIL ${name}: ${error.message}`); }
      finally { await context.close(); }
    }
    for (const [name, scenario, success, expectedError] of [
      ['normal observed generation', {}, true],
      ['short reply before first poll', { fast: true }, true],
      ['short reply with path attribute mutations', { fast: true, attribute: true }, true],
      ['aria-checked modes', { checked: true }, true],
      ['short reply and aria-checked together', { checked: true, fast: true }, true],
      ['text without generation evidence', { noBusy: true }, false, 'completion_unconfirmed'],
      ['missing send icon is not generation evidence', { noBusy: true, unknownIcon: true }, false, 'completion_unconfirmed'],
      ['old assistant never completes new send', { stale: true }, false],
      ['ambiguous new assistants fail closed', { ambiguous: true }, false, 'ambiguous_response'],
      ['new assistant identity replacement fails closed', { replace: true }, false, 'response_identity_changed'],
      ['completion waits for final text to settle', { growing: true }, true],
    ]) {
      const { page, context } = await fixture(browser, scenario);
      try {
        if(success || expectedError) await page.waitForFunction(() => window.events.some(e=>e.kind==='done'||e.kind==='error'),null,{timeout:6000});
        else await page.waitForTimeout(1600);
        const result = await page.evaluate(() => ({ events: window.events, sends: window.sends }));
        assert.equal(result.events.filter(e => e.kind === 'done').length, success ? 1 : 0);
        assert.equal(result.sends, 1, 'never replay the send');
        if (success) assert.equal(result.events.filter(e => e.kind === 'error').length, 0);
        if (expectedError) assert.ok(result.events.some(e => e.kind === 'error' && e.code === expectedError && e.sent === true));
        if (scenario.checked) assert.equal(await page.locator('.ds-toggle-button').first().getAttribute('aria-checked'), 'false');
        if (scenario.growing) assert.equal(result.events.filter(e => e.kind === 'text').map(e => e.text).join(''), 'Synthetic final. more more more more');
        await page.evaluate(async () => { await window.__orcaWebBeta.stop('owned-send'); await window.run; });
        console.log(`PASS ${name}`);
      } catch (error) { failures++; console.error(`FAIL ${name}: ${error.message}`); }
      finally { await context.close(); }
    }
  } finally { await browser.close(); }
  if (failures) process.exitCode = 1;
})().catch(error => { console.error(error); process.exitCode = 1; });
