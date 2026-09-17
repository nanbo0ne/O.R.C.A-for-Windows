// Uses the real Settings modal and ProviderEditor through the application shell.
// NODE_PATH=<bundled node_modules> node provider-editor.browser.cjs <local Vite URL> <evidence directory>
const fs = require('node:fs/promises');
const { chromium, executablePath, configuration, newReport, check, appPage, panels, settle, screenshot, finish } = require('./transcript-layout.browser.cjs');

const existing = {
  name: 'existing-anthropic-fixture', kind: 'anthropic', builtIn: false, added: true,
  baseUrl: 'https://existing-provider.invalid/v1', modelsUrl: '', models: ['claude-fixture'], default: 'claude-fixture',
  apiKeyEnv: 'EXISTING_FIXTURE_KEY', keySet: true, balanceUrl: '', contextWindow: 100000,
  reasoningProtocol: '', supportedEfforts: [], defaultEffort: '',
};

async function openAccess(page) {
  await panels(page, 'modern', true, false);
  await page.getByRole('button', { name: 'Settings', exact: true }).filter({ visible: true }).click();
  const dialog = page.locator('.settings-modal');
  await dialog.waitFor();
  await dialog.locator('.settings-center__navitem').filter({ has: page.locator('span', { hasText: /^Models$/ }) }).click();
  await dialog.getByRole('button', { name: 'Access', exact: true }).click();
  await dialog.locator('.provider-access-grid').waitFor();
  await settle(page);
  return dialog;
}
async function createProvider(page, report, dialog, name, expectedKind) {
  await dialog.getByRole('button', { name: '+ Add provider', exact: true }).click();
  await dialog.getByRole('tab', { name: 'Custom provider', exact: true }).click();
  const editor = dialog.locator('.provider-editor--wizard');
  check(report, await editor.locator('[aria-readonly="true"] strong').textContent() === 'OpenAI-compatible', 'New provider displays the fixed OpenAI-compatible protocol');
  const inputs = editor.locator(':scope > input');
  await inputs.nth(0).fill(name);
  await inputs.nth(1).fill('https://provider-fixture.invalid/v1');
  await editor.locator('input[type="password"]').fill('synthetic-ui-test-key');
  check(report, await editor.locator('.prov-card__actions .btn--primary').isDisabled(), 'New provider cannot save until it has models');
  await editor.getByRole('button', { name: 'Test and fetch models', exact: true }).click();
  await editor.locator('.provider-fetch-status--ok').waitFor();
  const fetched = await page.evaluate(name => window.__browserRegression.calls.fetch.filter(call => call.name === name), name);
  check(report, fetched.length === 1 && fetched[0].kind === expectedKind, 'FetchProviderModels receives the actual intended protocol', { name, expectedKind, fetched });
  check(report, fetched[0]?.baseUrl === 'https://provider-fixture.invalid/v1' && fetched[0]?.apiKeyEnv === `${name.toUpperCase().replaceAll('-', '_')}_API_KEY`, 'Fetch receives the entered URL and derived key environment', { fetched });
  check(report, await editor.locator('.provider-model-chip').count() === 2, 'Fetched models are rendered in the real provider wizard');
  if (expectedKind === 'openai') await screenshot(page, report, configuration().output, 'provider-new-openai.png');
  await editor.locator('.prov-card__actions .btn--primary').click();
  await editor.waitFor({ state: 'detached' });
  const saved = await page.evaluate(name => window.__browserRegression.calls.save.filter(call => call.name === name), name);
  check(report, saved.length === 1 && saved[0].kind === expectedKind, 'SaveProvider receives the actual intended protocol', { name, expectedKind, saved });
  check(report, saved[0]?.models.length === 2 && saved[0]?.default === saved[0]?.models[0], 'Save preserves fetched models and selected default', { saved });
  const card = dialog.locator('.provider-access-card').filter({ hasText: name });
  await card.waitFor();
  check(report, await card.locator('.provider-access-meta').innerText().then(text => text.includes(expectedKind)), 'Saved provider card renders the persisted protocol', { name, expectedKind });
  return { fetched, saved };
}

async function run() {
  const { url, output } = configuration();
  const report = newReport('provider-editor');
  await fs.mkdir(output, { recursive: true });
  const browser = await chromium.launch({ headless: true, executablePath });
  try {
    for (const oldProviderDefault of [false, true]) {
      const { page, context } = await appPage(browser, report, { url, providerKinds: ['anthropic', 'openai'], providers: [existing], oldProviderDefault });
      const dialog = await openAccess(page);
      check(report, await page.evaluate(() => window.__browserRegression.calls.settings.every(call => JSON.stringify(call.providerKinds) === '["anthropic","openai"]')), 'Settings supplies the production-like sorted registry [anthropic, openai]');
      const name = oldProviderDefault ? 'negative-default-fixture' : 'new-openai-fixture';
      const payloads = await createProvider(page, report, dialog, name, oldProviderDefault ? 'anthropic' : 'openai');
      report.cases.push({ case: oldProviderDefault ? 'old-default-negative-control' : 'new-provider', ...payloads });
      if (oldProviderDefault) {
        report.negativeControls.push({ kind: 'old-provider-kind-initializer', visibleLabel: 'OpenAI-compatible', ...payloads });
        check(report, payloads.saved[0]?.kind !== 'openai' && payloads.fetched[0]?.kind !== 'openai', 'Negative control reproduces both mismatched Anthropic payloads beneath the OpenAI label');
      } else {
        const card = dialog.locator('.provider-access-card').filter({ hasText: existing.name });
        await card.getByRole('button', { name: 'Configure', exact: true }).click();
        const editor = card.locator('.provider-editor');
        const protocol = editor.locator(':scope > select');
        check(report, await protocol.inputValue() === 'anthropic', 'Existing Anthropic edit retains its stored protocol');
        await editor.locator(':scope > input').nth(1).fill('https://edited-provider.invalid/v1');
        const count = await page.evaluate(() => window.__browserRegression.calls.fetch.length);
        await editor.getByRole('button', { name: 'Test and fetch models', exact: true }).click();
        await editor.locator('.provider-fetch-status--ok').waitFor();
        const fetched = await page.evaluate(count => window.__browserRegression.calls.fetch.slice(count), count);
        check(report, fetched.length === 1 && fetched[0].kind === 'anthropic' && fetched[0].baseUrl === 'https://edited-provider.invalid/v1', 'Existing Anthropic fetch retains kind and sends edited URL', { fetched });
        await editor.locator('.prov-card__actions .btn--primary').click();
        await editor.waitFor({ state: 'detached' });
        const saved = await page.evaluate(name => window.__browserRegression.calls.save.filter(call => call.name === name), existing.name);
        check(report, saved.length === 1 && saved[0].kind === 'anthropic' && saved[0].baseUrl === 'https://edited-provider.invalid/v1', 'Existing Anthropic save never silently converts to OpenAI', { saved });
        report.cases.push({ case: 'existing-anthropic-edit', fetched, saved });
        await card.getByRole('button', { name: 'Configure', exact: true }).click();
        check(report, await card.locator('.provider-editor > select').inputValue() === 'anthropic', 'Reopening persisted Anthropic provider retains protocol');
      }
      await context.close();
    }
  } catch (error) {
    check(report, false, 'Provider browser run completed', { error: error.stack });
  } finally {
    await browser.close();
    await finish(report, output, 'provider-editor-results.json');
  }
}
run().catch(error => { console.error(error); process.exitCode = 1; });
