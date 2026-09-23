// Run: tsx src/__tests__/context-panel-usage.test.ts

import {
  computeContextPanelUsage,
  contextPanelCurrencySymbol,
  formatContextPanelMoney,
  resolveContextPanelRequestCount,
} from "../lib/contextPanelUsage";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\ncontext panel usage");

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 19337,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      requestCount: 2,
      readFiles: [],
      changedFiles: [],
    },
    usage: {
      promptTokens: 12000,
      completionTokens: 7000,
      totalTokens: 19000,
      reasoningTokens: 300,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionCacheHitTokens: 0,
      sessionCacheMissTokens: 0,
    },
    sessionTokens: 19337,
  }).promptTokens,
  12000,
  "falls back to latest usage breakdown when panel only has totals",
);

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 28393607 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 28393607,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionPromptTokens: 28330306,
      sessionCompletionTokens: 63301,
      sessionReasoningTokens: 18705,
      requestCount: 247,
      readFiles: [],
      changedFiles: [],
    },
    usage: {
      promptTokens: 900000,
      completionTokens: 50000,
      totalTokens: 950000,
      cacheHitTokens: 0,
      cacheMissTokens: 900000,
      sessionCacheHitTokens: 0,
      sessionCacheMissTokens: 28330306,
    },
    sessionTokens: 28393607,
  }).usedTokens,
  0,
  "keeps an authoritative zero snapshot after controller rebuild",
);

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 19337,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      requestCount: 2,
      readFiles: [],
      changedFiles: [],
    },
    sessionTokens: 19337,
  }).usedTokens,
  0,
  "does not treat cumulative session totals as current context usage",
);

{
  const got = computeContextPanelUsage({
    context: { used: 2400, window: 10000, sessionTokens: 25000 },
    info: {
      usedTokens: 2400,
      windowTokens: 10000,
      promptTokens: 1800,
      completionTokens: 600,
      totalTokens: 25000,
      lastRequestAvailable: true,
      lastRequestTotalTokens: 2400,
      reasoningTokens: 0,
      reasoningTokensAvailable: true,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionPromptTokens: 20000,
      sessionCompletionTokens: 5000,
      sessionReasoningTokens: 900,
      sessionReasoningTokensAvailable: true,
      sessionReasoningTokensPartial: true,
      readFiles: [],
      changedFiles: [],
    },
  });
  eq(got.usedTokens, 2400, "context occupancy stays independent of cumulative session usage");
  eq(got.promptTokens + got.completionTokens, 25000, "cumulative prompt and completion account without reasoning being added twice");
  eq(got.currentReasoningTokensAvailable, true, "an explicitly reported zero reasoning value remains available");
  eq(got.currentReasoningTokens, 0, "an explicitly reported zero reasoning value stays zero");
  eq(got.currentOutputTokens, 600, "reported zero reasoning leaves full completion in output");
  eq(got.currentTotalTokens, 2400, "latest request total is independent of the cumulative backend total");
  eq(got.totalTokens, 25000, "session total remains cumulative");
  eq(got.sessionReasoningTokensAvailable, true, "cumulative reasoning records that at least one request reported it");
  eq(got.sessionReasoningTokensPartial, true, "cumulative reasoning marks requests with missing reports as partial");
}

{
  const got = computeContextPanelUsage({
    context: { used: 0, window: 10000, sessionTokens: 30000 },
    info: {
      usedTokens: 7200,
      windowTokens: 10000,
      promptTokens: 120,
      completionTokens: 80,
      totalTokens: 30000,
      lastRequestAvailable: true,
      lastRequestTotalTokens: 200,
      reasoningTokens: 30,
      reasoningTokensAvailable: true,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      readFiles: [],
      changedFiles: [],
    },
  });
  eq(got.usedTokens, 0, "authoritative post-compaction zero beats a stale nonzero panel snapshot");
  eq(got.currentOutputTokens, 50, "reported reasoning is subtracted from completion to show visible output");
  eq(got.currentTotalTokens, 200, "latest request residual does not use cumulative total");
}

eq(
  computeContextPanelUsage({
    usage: { promptTokens: 8, completionTokens: 3, totalTokens: 11, cacheHitTokens: 0, cacheMissTokens: 0, sessionCacheHitTokens: 0, sessionCacheMissTokens: 0 },
  }).currentReasoningTokensAvailable,
  false,
  "missing reasoning usage remains unavailable rather than looking like reported zero",
);

{
  const got = computeContextPanelUsage({
    usage: { promptTokens: 8, completionTokens: 3, totalTokens: 11, cacheHitTokens: 0, cacheMissTokens: 0, sessionCacheHitTokens: 0, sessionCacheMissTokens: 0 },
  });
  eq(got.currentOutputTokens, 3, "when reasoning is omitted, output retains completion tokens");
  eq(got.currentTotalTokens, 11, "wire total is used only for the latest request fallback");
  eq(got.currentRequestAvailable, true, "a present usage receipt is available even when reasoning is omitted");
}

eq(
  computeContextPanelUsage({}).currentRequestAvailable,
  false,
  "does not present absent latest-request usage as a precise zero breakdown",
);

eq(
  computeContextPanelUsage({
    context: { used: 0, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 19337,
      reasoningTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      sessionPromptTokens: 12000,
      sessionCompletionTokens: 7000,
      sessionReasoningTokens: 300,
      requestCount: 2,
      readFiles: [],
      changedFiles: [],
    },
    sessionTokens: 19337,
  }).reasoningTokens,
  300,
  "uses persisted session breakdown after restart",
);

eq(
  computeContextPanelUsage({
    context: { used: 5000, window: 1000000, sessionTokens: 19337 },
    info: {
      usedTokens: 0,
      windowTokens: 1000000,
      promptTokens: 3200,
      completionTokens: 900,
      totalTokens: 19337,
      reasoningTokens: 100,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      readFiles: [],
      changedFiles: [],
    },
  }).usedTokens,
  5000,
  "keeps the real context snapshot when it is available",
);

{
  const got = computeContextPanelUsage({
    context: { used: 6000, window: 100000, sessionTokens: 50000 },
    info: {
      usedTokens: 6000,
      windowTokens: 100000,
      promptTokens: 5900,
      completionTokens: 80,
      totalTokens: 50000,
      reasoningTokens: 20,
      cacheHitTokens: 3000,
      cacheMissTokens: 2900,
      sessionPromptTokens: 42000,
      sessionCompletionTokens: 7500,
      sessionReasoningTokens: 500,
      requestCount: 10,
      readFiles: [],
      changedFiles: [],
    },
  });
  eq(got.promptTokens, 42000, "keeps persisted cumulative prompt tokens for metrics");
  eq(got.currentPromptTokens, 5900, "keeps current prompt tokens for the context-window bar after compaction");
  eq(got.usedTokens, 6000, "uses recalculated compacted context usage after compaction");
}

eq(
  resolveContextPanelRequestCount({
    usedTokens: 0,
    windowTokens: 1000,
    promptTokens: 0,
    completionTokens: 0,
    totalTokens: 0,
    reasoningTokens: 0,
    cacheHitTokens: 0,
    cacheMissTokens: 0,
    requestCount: undefined,
    readFiles: [{ path: "a.ts", turn: 1, time: 1 }],
    changedFiles: [{ path: "b.ts", turns: [1], sources: [] }],
  }, undefined),
  0,
  "does not derive request count from referenced files",
);

eq(
  resolveContextPanelRequestCount(undefined, { used: 0, window: 1000, sessionTokens: 0, requestCount: 7 }),
  7,
  "falls back to the context snapshot request count",
);

eq(contextPanelCurrencySymbol("USD"), "$", "formats known currency codes with their symbols");
eq(formatContextPanelMoney(1.25, "CAD"), "CAD 1.25", "does not relabel an unsupported currency as yuan");
eq(formatContextPanelMoney(1.25), "1.25", "does not invent a currency when none is supplied");
eq(formatContextPanelMoney(Number.NaN, "USD"), "-", "does not render a non-finite cost");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
