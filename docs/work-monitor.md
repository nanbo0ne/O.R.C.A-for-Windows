# Work Monitor (Local, Unreleased)

## Scope and Entry Points

- New monitor code: `internal/monitor/`, `internal/control/work_monitor.go`,
  `desktop/work_monitor.go`, `desktop/frontend/src/components/WorkMonitor.tsx`,
  `work-monitor.css`, `desktop/frontend/src/lib/workMonitor.ts`, and
  `workMonitorLabels.ts`.
- Additive integration only: optional context/state sink calls in the controller;
  one observation call in the desktop event sink; request identity in OpenAI and
  Anthropic adapters; an observation around the existing retry transport; an App
  monitor field; optional bridge methods; sidebar state, button and component.
- No monitor persistence, session replay, config, credentials or log reads. No
  release or commit. No changes by this worker to transcript, usage parsing,
  session storage, HistoryForTab, compact behavior, or styles.css.
- The controller's manual Compact path now also emits a monitor-only stopped/error
  observation on return. It never reports a final answer for an auxiliary operation.

## Actual Observation and Identity

`MonitorSubscribe(activeTabId)`, `MonitorUnsubscribe(generation)`, and
`MonitorSnapshot(generation, afterSeq)` are additive Wails methods. Older backends
show an unavailable state, without fabricated browser data.

Only a visible, expanded pane in the active tab subscribes. Tab changes, closing,
settings/history, responsive sidebar collapse, and document visibility hiding
dispose the subscription. The backend rechecks active tab identity; stale cleanup
cannot close a newer generation. A five-second renewable lease clears abandoned
subscriptions. Hidden producers never open `GetBody` or serialize events.

OpenAI/Anthropic observe the encoded request body immediately before each actual
transport attempt, after request transformations. Authentication headers, cookies,
endpoint URLs and raw transport errors are never inspected. Logical provider
request IDs remain intact; attempts have separate monotonic IDs. Usage receipts
join the logical request, not an arbitrary attempt. Generation and sequence IDs
are monotonic and memory-only.

Response text/reasoning comes only from the existing post-Hook event sink, not raw
provider chunks. Original reasoning replay and signatures in outgoing messages
are withheld so the pane cannot bypass PostLLMCall. `thinking` configuration
objects and effort fields remain visible. Free text filters recognize bearer
tokens, common key formats and credential assignments; **arbitrary secrets are
not guaranteed detectable, especially when split across streaming chunks**.
Complete structured text (including nested encoded JSON) is filtered by key;
quoted credential assignments also receive best-effort filtering. This is a filtered diagnostic view, not a credential
auditor or guaranteed complete wire dump.

Response/tool events that lack a request ID are explicitly turn-scoped. They are
not falsely matched to the latest attempt. Child lifecycle and usage are included;
child response streams not exposed by existing sinks remain unobservable. Tool
dispatch does not prove permission, Hook approval or actual execution.

## Bounds and Phases

- Backend retained payload accounting: 24 MiB including per-entry overhead,
  maximum 100 request attempts and 8192 entries. Webview retained accounting:
  4 MiB, 100 requests and 4096 entries. Snapshot batches: at most 512 KiB.
  These limits leave headroom below the 32 MiB diagnostic payload envelope;
  they are not a limit on the process/runtime/DOM allocator's total memory.
- Per request: scan at most 8 MiB; retain at most 256 KiB of JSON; bound individual
  strings to 16 KiB, nesting to 24 and arrays to 1024 elements. Images are skipped
  while scanning, preserving surrounding metadata instead of discarding the
  entire request. Long strings and partial/invalid structures are marked.
- The framing scanner exists because `encoding/json.Decoder.Token` materializes
  long strings before returning them. Scalar validation/unescaping and output
  encoding use encoding/json. Scan limits preserve a valid partial prefix.
- Producers use try-locks and bounded synchronous sanitization, no waiting queue,
  network/disk I/O, or unbounded goroutines. Contention drops are counted.
  Eviction, dropping, filtering and display limits are visible. This prioritizes
  agent progress over a lossless diagnostic stream.
- Input, first-fragment waiting, reasoning, output, tools, wait, actual pause gate,
  accepted cancellation and terminal phases come from observed boundaries.
  Prefill and Hook-internal timing are unknown. HTTP errors can retry and do not
  close a turn. Final requires successful TurnDone with FinalMessageID; tools-only
  or no-answer completion is stopped. Late phases cannot reopen a retained closed
  turn, including across interleaved turns. Phase guards retain 128 turn IDs;
  dropping an older guard increments the visible dropped-record counter.
- Retry events append an incomplete diagnostic with numeric attempt bounds only;
  they do not assert whether the cause was HTTP retry or partial SSE recovery.
  Failed/interrupted/cancelled turns and control failures append incomplete
  diagnostics without copying provider error text or URLs. Existing Agent
  partial-stream continuation and provider no-replay semantics are unchanged.
- A read-only cylinder-side strip replaces the scrollable phase history. It is
  6px high at rest and expands to 28px on hover or keyboard focus. A fixed centre
  pointer marks the current phase; at most three coloured faces are visible.
  The settled centre occupies 63.64% of the strip, with 18.18% on each side,
  shortening the original projected centre by ten percent.
- Red represents input/tools, yellow waiting, green reasoning and blue output.
  Neighbouring faces are fixed positions on the cylinder, not predicted events.
  Observed transitions move forward with gentle easing; pause, stop and an
  unavailable observation freeze motion. Reduced motion snaps to the new phase.
  No timer manufactures transitions. Tool/child counts in the header tooltip
  describe observed retained lanes, not a complete live scheduler.

## Frontend and Verification

The pane sits between ProjectTree and the bottom navigation, in the same column.
Default height is 320px, resizable with a viewport/container cap; ProjectTree keeps
at least 80px. Open-state CSS is scoped; the closed baseline is unchanged.
Browser/Wails viewports below 560px height close the pane and unsubscribe
instead of violating its minimum size or the navigation's containment. Chinese
and English labels are supported. Raw text is primary; metadata/limits collapse
into details. Adjacent response deltas coalesce with sequence ranges. The view
renders at most 100 content blocks, with omitted-history markers. Content retains
its independent follow control (48px tolerance); the phase strip has no scrolling,
dragging or phase-selection controls.

### Phase Strip Refinement (2026-09-23)

The dedicated synthetic browser fixture `phase-drum.browser.cjs` checks collapsed
and expanded rendering, monotonic transitions, the three-colour limit, interrupted
transitions, rapid updates, read-only input, keyboard focus and reduced motion.
The projection unit test covers 801 positions and the ten-percent centre reduction.
The self-contained synthetic preview and screenshots are generated into
`.tmp/phase-drum-evidence/` at repository root. This refinement changes only the
monitor frontend; it is local and unreleased, with no native-window acceptance claim.

Offline checks executed:

- `go test ./internal/monitor ./internal/provider/openai ./internal/provider/anthropic ./internal/control -run 'TestMonitor|TestWorkMonitor' -count=1`
- `go test . -run '^TestWorkMonitor' -count=1` in desktop.
- `go test ./internal/monitor -run '^$' -fuzz '^FuzzMonitorScan$' -fuzztime=3s -parallel=2`
  passed ~89,900 fuzz executions, plus deterministic invalid/depth/Unicode/escape
  and 500 random byte fixtures. Provider tests use fake RoundTrippers, no sockets
  or production APIs. They compare captured JSON to the actual encoded body,
  verify retry/receipt IDs and exclude auth, raw errors and pre-Hook reasoning.
- `npm run typecheck`, `npm run test:conversation-ui`, and dedicated CSS syntax.
- `work-monitor.browser.cjs`: 52 assertions, 4 expanded layout cases, 2 screenshots,
  plus responsive-collapse, document-hidden, follow, resize and 480px/180px-height
  close/unsubscribe checks. Isolated
  browser contexts and synthetic bindings only; external requests are blocked.
- Main's `workspace-monitor.browser.cjs`: 52 assertions, 8 cases, 8 screenshots.
  At 1366x900 modern, pane y394..714 (320px), nav bottom862, within sidebar900;
  1280x720 keeps the 80px tree. Below1120 the existing app policy collapses the
  sidebar and capture is off.
- Race detector could not run: local Go has CGO disabled and no gcc/clang on PATH.
  Concurrency unit tests ran without the race detector. No production/manual
  Wails session or live API acceptance is claimed.

Reusable local fixture: `desktop/frontend/src/__tests__/work-monitor.browser.cjs`
tested at `http://127.0.0.1:5291`; evidence `.tmp/work-monitor-evidence/` at repository root.
The temporary 5291 server was stopped after validation; the fixture can also use
the main worker's existing 5287 server (left untouched).
Main's full-shell fixture uses port5287 and `desktop/frontend/.tmp/workspace-monitor-evidence/`.
Use the bundled Node 24 runtime and its NODE_PATH for Playwright. No dependencies
were installed. Existing main-owned package scripts are reused unchanged.

## Read-Only Review Follow-Up

The final review's three monitor findings were fixed narrowly:

1. `Text` now parses complete bounded JSON through encoding/json and the same
   depth-bounded sanitizer used for request data. Bare `token`, hyphenated keys,
   nested encoded JSON in arguments/results, and quoted assignment forms have
   regression coverage. Literal filtering is separate to avoid resetting the
   recursive depth budget. Both locale strings explicitly disclaim recognition
   of arbitrary or chunk-split secrets.
2. Retry and unsuccessful terminal/control events append bounded incomplete
   diagnostics, without raw errors, endpoints or credential values. Recovery
   output remains visible and separate from those diagnostics. A retry does not
   claim partial SSE as its cause: the existing event also represents HTTP retry.
   Agent partial-stream continuation code was inspected, not modified.
3. The monitor alone closes below 560px viewport height and releases capture;
   this includes the native shell's supported minimum height of 480px and an
   extreme browser height of 180px. Closed sidebar layout remains unchanged.

Follow-up validation: monitor package tests, desktop `TestWorkMonitor*`,
`test:conversation-ui`, typecheck, and the updated 52-assertion browser fixture
all passed. Browser fixture used the existing main-owned port5287 server.
Foreground child lifecycle changes and final native Wails recompile remain
owned by the main worker; no independent acceptance claim is made for them here.
