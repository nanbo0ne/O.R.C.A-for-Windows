import assert from "node:assert/strict";
import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import { buildTimelineSegments } from "../lib/transcriptTimeline";
import { en } from "../locales/en";
import { zh } from "../locales/zh";

assert.equal(zh["compaction.title"], "\u4e0a\u4e0b\u6587\u5df2\u538b\u7f29");
assert.equal(zh["compaction.working"], "\u6b63\u5728\u538b\u7f29\u4e0a\u4e0b\u6587");
assert.equal(en["compaction.title"], "Context compacted");
assert.equal(en["compaction.working"], "Compacting context");

const text = "Repeat this instruction";
let correlated: typeof initialState = { ...initialState, currentTurnId: "one" };
for (const id of ["first", "second"]) correlated = reducer(correlated, {type:"steer_sent",text:"Same image @.orca/attachments/test.png",id,turnId:"one"});
correlated = reducer(correlated,{type:"event",e:{kind:"steer",text:"Same image @.orca/attachments/test.png",itemId:"second",messageId:"saved-second",turnId:"one"}});
correlated = reducer(correlated,{type:"steer_failed",id:"first"});
assert.equal(correlated.items.length,1);
assert.equal(correlated.items[0].id,"second","out-of-order image guidance acknowledgement matches its own submission");
assert.equal(correlated.items[0].messageId,"saved-second");
let state: typeof initialState = { ...initialState, currentTurnId: "one", running: true, turnActive: true };
state = reducer(state, { type: "steer_sent", text });
const optimisticID = state.items[0].id;
state = reducer(state, { type: "event", e: { kind: "steer", messageId: "first", turnId: "one", text } });
assert.equal(state.items.length, 1);
assert.equal(state.items[0].id, optimisticID, "acknowledgement preserves the mounted optimistic bubble");
assert.equal(state.items[0].messageId, "first");
state = reducer(state, { type: "steer_sent", text });
state = reducer(state, { type: "event", e: { kind: "steer", messageId: "second", turnId: "one", text } });
assert.equal(state.items.length, 2, "distinct IDs preserve identical guidance in one turn");
const beforeReplay = state;
state = reducer(state, { type: "event", e: { kind: "steer", messageId: "first", turnId: "one", text } });
assert.equal(state, beforeReplay, "only exact stable event IDs deduplicate");
state = reducer(state, { type: "event", e: { kind: "steer", messageId: "third", turnId: "two", text } });
assert.equal(state.items.length, 3, "same text in a later turn survives");

const history = historyMessagesToItems([
  { role: "compaction", messageId: "legacy", level: "legacy", content: "", summary: "Legacy summary" },
  { role: "user", messageId: "user", turnId: "one", content: "Start" },
  { role: "steer", messageId: "first", turnId: "one", content: text },
  { role: "steer", messageId: "second", turnId: "one", content: text },
  { role: "compaction", messageId: "boundary", turnId: "one", content: "", summary: "Summary", trigger: "auto", archive: "archive.txt" },
  { role: "assistant", messageId: "answer", turnId: "one", content: "Answer", reasoning: "Reasoning", final: true },
  { role: "turn_stats", content: "", turnId: "one", outcome: "success" },
], "fixture").items;
assert.deepEqual(history.slice(0, 5).map(item => item.id), ["legacy", "user", "first", "second", "boundary"]);
assert.ok(history[0].kind === "compaction" && history[0].legacy);
assert.ok(history[4].kind === "compaction" && !history[4].legacy && history[4].archive === "archive.txt");
const segments = buildTimelineSegments(history, false);
assert.equal(segments.filter(segment => segment.kind === "user").length, 1, "guidance does not create extra turns/actions");
assert.equal(segments.filter(segment => segment.kind === "steer").length, 2);
assert.equal(segments.filter(segment => segment.kind === "compaction").length, 2);
assert.ok(segments.every(segment => segment.kind !== "completed" || segment.hidden.every(item => item.kind !== "steer" && item.kind !== "compaction")));
const previousTurn: Item[] = [
  { kind: "user", id: "previous", turnId: "previous", text: "Previous" },
  { kind: "assistant", id: "previous-answer", turnId: "previous", text: "Done", reasoning: "Detail", streaming: false, final: true },
  { kind: "turn_stats", id: "previous-stats", turnId: "previous", success: true, outcome: "success" },
];
assert.deepEqual(buildTimelineSegments([...previousTurn, ...history], false).slice(0, 2), buildTimelineSegments(previousTurn, false),
  "guidance and compaction in another turn never change prior folding or identity");

const scoped: Item[] = [
  { kind: "user", id: "scoped-user", turnId: "scoped", text: "Inspect" },
  { kind: "tool", id: "pre-tool", turnId: "scoped", name: "read", args: "{}", status: "done", readOnly: true },
  { kind: "assistant", id: "pre-progress", turnId: "scoped", text: "Intermediate progress", reasoning: "", streaming: false },
  { kind: "steer", id: "guidance", turnId: "scoped", text },
  { kind: "tool", id: "post-tool", turnId: "scoped", name: "bash", args: "{}", status: "done", readOnly: false },
  { kind: "assistant", id: "scoped-final", turnId: "scoped", text: "Final response", reasoning: "", streaming: false, final: true },
  { kind: "turn_stats", id: "scoped-stats", turnId: "scoped", success: true, outcome: "success", elapsedMs: 5000, tokens: 117 },
];
const scopedSegments = buildTimelineSegments(scoped, false);
assert.deepEqual(scopedSegments.map(segment => segment.kind), ["user", "stats", "process", "steer", "process", "assistant"]);
assert.deepEqual(scopedSegments.filter(segment => segment.kind === "process").map(segment => [segment.defaultCollapsed, segment.items.map(item => item.id)]),
  [[true, ["pre-tool", "pre-progress"]], [true, ["post-tool"]]]);
assert.equal(scopedSegments.filter(segment => segment.kind === "stats").length, 1, "one full-turn total, never per-group totals");
assert.equal(scopedSegments.filter(segment => segment.kind === "assistant")[0].item.id, "scoped-final");
for (const outcome of ["failed", "cancelled", "interrupted"] as const) {
  const failed: Item[] = [...scoped.slice(0, -1), { kind: "turn_stats", id: "failed-stats", turnId: "scoped", success: false, outcome }];
  assert.ok(buildTimelineSegments(failed, false).every(segment => segment.kind !== "process" || !segment.defaultCollapsed), `${outcome} diagnostics stay expanded`);
}
assert.ok(buildTimelineSegments(scoped.slice(0, -1), true).every(segment => segment.kind !== "process" || !segment.defaultCollapsed), "running groups stay open");
const manyBoundaries: Item[] = [scoped[0], ...Array.from({ length: 1000 }, (_, i): Item[] => [
  { ...scoped[1], id: `tool-${i}` },
  { kind: "steer", id: `guide-${i}`, turnId: "scoped", text },
]).flat(), ...scoped.slice(-2)];
const many = buildTimelineSegments(manyBoundaries, false);
assert.equal(many.filter(segment => segment.kind === "steer").length, 1000);
assert.equal(many.filter(segment => segment.kind === "process" && segment.defaultCollapsed).length, 1000);
assert.equal(many.filter(segment => segment.kind === "stats").length, 1);
console.log("PASS transcript guidance hydration, stable IDs, duplicate text, legacy note, same-turn boundaries and prior-turn isolation");
