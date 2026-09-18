import assert from "node:assert/strict";
import { test } from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { ComposerDraftFlushError, loadComposerDraft, persistComposerDraft, registerComposerDraftFlusher } from "../lib/composerDraftPersistence";
import { INSTALLER_DRAFT_FLUSH_ACK, INSTALLER_DRAFT_FLUSH_REQUEST, installInstallerShutdownFlush } from "../lib/useInstallerShutdown";

class Runtime {
  readonly handlers = new Set<(...data: unknown[]) => void>();
  readonly acks: unknown[][] = [];
  EventsOn(name: string, callback: (...data: unknown[]) => void) {
    assert.equal(name, INSTALLER_DRAFT_FLUSH_REQUEST);
    this.handlers.add(callback);
    return () => { this.handlers.delete(callback); };
  }
  EventsEmit(name: string, ...data: unknown[]) {
    assert.equal(name, INSTALLER_DRAFT_FLUSH_ACK);
    this.acks.push(data);
  }
  request(id: unknown) { for (const callback of this.handlers) callback(id); }
  async waitForAck() {
    const deadline = performance.now() + 1500;
    while (this.acks.length === 0 && performance.now() < deadline) await delay(5);
    assert.equal(this.acks.length, 1, "one ack must arrive within the budget");
    return this.acks[0];
  }
}

test("flushes latest text, ready attachments and references before acknowledging", async (t) => {
  const previousStorage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, "localStorage", { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => { values.set(key, value); },
    removeItem: (key: string) => { values.delete(key); },
  } });
  t.after(() => {
    if (previousStorage) Object.defineProperty(globalThis, "localStorage", previousStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  });
  let text = "old render";
  const snapshot = () => ({ tabId: "active", text, attachments: [{ id: "image", path: "C:/temp/image.png", status: "ready", displayName: "image.png" }], workspaceRefs: [{ path: "C:/project" }], sessionRefs: [{ path: "C:/session.jsonl", title: "reference" }] });
  t.after(registerComposerDraftFlusher(() => persistComposerDraft(snapshot(), { strict: true })));
  let finish: () => void = () => {};
  t.after(registerComposerDraftFlusher(() => new Promise<void>((resolve) => { finish = resolve; })));
  const runtime = new Runtime();
  const dispose = installInstallerShutdownFlush(runtime);
  t.after(dispose);
  text = "latest unsent edit";
  runtime.request("request-1");
  await delay(0);
  assert.equal(runtime.acks.length, 0, "ack waits for every registered flusher");
  const draft = loadComposerDraft("active");
  assert.equal(draft?.text, text);
  assert.equal(draft?.attachments[0]?.path, "C:/temp/image.png");
  assert.equal(draft?.workspaceRefs[0]?.path, "C:/project");
  assert.equal(draft?.sessionRefs[0]?.title, "reference");
  finish();
  assert.deepEqual(await runtime.waitForAck(), ["request-1", true]);
});

test("waits briefly for attachments to finish and acknowledges only successful flush", async (t) => {
  let ready = false;
  let attempts = 0;
  t.after(registerComposerDraftFlusher(() => {
    attempts++;
    if (!ready) throw new ComposerDraftFlushError();
  }));
  const runtime = new Runtime();
  t.after(installInstallerShutdownFlush(runtime));
  runtime.request("attachments");
  await delay(40);
  assert.equal(runtime.acks.length, 0);
  ready = true;
  assert.deepEqual(await runtime.waitForAck(), ["attachments", true]);
  assert.ok(attempts >= 2);
});

test("unfinished attachments fail within the frontend budget", async (t) => {
  t.after(registerComposerDraftFlusher(() => { throw new ComposerDraftFlushError(); }));
  const runtime = new Runtime();
  t.after(installInstallerShutdownFlush(runtime));
  const started = performance.now();
  runtime.request("timeout");
  assert.deepEqual(await runtime.waitForAck(), ["timeout", false]);
  assert.ok(performance.now() - started < 1200);
});

test("a hung flusher times out and cannot send a late success", async (t) => {
  let finish: () => void = () => {};
  t.after(registerComposerDraftFlusher(() => new Promise<void>((resolve) => { finish = resolve; })));
  const runtime = new Runtime();
  t.after(installInstallerShutdownFlush(runtime));
  runtime.request("hung");
  assert.deepEqual(await runtime.waitForAck(), ["hung", false]);
  finish();
  await delay(0);
  assert.equal(runtime.acks.length, 1);
});

test("storage errors send a failure ack without retry or rejection leakage", async (t) => {
  let attempts = 0;
  t.after(registerComposerDraftFlusher(() => {
    attempts++;
    throw Object.assign(new Error("storage full"), { name: "QuotaExceededError" });
  }));
  const runtime = new Runtime();
  t.after(installInstallerShutdownFlush(runtime));
  runtime.request("storage-failure");
  assert.deepEqual(await runtime.waitForAck(), ["storage-failure", false]);
  assert.equal(attempts, 1);
});

test("ignores malformed and duplicate requests, cancels retries and unregisters on cleanup", async (t) => {
  let attempts = 0;
  t.after(registerComposerDraftFlusher(() => { attempts++; throw new ComposerDraftFlushError(); }));
  const runtime = new Runtime();
  const dispose = installInstallerShutdownFlush(runtime);
  for (const invalid of [null, {}, 12, "", "bad id", "a".repeat(129)]) runtime.request(invalid);
  assert.equal(attempts, 0);
  runtime.request("same-id");
  runtime.request("same-id");
  await delay(0);
  assert.equal(attempts, 1);
  dispose();
  dispose();
  runtime.request("after-cleanup");
  await delay(40);
  assert.equal(attempts, 1);
  assert.equal(runtime.handlers.size, 0);
  assert.equal(runtime.acks.length, 0);
});

test("suppresses stale and unmounted asynchronous acks", async (t) => {
  const finishers: Array<() => void> = [];
  t.after(registerComposerDraftFlusher(() => new Promise<void>((resolve) => { finishers.push(resolve); })));
  const runtime = new Runtime();
  const dispose = installInstallerShutdownFlush(runtime);
  t.after(dispose);
  runtime.request("old");
  runtime.request("new");
  finishers[0]();
  await delay(0);
  assert.equal(runtime.acks.length, 0);
  finishers[1]();
  assert.deepEqual(await runtime.waitForAck(), ["new", true]);
  runtime.request("unmounted");
  dispose();
  finishers[2]();
  await delay(0);
  assert.equal(runtime.acks.length, 1);
});

test("plain browser and incomplete runtime are no-ops; lost runtime ack is contained", async (t) => {
  installInstallerShutdownFlush()();
  installInstallerShutdownFlush({})();
  installInstallerShutdownFlush({ EventsOn: () => { throw new Error("should not subscribe without EventsEmit"); } })();
  let attempts = 0;
  t.after(registerComposerDraftFlusher(() => { attempts++; }));
  const runtime = new Runtime();
  runtime.EventsEmit = () => { throw new Error("webview closed"); };
  t.after(installInstallerShutdownFlush(runtime));
  runtime.request("runtime-gone");
  await delay(0);
  assert.equal(attempts, 1);
});
