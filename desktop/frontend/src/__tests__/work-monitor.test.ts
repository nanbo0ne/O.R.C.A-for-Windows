import assert from "node:assert/strict";
import { clampMonitorHeight, emptyMonitor, monitorCost, monitorViewportFits, nearEnd, observedLanes, phaseDuration, reduceMonitor } from "../lib/workMonitor";
import type { MonitorEntry, MonitorSnapshot } from "../lib/workMonitor";
import { workMonitorLabels } from "../lib/workMonitorLabels";

const entry = (seq: number, more: Partial<MonitorEntry> = {}): MonitorEntry => ({ generation: "g", seq, tabId: "a", turnId: "turn", time: seq * 1000, kind: "response", data: { channel: "decode", text: "chunk" }, ...more });
const snapshot = (entries: MonitorEntry[], more: Partial<MonitorSnapshot> = {}): MonitorSnapshot => ({ generation: "g", entries, cursor: entries[entries.length - 1]?.seq ?? 0, evicted: 0, dropped: 0, bytes: 0, expired: false, ...more });
let state = reduceMonitor(emptyMonitor("g"), snapshot([entry(1), entry(2), entry(3)]), "a");
assert.equal(state.entries.length, 1);
assert.equal(state.entries[0].seqEnd, 3);
assert.deepEqual(state.entries[0].data, { channel: "decode", text: "chunkchunkchunk" });
assert.equal(state.cursor, 3);
assert.equal(reduceMonitor(state, snapshot([entry(2)]), "a").entries.length, 1);
assert.equal(reduceMonitor(state, snapshot([entry(4)], { generation: "stale" }), "a"), state);
assert.equal(reduceMonitor(state, snapshot([entry(4)], { expired: true }), "a"), state);
assert.equal(reduceMonitor(state, snapshot([entry(4, { tabId: "b" })]), "a").entries.length, 1);

state = emptyMonitor("g");
for (let i = 1; i <= 1000; i++) {
  state = reduceMonitor(state, snapshot([entry(i, { kind: "request", requestId: `r${i}`, data: { body: { model: "fake", messages: [{ role: "user", content: "x".repeat(12000) }] } } })]), "a");
}
assert.ok(state.entries.length <= 100);
assert.ok(state.bytes <= 4 * 1024 * 1024);
assert.ok(state.localEvicted >= 900);
assert.equal(state.bytes, state.entries.reduce((n, e) => n + monitorCost(e), 0));
assert.equal(clampMonitorHeight(10000, 900), 585);
assert.equal(clampMonitorHeight(0, 900), 140);
assert.equal(clampMonitorHeight(320, 180), 117);
assert.equal(monitorViewportFits(180), false);
assert.equal(monitorViewportFits(480), false);
assert.equal(monitorViewportFits(560), true);
const partialState = reduceMonitor(emptyMonitor("g"), snapshot([entry(1), entry(2, {kind: "diagnostic", incomplete: true, data: { code: "retry_observed" }}), entry(3)]), "a");
assert.equal(partialState.entries.length, 3);
assert.equal(partialState.entries[1].incomplete, true);
assert.equal(phaseDuration(entry(1, { phase: "decode" }), entry(3), 9000), 2000);
assert.equal(phaseDuration(entry(1, { phase: "decode" }), undefined, 9000), 8000);
assert.equal(phaseDuration(entry(1, { phase: "stopped" }), undefined, 9000), 0);
assert.ok(nearEnd(1000, 855, 100, 48));
assert.ok(!nearEnd(1000, 855, 100, 24));
assert.deepEqual(observedLanes([
  entry(1, { kind: "tool", data: { id: "a", kind: "tool_dispatch" } }),
  entry(2, { kind: "tool", data: { id: "b", kind: "tool_dispatch" } }),
  entry(3, { kind: "tool", data: { id: "a", kind: "tool_result" } }),
  entry(4, { kind: "child", data: { id: "c", running: true } }),
]), { tools: 1, children: 1 });
assert.equal(workMonitorLabels("zh").name, "工作监控");
assert.equal(workMonitorLabels("en").name, "Work monitor");
console.log("work-monitor: reducer, 1000 requests, bounds, pairing, timing, independent follow, lanes and locales passed");
