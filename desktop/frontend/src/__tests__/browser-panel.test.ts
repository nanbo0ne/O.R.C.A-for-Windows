import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { browserTaskRunning, nextZoomFactor, normalizeAddress, normalizeBrowserState, uploadOrigin } from "../components/BrowserPanel";
import { setBrowserViewport, type BrowserUpload, type BrowserViewport } from "../lib/bridge";

const cases: Array<[string, string]> = [
  ["localhost:3000", "http://localhost:3000"],
  ["127.0.0.1:5173/path", "http://127.0.0.1:5173/path"],
  ["192.168.1.4", "http://192.168.1.4"],
  ["[::1]:8080", "http://[::1]:8080"],
  ["::1", "http://[::1]"],
  ["2001:4860:4860::8888", "https://[2001:4860:4860::8888]"],
  ["999.999.1.1", "https://999.999.1.1"],
  ["example.com", "https://example.com"],
  ["https://example.com/a", "https://example.com/a"],
  ["about:blank", "about:blank"],
];
for (const [input, expected] of cases) {
  const actual = normalizeAddress(input);
  if (actual !== expected) throw new Error(`normalizeAddress(${input}) returned ${actual}, expected ${expected}`);
}
console.log(`browser address normalization: ${cases.length} passed`);

const zoomCases: Array<[number, -1 | 1, number]> = [
  [0.5, -1, 0.5],
  [1, 1, 1.1],
  [1, -1, 0.9],
  [1.95, 1, 2],
  [2, 1, 2],
];
for (const [current, direction, expected] of zoomCases) {
  const actual = nextZoomFactor(current, direction);
  if (actual !== expected) throw new Error(`nextZoomFactor(${current}, ${direction}) returned ${actual}, expected ${expected}`);
}
console.log(`browser zoom clamping: ${zoomCases.length} passed`);

for (const state of ["idle", "succeeded", "failed", "cancelled", "taken_over", "completed", "complete", "canceled", "stopped", "done", "success", "error", "timedout", "timeout", "takenover"]) {
  if (browserTaskRunning(state)) throw new Error(`Terminal browser task state ${state} was reported as running`);
}
for (const state of ["awaiting_approval", "running", "stopping"]) {
  if (!browserTaskRunning(state)) throw new Error(`Active backend browser task state ${state} was not reported as running`);
}
console.log("browser task backend-state handling: 18 passed");

const uploadBase = { id: "u", tabId: "t", url: "", origin: "", documentId: "d", generation: 1, frameId: "f", backendNodeId: 1, multiple: false, accept: "", status: "pending", requiresApproval: true } satisfies BrowserUpload;
if (uploadOrigin({ ...uploadBase, url: "https://example.com/form" }) !== "https://example.com") throw new Error("Upload origin was not derived from its URL");
if (uploadOrigin({ ...uploadBase, url: "not a valid URL" }) !== "not a valid URL") throw new Error("Malformed upload URL was not safely represented");
console.log("upload origin malformed-input handling: 2 passed");

const nativeEmptyState = normalizeBrowserState({
  supported: true,
  tabs: [{ id: "native-tab", title: "", url: "about:blank", loading: false, canGoBack: false, canGoForward: false, zoom: 1 }],
  activeTabId: "native-tab",
  busy: false,
  downloads: null,
  uploads: null,
} as never);
if (nativeEmptyState.tabs.length !== 1 || nativeEmptyState.downloads.length !== 0 || nativeEmptyState.uploads.length !== 0) {
  throw new Error("Native state with nil optional collections was not normalized");
}
const failedNativeState = normalizeBrowserState({ supported: true, tabs: null, activeTabId: null, busy: false, downloads: null, uploads: null } as never);
if (failedNativeState.tabs.length || failedNativeState.downloads.length || failedNativeState.uploads.length || failedNativeState.activeTabId) {
  throw new Error("Incomplete native error state was not normalized");
}
console.log("native BrowserState collection normalization: 2 passed");

const savedWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
const shownViewport: BrowserViewport = { x: 10, y: 20, width: 600, height: 400, scale: 1, visible: true };
const hiddenViewport: BrowserViewport = { x: 0, y: 0, width: 0, height: 0, scale: 1, visible: false };
const flushDispatch = () => new Promise<void>((resolve) => setImmediate(resolve));
type ViewportCall = { owner: string; viewport: BrowserViewport };

function viewportFixture() {
  const starts: ViewportCall[] = [];
  const applied: ViewportCall[] = [];
  const release: Array<(error?: Error) => void> = [];
  let inflight = 0;
  let maxInflight = 0;
  const dispatch = (owner: string, viewport: BrowserViewport) => {
    starts.push({ owner, viewport });
    maxInflight = Math.max(maxInflight, ++inflight);
    return new Promise<void>((resolve, reject) => release.push((error) => {
      inflight--;
      if (error) { reject(error); return; }
      applied.push({ owner, viewport });
      resolve();
    }));
  };
  Object.defineProperty(globalThis, "window", { configurable: true, value: { go: { main: { App: { SetBrowserViewport: dispatch } } } } });
  return { starts, applied, release, dispatch, maxInflight: () => maxInflight };
}

try {
  // Controlled Wails-style concurrent dispatch: a late cleanup hide wins.
  const unordered = viewportFixture();
  const lateHide = unordered.dispatch("owner", hiddenViewport);
  const earlyShow = unordered.dispatch("owner", shownViewport);
  unordered.release[1](); await earlyShow;
  unordered.release[0](); await lateHide;
  assert.equal(unordered.applied[unordered.applied.length - 1]?.viewport.visible, false);

  for (const [name, updates] of [
    ["effect cleanup then replacement show", [["owner", hiddenViewport], ["owner", shownViewport]]],
    ["modal hide cannot be overtaken by an older show", [["owner", shownViewport], ["owner", hiddenViewport]]],
    ["unmount/remount and owner switch share one queue", [["old-owner", shownViewport], ["old-owner", hiddenViewport], ["new-owner", shownViewport]]],
  ] as Array<[string, Array<[string, BrowserViewport]>]>) {
    const fixture = viewportFixture();
    const pending = updates.map(([owner, viewport]) => setBrowserViewport(owner, viewport));
    for (let i = 0; i < pending.length; i++) {
      await flushDispatch();
      assert.equal(fixture.starts.length, i + 1, `${name}: dispatched before its predecessor completed`);
      assert.deepEqual(fixture.starts[i], { owner: updates[i][0], viewport: updates[i][1] });
      fixture.release[i]();
      await pending[i];
    }
    assert.equal(fixture.maxInflight(), 1, name);
    assert.deepEqual(fixture.applied, updates.map(([owner, viewport]) => ({ owner, viewport })), name);
  }

  const failed = viewportFixture();
  const first = setBrowserViewport("owner", shownViewport);
  const rejected = assert.rejects(first, /synthetic viewport failure/);
  const mutableHide = { ...hiddenViewport };
  const second = setBrowserViewport("owner", mutableHide);
  mutableHide.visible = true;
  mutableHide.width = 999;
  await flushDispatch();
  assert.equal(failed.starts.length, 1);
  failed.release[0](new Error("synthetic viewport failure"));
  await rejected;
  await flushDispatch();
  assert.equal(failed.starts.length, 2, "failure must not poison the next explicit viewport update");
  assert.deepEqual(failed.starts[1].viewport, hiddenViewport, "queued viewport must be an immutable snapshot");
  failed.release[1](); await second;
  assert.equal(failed.starts.length, 2, "failed presentation must not be retried");
  assert.deepEqual(failed.applied, [{ owner: "owner", viewport: hiddenViewport }]);

  const panelSource = readFileSync(new URL("../components/BrowserPanel.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(panelSource, /app\.SetBrowserViewport\(/, "component must not bypass viewport ordering");
  assert.equal(panelSource.match(/\bsetBrowserViewport\(ownerTabId,/g)?.length, 2, "both resize and cleanup must use the shared queue");
} finally {
  if (savedWindow) Object.defineProperty(globalThis, "window", savedWindow);
  else Reflect.deleteProperty(globalThis, "window");
}
console.log("native viewport ordering: controlled race, cleanup/show, modal hide, remount/owner switch, failure recovery and snapshot checks passed");
