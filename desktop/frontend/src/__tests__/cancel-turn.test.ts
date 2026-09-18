import assert from "node:assert/strict";
import { mock, test } from "node:test";
import { readFileSync } from "node:fs";
import { cancelAndObserve, startTurnStatusPolling } from "../lib/cancelTurn";
import { initialState, reducer } from "../lib/useController";
import type { TurnStatus } from "../lib/types";

let state = reducer(initialState, { type: "user", text: "test", seq: 0 });
state = reducer(state, { type: "cancel_requested" });
assert.equal(state.running, true, "requesting stop must not pretend the backend stopped");
state = reducer(state, { type: "event", e: { kind: "turn_started", turnId: "a" } });
assert.equal(state.cancelRequested, true, "late start receipt must preserve requested stop");
state = reducer(state, { type: "turn_status", status: { running: false, turnId: "a", outcome: "cancelled" } });
assert.equal(state.running, false);
assert.equal(state.cancelRequested, false);
assert.equal(state.items.some((item) => item.kind === "turn_stats" && item.outcome === "cancelled"), true);
state = reducer(state, { type: "user", text: "next", seq: state.seq });
const pendingNext = state;
for (const turnId of ["a", undefined]) {
  state = reducer(pendingNext, { type: "event", e: { kind: "turn_done", turnId, outcome: "cancelled" } });
  assert.equal(state.running, true, "late completion cannot stop a pending successor without an ID");
  assert.equal(state.pendingUser, "next");
}
state = reducer(pendingNext, { type: "turn_status", status: { running: false, turnId: "a" } });
assert.equal(state.running, true, "unscoped stale status cannot finish a pending submission");
state = reducer(pendingNext, { type: "turn_status", turnEpoch: pendingNext.turnEpoch - 1, status: { running: false } });
assert.equal(state, pendingNext, "previous submission's status cannot stop its successor");
state = reducer(state, { type: "event", e: { kind: "turn_started", turnId: "b" } });
state = reducer(state, { type: "turn_status", status: { running: false, turnId: "a", outcome: "cancelled" } });
assert.equal(state.running, true, "late status cannot stop successor");

for (const running of [true, false]) {
  const stopped = reducer({ ...state, running, cancelRequested: true, cancelSlow: true }, { type: "backend_status", running: false });
  assert.equal(stopped.running, false);
  assert.equal(stopped.cancelRequested, false, "backend idle clears stop request");
  assert.equal(stopped.cancelSlow, false, "backend idle clears waiting feedback");
}
const failedSend = reducer({ ...pendingNext, cancelRequested: true, cancelSlow: true }, { type: "send_failed", error: "offline" });
assert.equal(failedSend.cancelRequested, false);
assert.equal(failedSend.cancelSlow, false);
const cancelledBeforeStart = reducer({ ...pendingNext, cancelRequested: true }, {
  type: "turn_status", turnEpoch: pendingNext.turnEpoch,
  status: { running: false, turnId: "queued-tab-2", outcome: "cancelled" },
});
assert.equal(cancelledBeforeStart.running, false, "authoritative cancellation can finish an admitted queue without turn_started");
assert.equal(cancelledBeforeStart.pendingUser, undefined);
assert.equal(cancelledBeforeStart.items.filter((item) => item.kind === "user" && item.text === "next").length, 1);

let queued = reducer(initialState, { type: "user", text: "queued", seq: 0 });
const queuedEpoch = queued.turnEpoch;
queued = reducer(queued, { type: "cancel_requested" });
queued = reducer(queued, { type: "turn_status", turnEpoch: queuedEpoch, status: { running: true, turnId: "queued-tab-1" } });
let queuePolls = 0;
await cancelAndObserve({
  turnId: "queued-tab-1",
  status: async () => ({ running: ++queuePolls < 3, turnId: queuePolls === 1 ? "queued-tab-1" : "real-1", outcome: queuePolls === 3 ? "cancelled" : undefined }),
  cancel: async (id) => {
    assert.equal(id, "queued-tab-1");
    return { accepted: true, running: true, turnId: "real-1", cancelRequested: true };
  },
  current: () => true,
  update: (status, replacedTurnId) => { queued = reducer(queued, { type: "turn_status", status, replacedTurnId, turnEpoch: queuedEpoch }); },
  slow: () => assert.fail("queue transition should finish promptly"),
  error: (error) => { throw error; },
  delay: async () => {},
});
assert.equal(queuePolls, 3, "polling follows the actual ID returned in accepted ack");
assert.equal(queued.running, false);
assert.equal(queued.cancelRequested, false);
assert.ok(queued.items.some((item) => item.kind === "turn_stats" && item.turnId === "real-1" && item.outcome === "cancelled"));

// Use the real scheduling path with virtual timers: no five-second wall wait.
if (Number(process.versions.node.split(".")[0]) === 18) {
  mock.timers.enable(["setTimeout"] as unknown as Parameters<typeof mock.timers.enable>[0]);
} else {
  mock.timers.enable({ apis: ["setTimeout"] });
}
try {
  let admit!: () => void;
  let waiting = 0;
  let reads = 0;
  let cancellations = 0;
  const observed: TurnStatus[] = [];
  const stopping = cancelAndObserve({
    admission: new Promise<void>((resolve) => { admit = resolve; }),
    status: async () => ({ running: ++reads < 2, turnId: "admitted", outcome: reads === 2 ? "cancelled" : undefined }),
    cancel: async (id) => { cancellations++; return { accepted: true, running: true, turnId: id }; },
    current: () => true,
    update: (status) => { observed.push(status); },
    slow: () => { waiting++; },
    error: (error) => { throw error; },
  });
  mock.timers.tick(4999);
  assert.equal(waiting, 0);
  mock.timers.tick(1);
  assert.equal(waiting, 1, "waiting feedback starts five seconds after stop, even before Submit resolves");
  assert.equal(reads, 0);
  assert.equal(cancellations, 0);
  assert.equal(observed.length, 0, "pending admission never fabricates idle");
  admit();
  for (let i = 0; i < 20; i++) await Promise.resolve();
  assert.equal(cancellations, 1);
  assert.equal(observed[observed.length - 1]?.running, true, "accepted ack is not completion");
  mock.timers.tick(499);
  await Promise.resolve();
  assert.equal(reads, 1, "poll must wait 500ms");
  mock.timers.tick(1);
  await stopping;
  assert.equal(reads, 2);
  assert.equal(observed[observed.length - 1]?.running, false);
} finally {
  mock.timers.reset();
}

for (const running of [true, false]) {
  await cancelAndObserve({
    turnId: "old",
    status: async () => ({ running: true, turnId: "old" }),
    cancel: async () => ({ accepted: false, running, turnId: "successor" }),
    current: () => true,
    update: () => assert.fail("rejected stale ack must not update the successor"),
    slow: () => {}, error: () => {},
  });
}

let successorReads = 0;
let successorUpdates = 0;
await cancelAndObserve({
  turnId: "old",
  status: async () => ({ running: true, turnId: ++successorReads === 1 ? "old" : "next" }),
  cancel: async () => ({ accepted: true, running: true, turnId: "old" }),
  current: () => true,
  update: (status) => { successorUpdates++; assert.equal(status.turnId, "old"); },
  delay: async () => {}, slow: () => {}, error: (error) => { throw error; },
});
assert.equal(successorReads, 2, "observation stops when a successor appears");
assert.equal(successorUpdates, 1, "successor poll is never applied to the cancelled turn");

const composerSource = readFileSync(new URL("../components/Composer.tsx", import.meta.url), "utf8");
assert.match(composerSource, /const chooseEffortLevel = \(level: string\) => \{\s*if \(disabled \|\| running \|\| effortSaving\) return;/, "Classic secondary effort handler rejects changes while busy");
assert.match(composerSource, /onClick=\{\(\) => chooseEffortLevel\(level\)\}\s+disabled=\{disabled \|\| running \|\| effortSaving\}/, "Classic secondary effort options are disabled while busy");
const controllerSource = readFileSync(new URL("../lib/useController.ts", import.meta.url), "utf8");
assert.ok(controllerSource.includes("if (current?.effortPending || current?.running) return;"), "deferred menu callbacks recheck live running state before saving effort");

let statuses = 0;
let clock = 0;
let slow = false;
const updates: TurnStatus[] = [];
await cancelAndObserve({
  status: async () => ({ running: ++statuses < 14, turnId: "a", cancelRequested: true, outcome: statuses < 14 ? undefined : "cancelled" }),
  cancel: async (id) => { assert.equal(id, "a"); return { accepted: true, running: true, turnId: id, cancelRequested: true }; },
  current: () => true,
  update: (status) => updates.push(status),
  slow: () => { slow = true; },
  error: (error) => { throw error; },
  delay: async () => { clock += 500; },
  now: () => clock,
});
assert.equal(slow, true);
assert.equal(updates[updates.length - 1]?.running, false, "lost completion recovered by authoritative polling");

let current = true;
let cancelled = 0;
await cancelAndObserve({
  status: async () => { current = false; return { running: true, turnId: "new" }; },
  cancel: async () => { cancelled++; return { accepted: true, running: false }; },
  current: () => current,
  update: () => assert.fail("stale poll updated new turn"), slow: () => {}, error: () => {},
});
assert.equal(cancelled, 0);

let effort = reducer(initialState, { type: "effort", effort: { supported: true, current: "auto", default: "auto", levels: ["auto", "xhigh"] }, requestId: 1 });
effort = reducer(effort, { type: "effort_pending", requestId: 2 });
effort = reducer(effort, { type: "effort", effort: { ...effort.effort!, current: "auto" }, requestId: 3 });
effort = reducer(effort, { type: "effort_settled", requestId: 4, effort: { ...effort.effort!, current: "xhigh" } });
effort = reducer(effort, { type: "effort", effort: { ...effort.effort!, current: "auto" }, requestId: 3 });
assert.equal(effort.effort?.current, "xhigh", "stale read cannot undo successful save");
effort = reducer(effort, { type: "effort_pending", requestId: 5 });
effort = reducer(effort, { type: "effort_settled", requestId: 6 });
assert.equal(effort.effort?.current, "xhigh", "failed save preserves previous selection");
assert.equal(effort.effortPending, false);

await test("recurring status polling reconciles running tabs and early failures", async ({ mock }) => {
  if (Number(process.versions.node.split(".")[0]) === 18) {
    mock.timers.enable(["setTimeout"] as unknown as Parameters<typeof mock.timers.enable>[0]);
  } else {
    mock.timers.enable({ apis: ["setTimeout"] });
  }
  try {
    const running = (id: string) => reducer(reducer(initialState, { type: "user", text: id, seq: 0 }), { type: "event", e: { kind: "turn_started", turnId: id } });
    let early = reducer(initialState, { type: "user", text: "early failure", seq: 0 });
    early = reducer(early, { type: "event", e: { kind: "turn_done", turnId: "early-id", outcome: "failed", err: "Plan setup failed before agent start" } });
    assert.equal(early.running, true, "unconfirmed early terminal waits for backend identity");
    const states = new Map([
      ["plan", running("plan-id")], ["background", running("background-id")],
      ["pending", early], ["slow", running("old-id")], ["retry", running("retry-id")],
      ["identity", running("identity-old")],
      ["queue", reducer(initialState, { type: "user", text: "queued", seq: 0 })],
      ["idle", initialState],
    ]);
    const backend = new Map<string, TurnStatus>([
      ["plan", { running: true, turnId: "plan-id" }],
      ["background", { running: true, turnId: "background-id" }],
      ["pending", { running: false, turnId: "early-id", outcome: "failed" }],
      ["retry", { running: false, turnId: "retry-id", outcome: "failed" }],
      ["queue", { running: true, turnId: "queued-tab-9" }],
    ]);
    const reads = new Map<string, number>();
    let submitPending = true;
    let resolveSlow!: (status: TurnStatus) => void;
    let resolveIdentity!: (status: TurnStatus) => void;
    let resolveDisposed!: (status: TurnStatus) => void;
    let disposedUpdates = 0;
    const stopPolling = startTurnStatusPolling({
      tabs: () => states.keys(),
      state: (id) => states.get(id),
      pending: (id) => id === "pending" && submitPending,
      status: async (id) => {
        const count = (reads.get(id) ?? 0) + 1;
        reads.set(id, count);
        if (id === "slow") return new Promise<TurnStatus>((resolve) => { resolveSlow = resolve; });
        if (id === "identity") return new Promise<TurnStatus>((resolve) => { resolveIdentity = resolve; });
        if (id === "disposed") return new Promise<TurnStatus>((resolve) => { resolveDisposed = resolve; });
        if (id === "retry" && count === 1) throw new Error("temporary bridge failure");
        return backend.get(id)!;
      },
      update: (id, status, turnEpoch) => {
        if (id === "disposed") disposedUpdates++;
        states.set(id, reducer(states.get(id)!, { type: "turn_status", status, turnEpoch }));
      },
    });
    const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
    const tick = async () => { mock.timers.tick(500); await flush(); };
    try {
      await tick();
      assert.equal(reads.get("pending"), undefined, "never poll before pending Submit resolves");
      assert.equal(reads.get("idle"), undefined, "idle tabs do not poll");
      assert.equal(states.get("retry")?.running, true, "failed status read never invents idle");
      assert.equal(states.get("queue")?.currentTurnId, undefined, "normal polling does not bind a transient queue alias");
      backend.set("plan", { running: false, turnId: "plan-id", outcome: "success" });
      backend.set("background", { running: false, turnId: "background-id", outcome: "cancelled" });
      backend.set("queue", { running: false, turnId: "queue-real", outcome: "failed" });
      submitPending = false;
      await tick();
      assert.equal(states.get("plan")?.running, false, "lost plan TurnDone recovers on the next 500ms tick");
      assert.equal(states.get("background")?.running, false, "inactive running tabs recover too");
      assert.equal(states.get("retry")?.running, false, "read failures retry without a one-shot watchdog");
      assert.equal(states.get("queue")?.running, false, "queue-to-real transition recovers even with lost start and done events");
      assert.equal(reads.get("slow"), 1, "ticks never overlap a tab's in-flight status request");
      states.set("identity", reducer(states.get("identity")!, { type: "event", e: { kind: "turn_started", turnId: "identity-new" } }));
      resolveIdentity({ running: false, turnId: "identity-old" });
      await flush();
      assert.equal(states.get("identity")?.running, true, "identity changes reject old responses even within the same epoch");
      states.delete("identity");
      const failed = states.get("pending")!;
      assert.equal(failed.running, false);
      assert.ok(failed.items.some((item) => item.kind === "notice" && item.text === "Plan setup failed before agent start"), "confirmed pre-start failure replays its original error");
      assert.ok(failed.items.some((item) => item.kind === "turn_stats" && item.outcome === "failed"));
      states.set("slow", reducer(states.get("slow")!, { type: "user", text: "successor", seq: 2 }));
      resolveSlow({ running: false, turnId: "old-id", outcome: "cancelled" });
      await flush();
      assert.equal(states.get("slow")?.running, true, "old epoch's delayed idle response cannot finish a pending successor");
      states.delete("slow");
      states.set("disposed", running("disposed-id"));
      await tick();
      assert.equal(reads.get("plan"), 2, "completed tabs stop polling");
      stopPolling();
      resolveDisposed({ running: false, turnId: "disposed-id" });
      await flush();
      assert.equal(disposedUpdates, 0, "unmount ignores outstanding responses");
      await tick();
      assert.equal(reads.get("disposed"), 1, "unmount clears recurring timer");
    } finally {
      stopPolling();
    }
  } finally {
    mock.timers.reset();
  }
});

let mismatchedError = reducer(initialState, { type: "user", text: "new submission", seq: 0 });
mismatchedError = reducer(mismatchedError, { type: "event", e: { kind: "turn_done", turnId: "unknown-old", err: "stale error", outcome: "failed" } });
mismatchedError = reducer(mismatchedError, { type: "turn_status", turnEpoch: mismatchedError.turnEpoch, status: { running: true, turnId: "actual-new" } });
assert.equal(mismatchedError.running, true, "unmatched old terminal cannot finish a new turn");
mismatchedError = reducer(mismatchedError, { type: "turn_status", turnEpoch: mismatchedError.turnEpoch, status: { running: false, turnId: "actual-new", outcome: "cancelled" } });
assert.equal(mismatchedError.items.some((item) => item.kind === "notice" && item.text === "stale error"), false, "unmatched errors are not replayed onto a successor");
console.log("Cancellation reconciliation and effort sequencing passed");
