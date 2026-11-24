// Synthetic native bindings; mounts the real component and clicks owner switches.
const qa = require('./transcript-layout.browser.cjs');
const fs = require('node:fs/promises');

(async () => {
  const { url, output } = qa.configuration();
  await fs.mkdir(output, { recursive: true });
  const report = qa.newReport('browser-owner-transitions');
  const browser = await qa.chromium.launch({ headless: true, executablePath: qa.executablePath });
  try {
    for (const style of ['modern', 'classic']) {
      const { page, context } = await qa.appPage(browser, report, { url, style, browserAvailable: true });
      await page.evaluate(async () => {
        const { default: React } = await import('/node_modules/.vite/deps/react.js');
        const { default: ReactDOM } = await import('/node_modules/.vite/deps/react-dom_client.js');
        const { LocaleProvider } = await import('/src/lib/i18n.tsx');
        const { BrowserPanel } = await import('/src/components/BrowserPanel.tsx');
        const fixture = window.__browserOwnerFixture = { calls: [], viewports: [], reads: [] };
        const state = owner => ({ supported: true, tabs: [{ id: owner + '-tab', title: owner,
          url: 'about:blank', zoom: 1 }], activeTabId: owner + '-tab', busy: false, downloads: [], uploads: [] });
        window.go.main.App.GetBrowserState = async owner => {
          fixture.reads.push(owner);
          await new Promise(resolve => setTimeout(resolve, 150));
          return state(owner);
        };
        window.go.main.App.BrowserCommand = async (owner, command) => {
          fixture.calls.push({ owner, ...command });
          if (command.tabId && command.tabId !== owner + '-tab') throw new Error('browser tab belongs to another conversation');
          return state(owner);
        };
        window.go.main.App.SetBrowserViewport = async (owner, viewport) => fixture.viewports.push({ owner, ...viewport });
        const mount = document.createElement('section');
        mount.id = 'owner-fixture';
        mount.style.cssText = 'position:fixed;inset:50px;z-index:1000;background:white;display:flex;flex-direction:column';
        document.body.append(mount);
        function Harness() {
          const [owner, setOwner] = React.useState('owner-a');
          return React.createElement(React.Fragment, null,
            React.createElement('button', { onClick: () => setOwner('owner-a') }, 'Select owner A'),
            React.createElement('button', { onClick: () => setOwner('owner-b') }, 'Select owner B'),
            React.createElement(BrowserPanel, { ownerTabId: owner, visible: true }));
        }
        window.__browserOwnerRoot = ReactDOM.createRoot(mount);
        window.__browserOwnerRoot.render(React.createElement(LocaleProvider, null, React.createElement(Harness)));
      });
      await page.waitForFunction(() => window.__browserOwnerFixture.calls.some(c => c.owner === 'owner-a'));
      await page.getByRole('button', { name: 'Select owner B', exact: true }).click();
      await page.waitForFunction(() => window.__browserOwnerFixture.calls.some(c => c.owner === 'owner-b' && c.tabId === 'owner-b-tab'));
      await page.getByRole('button', { name: 'Select owner A', exact: true }).click();
      await page.waitForTimeout(450);
      const result = await page.evaluate(() => ({ ...window.__browserOwnerFixture,
        text: document.querySelector('#owner-fixture').textContent }));
      qa.check(report, result.calls.every(c => c.op === 'select' && c.tabId === c.owner + '-tab'),
        'Every activation uses a state snapshot belonging to the selected owner', { style, result });
      qa.check(report, !result.text.includes('belongs to another'), 'Owner switch leaves no foreign-tab error', { style });
      qa.check(report, result.calls.length === 3, 'A-B-A switch activates once per owner transition', { style, result });
      report.cases.push({ style, result });
      await qa.screenshot(page, report, output, `${style}-owner-switch.png`);
      await page.evaluate(() => window.__browserOwnerRoot.unmount());
      await context.close();
    }
  } finally { await browser.close(); }
  await qa.finish(report, output, 'report.json');
})().catch(error => { console.error(error); process.exitCode = 1; });
