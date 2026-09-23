import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import { activityIndicatorPhase, buildTimelineSegments, timelineKinds } from "../lib/transcriptTimeline";
import { readFileSync } from "node:fs";
import { readinessNoticeText } from "../lib/readinessNotice";
import { en } from "../locales/en";
import { zh } from "../locales/zh";

let failed = 0;

function equal<T>(label: string, actual: T, expected: T) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    return;
  }
  failed += 1;
  process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
}

const chronology: Item[] = [
  { kind: "user", id: "u1", text: "start" },
  { kind: "assistant", id: "a1", text: "first", reasoning: "", streaming: false },
  { kind: "tool", id: "t1", name: "read", args: "{}", readOnly: true, status: "done" },
  { kind: "assistant", id: "a2", text: "second", reasoning: "", streaming: false },
  { kind: "tool", id: "t2", name: "bash", args: "{}", readOnly: false, status: "done" },
];

equal(
  "a live turn interleaves progress text and tool groups",
  timelineKinds(buildTimelineSegments(chronology, true)),
  ["user", "assistant", "process:tool:read", "assistant", "process:tool:bash"],
);

const adjacent: Item[] = [
  { kind: "user", id: "u1", text: "start" },
  { kind: "assistant", id: "r1", text: "", reasoning: "thinking", streaming: false },
  { kind: "tool", id: "t1", name: "read", args: "{}", readOnly: true, status: "done" },
  { kind: "notice", id: "n1", level: "info", text: "done" },
  { kind: "assistant", id: "a1", text: "answer", reasoning: "", streaming: false },
  { kind: "tool", id: "t2", name: "bash", args: "{}", readOnly: false, status: "done" },
];

equal(
  "settled reasoning joins activity while visible progress separates tool groups",
  timelineKinds(buildTimelineSegments(adjacent, true)),
  ["user", "process:assistant,tool:read,notice", "assistant", "process:tool:bash"],
);

const consecutive: Item[] = [
  { kind: "user", id: "cu", text: "check and run" },
  { kind: "tool", id: "ct1", name: "read", args: "{}", readOnly: true, status: "done" },
  { kind: "assistant", id: "cr1", text: "", reasoning: "inspect the result", streaming: false },
  { kind: "phase", id: "cp1", text: "Checking" },
  { kind: "tool", id: "ct2", name: "task", args: "{}", readOnly: false, status: "done" },
  { kind: "tool", id: "child1", parentId: "ct2", name: "read", args: "{}", readOnly: true, status: "done" },
  { kind: "tool", id: "child2", parentId: "ct2", name: "bash", args: "{}", readOnly: false, status: "done" },
  { kind: "tool", id: "todo", name: "todo_write", args: "{}", readOnly: false, status: "done" },
  { kind: "tool", id: "plan", name: "exit_plan_mode", args: "{}", readOnly: false, status: "done" },
  { kind: "assistant", id: "empty", text: " \n", reasoning: "", streaming: false },
  { kind: "notice", id: "cn", level: "info", text: "Check finished" },
  { kind: "assistant", id: "cr2", text: " \n", reasoning: "verify once more", streaming: false },
  { kind: "phase", id: "cp2", text: "Checking" },
  { kind: "compaction", id: "cc", pending: false, trigger: "auto", messages: 10, summary: "context summary", archive: "" },
  { kind: "tool", id: "ct3", name: "bash", args: "{}", readOnly: false, status: "running" },
];
const consecutiveKinds = ["user", "process:tool:read,assistant,phase,tool:task,notice,assistant,phase", "compaction", "process:tool:bash"];
const consecutiveSegments = buildTimelineSegments(consecutive, true);
equal("hidden reasoning and repeated phases do not fragment consecutive tool activity", timelineKinds(consecutiveSegments), consecutiveKinds);
const consecutiveProcess = consecutiveSegments.find((segment) => segment.kind === "process");
equal(
  "process details retain chronological IDs including repeated phases",
  consecutiveProcess?.kind === "process" ? consecutiveProcess.items.map((item) => item.id) : [],
  ["ct1", "cr1", "cp1", "ct2", "cn", "cr2", "cp2"],
);
equal(
  "tool totals exclude nested subcalls and hidden workflow tools",
  consecutiveProcess?.kind === "process" ? consecutiveProcess.items.filter((item) => item.kind === "tool").length : 0,
  2,
);
const firstActivity = buildTimelineSegments(consecutive.slice(0, 3), true).find((segment) => segment.kind === "process");
equal("appending reasoning, phases, and tools preserves the process expand-state key", consecutiveProcess?.id, firstActivity?.id);
equal("timeline cache returns the same segments for unchanged items", buildTimelineSegments(consecutive, true) === consecutiveSegments, true);
equal("grouping does not mutate an earlier cached process", firstActivity?.items.map((item) => item.id), ["ct1", "cr1"]);

const stageReply: Item = { kind: "assistant", id: "stage1", text: "The check passed.", reasoning: "ready for the next phase", streaming: false };
const repeatedStages: Item[] = [
  ...consecutive,
  stageReply,
  { kind: "tool", id: "ct4", name: "read", args: "{}", readOnly: true, status: "done" },
  { ...stageReply, id: "stage2" },
  { kind: "tool", id: "ct5", name: "bash", args: "{}", readOnly: false, status: "done" },
];
equal(
  "each real stage reply remains a hard boundary even when its text repeats",
  timelineKinds(buildTimelineSegments(repeatedStages, true)),
  [...consecutiveKinds, "assistant", "process:tool:read", "assistant", "process:tool:bash"],
);
equal(
  "activity without a user item also merges only across settled reasoning",
  timelineKinds(buildTimelineSegments(repeatedStages.slice(1), false)),
  [...consecutiveKinds.slice(1), "assistant", "process:tool:read", "assistant", "process:tool:bash"],
);
equal(
  "a new user turn starts a separate process group",
  timelineKinds(buildTimelineSegments([...consecutive, { kind: "user", id: "cu2", text: "next" }, ...consecutive.slice(1, 3)], true)),
  [...consecutiveKinds, "user", "process:tool:read,assistant"],
);

for (const outcome of ["failed", "cancelled", "interrupted"] as const) {
  const terminal: Item[] = [
    ...consecutive.map((item): Item => item.kind === "tool" && item.status === "running"
      ? { ...item, status: outcome === "failed" ? "error" : "stopped", error: outcome === "failed" ? "verification failed" : undefined }
      : item),
    { kind: "turn_stats", id: `cs-${outcome}`, turnId: "ct", success: false, outcome },
  ];
  const terminalSegments = buildTimelineSegments(terminal, false);
  equal(`${outcome} activity stays grouped without successful-turn folding`, timelineKinds(terminalSegments), ["user", "stats", ...consecutiveKinds.slice(1)]);
  const terminalProcess = terminalSegments.find((segment) => segment.kind === "process");
  equal(`${outcome} preserves the running group's expand-state key`, terminalProcess?.id, consecutiveProcess?.id);
  equal(
    `${outcome} retains the terminal tool status`,
    terminalSegments.flatMap((segment) => segment.kind === "process" ? segment.items.filter((item) => item.kind === "tool").map((item) => item.status) : []),
    ["done", "done", outcome === "failed" ? "error" : "stopped"],
  );
}

const placeholder: Item = { kind: "assistant", id: "stream", text: "", reasoning: "", streaming: true };
for (const reasoning of ["", "uncommitted reasoning"]) {
  equal(
    "a live placeholder stays outside collapsible activity even without committed text",
    timelineKinds(buildTimelineSegments([...consecutive, { ...placeholder, reasoning }, repeatedStages[repeatedStages.length - 1]], true)),
    [...consecutiveKinds, "assistant", "process:tool:bash"],
  );
}
let liveStage = { ...initialState, items: consecutive, running: true, turnActive: true };
liveStage = reducer(liveStage, { type: "event", e: { kind: "reasoning", messageId: "stream", text: "consider the next step" } });
const placeholderItems = liveStage.items;
const mountedSegments = buildTimelineSegments(placeholderItems, true);
equal("reasoning-only live events mount an assistant consumer before any text arrives", timelineKinds(mountedSegments), [...consecutiveKinds, "assistant"]);
liveStage = reducer(liveStage, { type: "event", e: { kind: "text", messageId: "stream", text: "Visible stage" } });
equal("text deltas leave placeholder items unchanged", liveStage.items === placeholderItems, true);
equal("text deltas update live text independently of the cached timeline", liveStage.live?.text, "Visible stage");
equal("the live text consumer survives timeline cache hits", buildTimelineSegments(liveStage.items, true) === mountedSegments, true);
liveStage = reducer(liveStage, { type: "event", e: { kind: "message", messageId: "stream", text: "Visible stage" } });
equal("committing live text preserves its chronological reply boundary", timelineKinds(buildTimelineSegments(liveStage.items, true)), [...consecutiveKinds, "assistant"]);
const settledReasoning = [...consecutive, { ...placeholder, streaming: false, reasoning: "settled without text" }];
equal(
  "a reasoning-only placeholder joins activity when it settles",
  timelineKinds(buildTimelineSegments(settledReasoning, true)),
  [...consecutiveKinds.slice(0, -1), `${consecutiveKinds[consecutiveKinds.length - 1]},assistant`],
);

const withStats: Item[] = [
  ...chronology,
  { kind: "assistant", id: "final-1", messageId: "final-1", text: "Done.", reasoning: "", streaming: false, final: true, turnId: "turn-1" },
  { kind: "turn_stats", id: "s1", elapsedMs: 5000, tokens: 120, success: true, outcome: "success", turnId: "turn-1", finalMessageId: "final-1" },
];
equal(
  "successful completed turn collapses intermediate activity",
  timelineKinds(buildTimelineSegments(withStats, false)),
  ["user", "completed:assistant,tool:read,assistant,tool:bash"],
);

const foldedActivity = buildTimelineSegments([
  ...repeatedStages,
  { kind: "assistant", id: "cf", turnId: "ct", text: "All done.", reasoning: "final check", streaming: false, final: true },
  { kind: "turn_stats", id: "cs", turnId: "ct", success: true, outcome: "success" },
], false);
const folded = foldedActivity.filter((segment) => segment.kind === "process");
equal("successful completion folds both sides of the standalone compaction boundary", foldedActivity.map((segment) => segment.kind), ["user", "stats", "process", "compaction", "process", "assistant"]);
equal("scoped completed groups default to collapsed", folded.map((segment) => segment.defaultCollapsed), [true, true]);
equal("completed details retain ordered tools and progress without hidden workflow calls", folded.flatMap((segment) => segment.items.map((item) => item.id)),
  ["ct1", "cr1", "cp1", "ct2", "cn", "cr2", "cp2", "ct3", "stage1", "ct4", "stage2", "ct5", "cf"]);
equal("the completed final answer is separate from its reasoning", foldedActivity.find((segment) => segment.kind === "assistant")?.item,
  { kind: "assistant", id: "cf", turnId: "ct", text: "All done.", reasoning: "", streaming: false, final: true });

const recoveredStats: Item[] = [
  { kind: "user", id: "ru1", text: "fix it" },
  { kind: "tool", id: "rt1", name: "bash", args: "{}", readOnly: false, status: "error", error: "first attempt failed" },
  { kind: "tool", id: "rt2", name: "bash", args: "{}", readOnly: false, status: "done" },
  { kind: "assistant", id: "ra1", messageId: "ra1", text: "Fixed and verified.", reasoning: "", streaming: false, final: true, turnId: "turn-r" },
  { kind: "turn_stats", id: "rs1", elapsedMs: 7000, success: true, outcome: "success", turnId: "turn-r", finalMessageId: "ra1" },
];
equal(
  "explicit successful completion collapses a recovered tool failure",
  timelineKinds(buildTimelineSegments(recoveredStats, false)),
  ["user", "completed:tool:bash,tool:bash"],
);

const failedStats: Item[] = [
  ...chronology,
  { kind: "notice", id: "error", level: "warn", text: "failed" },
  { kind: "turn_stats", id: "s2", elapsedMs: 5000, success: false },
];
equal(
  "failed turn keeps its diagnostic timeline visible",
  timelineKinds(buildTimelineSegments(failedStats, false)),
  ["user", "stats", "assistant", "process:tool:read", "assistant", "process:tool:bash,notice"],
);

equal(
  "final legacy turn without completion evidence remains expanded",
  timelineKinds(buildTimelineSegments(chronology, false)),
  ["user", "assistant", "process:tool:read", "assistant", "process:tool:bash"],
);

const legacyHistory: Item[] = [
  ...chronology,
  { kind: "user", id: "u2", text: "next question" },
  { kind: "assistant", id: "a3", text: "working", reasoning: "", streaming: false },
];
equal(
  "a later user turn does not guess completion for legacy history",
  timelineKinds(buildTimelineSegments(legacyHistory, true)),
  ["user", "assistant", "process:tool:read", "assistant", "process:tool:bash", "user", "assistant"],
);

const failedLegacyHistory: Item[] = [
  { kind: "user", id: "fu1", text: "run" },
  { kind: "tool", id: "ft1", name: "bash", args: "{}", readOnly: false, status: "error", error: "failed" },
  { kind: "assistant", id: "fa1", text: "The command failed.", reasoning: "", streaming: false },
  { kind: "user", id: "fu2", text: "try something else" },
];
equal(
  "legacy failure evidence prevents automatic collapse",
  timelineKinds(buildTimelineSegments(failedLegacyHistory, false)),
  ["user", "process:tool:bash", "assistant", "user"],
);

const completedWithBackgroundNotice: Item[] = [
  ...withStats,
  { kind: "notice", id: "background", level: "info", text: "Background task finished" },
];
equal(
  "unowned background notices stay outside the completed turn",
  timelineKinds(buildTimelineSegments(completedWithBackgroundNotice, false)),
  ["user", "completed:assistant,tool:read,assistant,tool:bash", "process:notice"],
);

const withModeSwitch: Item[] = [
  ...withStats,
  { kind: "mode_switch", id: "switch-1", fromMode: "coding", toMode: "assistant", appliedMode: "assistant", phase: "completed", progress: 100, startedAt: 10, completedAt: 20 },
  { kind: "user", id: "u-next", text: "continue" },
];
equal(
  "mode switches remain standalone and never enter a completed process panel",
  timelineKinds(buildTimelineSegments(withModeSwitch, false)),
  ["user", "completed:assistant,tool:read,assistant,tool:bash", "mode_switch", "user"],
);

const restoredSwitch = historyMessagesToItems([{
  role: "mode_switch",
  content: "",
  switchId: "switch-restored",
  switchFromMode: "assistant",
  switchToMode: "coding",
  switchAppliedMode: "coding",
  switchPhase: "completed",
  switchProgress: 100,
  switchStartedAt: 100,
  switchCompletedAt: 200,
}], "h").items;
equal("persisted mode switch history restores its explicit item type", restoredSwitch[0]?.kind, "mode_switch");

let switching = reducer(initialState, { type: "runtime_switch", progress: {
  tabId: "tab", switchId: "switch-live", generation: 2, fromMode: "coding", toMode: "assistant",
  appliedMode: "coding", phase: "building", progress: 35, recorded: true, startedAt: 100,
} });
switching = reducer(switching, { type: "runtime_switch", progress: {
  tabId: "tab", switchId: "switch-live", generation: 2, fromMode: "coding", toMode: "assistant",
  appliedMode: "assistant", phase: "completed", progress: 100, recorded: true, startedAt: 100, completedAt: 200,
} });
equal("live switch phases update one stable timeline item", switching.items.filter((item) => item.kind === "mode_switch").length, 1);
equal("live switch completion updates the existing item", switching.items.find((item) => item.kind === "mode_switch")?.phase, "completed");
const staleSwitch = reducer(switching, { type: "runtime_switch", progress: {
  tabId: "tab", switchId: "stale", generation: 1, fromMode: "assistant", toMode: "coding",
  phase: "failed", progress: 35, recorded: true, startedAt: 50,
} });
equal("older runtime generations cannot overwrite the latest switch", staleSwitch.items.some((item) => item.kind === "mode_switch" && item.id === "stale"), false);

let protocol = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-p" } });
protocol = reducer(protocol, { type: "event", e: { kind: "text", turnId: "turn-p", itemId: "item-p", messageId: "message-p", text: "Working" } });
protocol = reducer(protocol, { type: "event", e: { kind: "message", turnId: "turn-p", itemId: "item-p", messageId: "message-p", text: "Working" } });
protocol = reducer(protocol, { type: "event", e: { kind: "answer_committed", turnId: "turn-p", finalItemId: "item-p", finalMessageId: "message-p" } });
protocol = reducer(protocol, { type: "event", e: {
  kind: "turn_done", turnId: "turn-p", finalItemId: "item-p", finalMessageId: "message-p", outcome: "success",
  turnTokens: 321, turnCost: 0.0123, turnCurrency: "$", turnCostAvailable: true,
} });
equal("answer_committed owns the exact final message", protocol.items.some((item) => item.kind === "assistant" && item.messageId === "message-p" && item.final), true);
equal("explicit success records a successful turn outcome", protocol.items.some((item) => item.kind === "turn_stats" && item.outcome === "success"), true);
const authoritativeStats = protocol.items.find((item) => item.kind === "turn_stats");
equal("TurnDone tokens use the backend authoritative aggregate", authoritativeStats?.kind === "turn_stats" ? authoritativeStats.tokens : undefined, 321);
equal("official DeepSeek cost is retained only with its source flag", authoritativeStats?.kind === "turn_stats" ? [authoritativeStats.cost, authoritativeStats.currency, authoritativeStats.costAvailable] : undefined, [0.0123, "$", true]);

const restoredStats = historyMessagesToItems([{
  role: "turn_stats", content: "", turnId: "history-turn", outcome: "success", elapsedMs: 1400,
  tokens: 88, cost: 0.0042, currency: "$", costAvailable: true, finalMessageId: "history-final",
}], "h").items[0];
equal(
  "MessageView turn stats restore cost fields",
  restoredStats?.kind === "turn_stats" ? [restoredStats.tokens, restoredStats.cost, restoredStats.currency, restoredStats.costAvailable] : undefined,
  [88, 0.0042, "$", true],
);

const historyAssistant = historyMessagesToItems([{ role: "assistant", content: "history answer" }], "history").items;
const liveAfterHistory = reducer(
  { ...initialState, items: historyAssistant, turnActive: true, currentTurnId: "live-turn", turnStartAt: Date.now() },
  { type: "event", e: { kind: "text", turnId: "live-turn", messageId: "live-message", text: "live answer" } },
);
equal(
  "message-only live events do not match an unidentifiable history assistant",
  liveAfterHistory.items.filter((item) => item.kind === "assistant").length,
  2,
);
equal(
  "message-only live events keep their own assistant identity",
  liveAfterHistory.live?.text,
  "live answer",
);

let cancelled = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-c" } });
cancelled = reducer(cancelled, { type: "event", e: {
  kind: "turn_done", turnId: "turn-c", outcome: "cancelled", err: "context canceled",
  turnTokens: 7, turnCost: 9, turnCurrency: "$", turnCostAvailable: false,
} });
equal("cancelled turns do not render context-canceled as an error", cancelled.items.some((item) => item.kind === "notice" && item.text === "context canceled"), false);
equal("cancelled turns retain their explicit outcome", cancelled.items.some((item) => item.kind === "turn_stats" && item.outcome === "cancelled"), true);
const cancelledStats = cancelled.items.find((item) => item.kind === "turn_stats");
equal("cancelled turns keep authoritative tokens", cancelledStats?.kind === "turn_stats" ? cancelledStats.tokens : undefined, 7);
equal("unavailable turn cost is never rendered as official", cancelledStats?.kind === "turn_stats" ? [cancelledStats.cost, cancelledStats.currency, cancelledStats.costAvailable] : undefined, [undefined, undefined, false]);

let stable = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-stable" } });
stable = reducer(stable, { type: "event", e: { kind: "text", turnId: "turn-stable", itemId: "item-stable", messageId: "message-stable", text: "Done" } });
stable = reducer(stable, { type: "event", e: { kind: "message", turnId: "turn-stable", itemId: "item-stable", messageId: "message-stable", text: "Done" } });
stable = reducer(stable, { type: "event", e: { kind: "answer_committed", turnId: "turn-stable", finalItemId: "item-stable", finalMessageId: "message-stable" } });
stable = reducer(stable, { type: "event", e: { kind: "turn_done", turnId: "turn-stable", finalItemId: "item-stable", finalMessageId: "message-stable", outcome: "success", turnTokens: 4 } });
const stableBefore = buildTimelineSegments(stable.items, false).find((segment) => segment.kind === "completed");
stable = reducer(stable, { type: "user", text: "next", seq: stable.seq });
stable = reducer(stable, { type: "event", e: { kind: "turn_started", turnId: "turn-next" } });
stable = reducer(stable, { type: "event", e: { kind: "turn_done", turnId: "turn-next", outcome: "cancelled" } });
const stableAfter = buildTimelineSegments(stable.items, false).find((segment) => segment.kind === "completed");
equal("a later cancelled turn preserves the previous completed segment identity", stableAfter?.kind === "completed" ? stableAfter.id : undefined, stableBefore?.kind === "completed" ? stableBefore.id : undefined);

let lateDone = reducer(initialState, { type: "event", e: { kind: "turn_started", turnId: "turn-old" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "text", turnId: "turn-old", itemId: "old-item", messageId: "old-message", text: "old answer" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "message", turnId: "turn-old", itemId: "old-item", messageId: "old-message", text: "old answer" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "answer_committed", turnId: "turn-old", finalItemId: "old-item", finalMessageId: "old-message" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "turn_done", turnId: "turn-old", finalMessageId: "old-message", outcome: "success", turnTokens: 12, turnCost: 0.01, turnCurrency: "$", turnCostAvailable: true } });
lateDone = reducer(lateDone, { type: "user", text: "new answer", seq: lateDone.seq });
lateDone = reducer(lateDone, { type: "event", e: { kind: "turn_started", turnId: "turn-new" } });
lateDone = reducer(lateDone, { type: "event", e: { kind: "text", turnId: "turn-new", itemId: "new-item", messageId: "new-message", text: "still working" } });
const afterLateDone = reducer(lateDone, { type: "event", e: {
  kind: "turn_done", turnId: "turn-old", finalMessageId: "old-message", outcome: "success",
  // This aggregate includes a child request that arrived after the first UI completion.
  turnTokens: 48, turnCost: 0.0345, turnCurrency: "$", turnCostAvailable: true,
} });
const refreshedOldStats = afterLateDone.items.find((item) => item.kind === "turn_stats" && item.turnId === "turn-old");
equal("late old TurnDone does not finalize the active new turn", [afterLateDone.running, afterLateDone.turnActive, afterLateDone.currentTurnId, afterLateDone.live?.text], [true, true, "turn-new", "still working"]);
equal("late child-inclusive TurnDone refreshes only the matching old stats", refreshedOldStats?.kind === "turn_stats" ? [refreshedOldStats.tokens, refreshedOldStats.cost, refreshedOldStats.currency, refreshedOldStats.costAvailable] : undefined, [48, 0.0345, "$", true]);
equal("late old TurnDone does not duplicate turn stats", afterLateDone.items.filter((item) => item.kind === "turn_stats").length, 1);

const runningSegments = buildTimelineSegments(chronology, true);
const completedSegments = buildTimelineSegments(failedStats, false);
const lastRunningProcess = [...runningSegments].reverse().find((segment) => segment.kind === "process");
const lastCompletedProcess = [...completedSegments].reverse().find((segment) => segment.kind === "process");
equal(
  "current process segment remains live before TurnDone",
  lastRunningProcess?.kind === "process" ? lastRunningProcess.completed : undefined,
  false,
);
equal(
  "failed process segment stays expanded after TurnDone",
  lastCompletedProcess?.kind === "process" ? lastCompletedProcess.completed : undefined,
  false,
);
equal("model activity rotates clockwise", activityIndicatorPhase(chronology, true, true, false), "model");
const activeTool: Item[] = [
  { kind: "user", id: "atu1", text: "run it" },
  { kind: "tool", id: "att1", name: "bash", args: "{}", readOnly: false, status: "running" },
];
equal("running tool activity rotates counterclockwise", activityIndicatorPhase(activeTool, true, true, false), "tool");
equal("disabled activity mark stays hidden", activityIndicatorPhase(chronology, false, true, false), undefined);
equal("paused activity mark stays hidden", activityIndicatorPhase(chronology, true, true, true), undefined);
equal("completed activity mark stays hidden", activityIndicatorPhase(failedStats, true, false, false), undefined);
const active = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
const done = reducer(active, { type: "event", e: { kind: "turn_done" } });
const backgroundNotice = reducer(done, {
  type: "event",
  e: { kind: "notice", level: "info", text: "background job finished" },
});
equal("background notice does not reactivate running state", backgroundNotice.running, false);
equal("background notice does not reactivate the turn", backgroundNotice.turnActive, false);

const transcriptSource = readFileSync(new URL("../components/Transcript.tsx", import.meta.url), "utf8");
const transcriptCss = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
equal("process group no longer nests an outer ProcessCard", transcriptSource.includes("<ProcessCard\n      tone=\"default\""), false);
equal("completed timeline has a flat activity rail", transcriptSource.includes('className="completed-turn__timeline process-activity-rail"'), true);
equal("completed process details do not create a nested process panel", transcriptSource.includes("<TimelineProcessGroup\n                      key={segment.id}"), false);
equal("successful completed turns start collapsed", transcriptSource.includes("const [open, setOpen] = useState(false)"), true);
equal("question rail does not scroll the transcript via scrollIntoView", transcriptSource.includes('el?.scrollIntoView({ block: "nearest" })'), false);
equal("older history auto-pages without forced collapsed cards", transcriptSource.includes("el.scrollTop < 320) loadEarlier()") && !transcriptSource.includes("<WarmTurnCard"), true);
equal("question marker mouse events do not bubble into the rail", transcriptSource.includes("e.stopPropagation();"), true);
equal("activity phase changes wait for the old single ring to fade out", transcriptSource.includes("}, 140);") && transcriptSource.includes("}, 160);"), true);
equal("activity mark renders exactly one phase-keyed spinner element", transcriptSource.includes('<span key={visual.phase} className={`process-activity-spinner process-activity-spinner--${visual.phase}`} />'), true);
equal("activity ring never flips animation direction in place", transcriptCss.includes("animation-direction"), false);
equal("clockwise and counterclockwise rings use separate generated gradients", transcriptCss.includes("process-activity-spin-clockwise") && transcriptCss.includes("process-activity-spin-counterclockwise"), true);
equal("completed turn header is light, bordered, and rounded", transcriptSource.includes("turnStatsHeaderStyle") && transcriptSource.includes("borderRadius: 6"), true);
equal("completed turn cost is guarded by the backend availability flag", transcriptSource.includes("item.costAvailable !== true"), true);
const standaloneStatsSource = transcriptSource.match(/function TurnStatsRow[\s\S]*?\r?\n}\r?\n\r?\nfunction CompletedTurn/);
equal("standalone stats render without a faux expand control", Boolean(standaloneStatsSource && !standaloneStatsSource[0].includes("useState") && !standaloneStatsSource[0].includes("onToggle")), true);

for (const dictionary of [en, zh]) {
  const translate = (key: keyof typeof en) => dictionary[key];
  equal("pending checklist notice uses the selected language", readinessNoticeText(en["notice.readinessPending"], translate), dictionary["notice.readinessPending"]);
  equal("actual failed action remains an explicit warning", readinessNoticeText(en["notice.readinessFailed"], translate), dictionary["notice.readinessFailed"]);
  equal("older diagnostic renders without exposing its raw task list", readinessNoticeText("final-answer readiness found a new failed action after targeted verification: synthetic task: pending", translate), dictionary["notice.readinessLegacyStopped"]);
  equal("unrelated notices remain unchanged", readinessNoticeText("synthetic server error", translate), "synthetic server error");
}

if (failed > 0) process.exit(1);
