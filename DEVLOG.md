# ORCA beta development log

## 2026-11-16 R8 diagnostics and R9 account adoption

- Open webpage exposed the actual signed-out state; the user signed in. The
  subsequent identity-latch failure revealed missing account-switch UX.
- R9 retains login and source authorization, drains old controllers/tools,
  archives bindings and checks a fresh native page before reopening admission.
  Generic recovery cannot adopt an account. Explicit logout still revokes.
- Persist identity loss with an empty profile-owned marker; no credentials or
  chat data. State version 6 rejects old readers. Add cancellation, failed
  release/init/recheck, restart, consent and real React click regressions.
- R8 native UI: 16 cases plus browser preview passed. Ordinary full root and
  desktop suites passed in normal host execution. No AV settings changed.
- Production read-only audit confirms desktop routes remain absent, matching
  existing migration checksums and old unresolved billing predating the
  current process. No deployment, refund, price change or paid request.

## 2026-11-16 native pointers and browser owner transitions

- R7 frozen build passes frontend full/beta tests, type/CSS checks, bundle and
  native Wails build. Root full race: 2385 top-level pass, 35 skip. Desktop full
  race: 802 pass, 18 skip. Native WebView2 callback lifecycle also passes with
  race/checkptr enabled. Ordinary test-launch failures remain, as does a missing
  final native UI test executable; neither is replaced with a passing label.
- R7 actual window restores the local browser tab, opens the top account entry,
  hides the native view under the modal, restores it afterwards and exits. No
  model request or permission change. A foreign overlay excludes screenshots.
- Reproduced wrong-tab initialization on actual React A-B-A clicks in Modern
  and Classic: six failed assertions before the fix, zero after (10 total).
  Browser state is now tagged by owner before effects can consume it. No layout
  or authorization behavior changed.
- Use typed COM self/output arguments. Pointer-liveness fixtures compare the
  original numeric address after stack growth/GC before using typed pointers;
  AST checks still require escape annotations. Native response fixtures use
  native memory matching WebView2. No checkptr or race suppression was added.
- Fix SID byte reading and the missing escaping boundary in the sealed UIA
  point lookup helper. Its output was lost during Go callback reentry under
  race instrumentation. The whole-PC feature is still disabled.
- R6 real native local navigation succeeded. The next web turn never sent:
  historical uncertain-send state still requires explicit human recovery;
  durable sending consent is intact. Closed the idle R6 window normally.

## 2026-11-16 transport reuse and browser task clock

- R4 real CSV-b and cross-b completed with verified outputs; cross-b required
  two corrections. R5 code-d failed before any write on a same-chat navigation
  timeout. Preserve both success and failure evidence.
- R6 keeps a ready exact conversation document instead of navigating on each
  tool receipt. Fresh navigation is bounded to two pre-dispatch attempts;
  pre-dispatch failures are KnownUnsent. New page URL guards prevent sending
  after navigation to a different chat. Synthetic raw-response cases pass,
  including two same-document turns and replay rejection.
- Browser children now allow 30 minutes per parent turn excluding approval
  waits, and notify the inherited model clock of human waiting. Cancellation,
  denial, caller deadlines and cumulative action limits remain intact.
- Browser-related race suite and main desktop full race tests pass in normal
  Windows permissions. Whole-suite race still fails at four native checkptr
  boundaries; ordinary main desktop test launch still returns Access denied.
  Neither result is concealed or worked around by changing security settings.
- Consolidated module status in docs/audits/orca-beta-acceptance-status-20261116.md.
  No paid calls, account deployment, installer release or public update change.

## 2026-11-15 web runtime and reasoning

- Extend active tasks to 30 minutes; keep permission waits excluded and retain
  the 30-action bound. R3 real evidence led to three task-wide corrections and
  35 generations, still at most one correction per malformed reply.
- Count generation duration toward pacing instead of waiting a second full
  interval after every response. Single-account serialization/RPM remain.
- Persist explicit result delivery checkpoints without reinterpreting legacy
  host acknowledgement as confirmed delivery. Add owner/restart/failure tests.
- Preserve timeout cause and no-replay diagnostics through Agent termination.
- Verify existing raw reasoning transport and shared React UI with synthetic
  streams: 16 UI cases, 68 assertions, no failures. R3 also displayed native
  reasoning before completion. Code-c independently passed 1000 random cases;
  final response failed the exhausted correction cap, not the new time limit.
- Preserve launch failures in root/desktop full suites; do not claim all green.

## 2026-11-15 later: CSV, cross-file and second code task

- CSV-a fully completes through the real UI Agent; output verified independently.
- Cross-a and code-b produce correct artifacts but reach the active five-minute
  cap before final answers. No clean full-turn pass is claimed. Code-b passes
  three original tests and 500 randomized cases without input mutation.
- Clarified that ledger returned is host acknowledgment, not webpage delivery.
  Last-result delivery and timeout/finalization remain acceptance gaps.
- Fixed unknown context rendering only, preserving known empty-context zeros;
  five new assertions, full frontend tests, CSS/types/build and diff check pass.
  Native rebuilt/rendered verification is pending. No release or deployment.

## 2026-11-15 code-a real Agent continuation

- User approved real writes. Agent fixed invoice.js, executed existing tests
  and wrote RESULT.md. Independent 3/3 tests, 500 randomized value/mutation
  checks and unchanged input hashes passed.
- Original turn stopped at ten generations (eight tools, one correction), not
  an approval timeout. Same-chat explicit read-only follow-up completed with
  two reads and no write replay. Preserve the original incomplete-turn result.
- Added precise budget diagnostics and fail-closed negative/overflow checks;
  no quota, consent, replay or tool-permission relaxation. Web race suite passed.
- Root full rerun: 56 passing packages; boot/bot/serve executables refused at
  launch. Keep Access denied failures and do not claim a passing full suite.
- CSV scenario is waiting for a Node tool approval. No release/deployment or
  new executable claim; current UI still uses the earlier frozen candidate.

## 2026-11-15 later: native candidate and final engineering checks

- Rebuilt the reviewed beta source into an internal Windows candidate; kept
  existing authorized test data and formal-version data separate.
- Full root/desktop suites, relevant client/server Windows race suites, frontend
  tests/build and three-platform core/host-package builds passed. Native UI:
  16 combinations, 164 clicks, 112 screenshots, plus real onboarding Skip.
- Corrected only the native test fixture's overly nested profile location and
  CWD cleanup. Retained first COM/path failure and the cleanup failure evidence.
- Started a new real Web Agent code task; five successful reads, currently
  waiting for write permission. No generated result is claimed as verified.
- Read-only current production checks found two forwarding and 30 reconcile
  records, all older than ten minutes. No state/funds/service changes made.
- New report: `docs/audits/orca-beta-integration-r2-20261115.md`. Source/evidence
  delivery only; no installer, publication, updater switch or paid request.

## 2026-11-15 evening: refreshed server baseline and isolated acceptance

- Read the two user-authorized infrastructure chats; verified the current
  Cloudflare SSH path and request-prices server version. Reproduced the live
  binary byte-for-byte, without touching production source or service state.
- Built a separate current-source account integration with the later execution
  lifetime fix. Preserved financial uncertainty, ordinary keys, current price
  tiers, response-model mapping and Chat group policy.
- PG/Redis acceptance: 90 top-level tests / 224 pass events. Actual server and
  ORCA account client: 34 top-level tests / 165 pass events. No failures/skips in
  either isolated run. Four synthetic model calls, zero paid/public model calls.
- Preserved request-price and reasoning metadata in client wire DTOs, with
  round-trip and legacy-catalog regression tests. Client account suite passed.
- Windows desktop test executable was deleted by antivirus before execution;
  recorded the exact matching path/timestamp and halted related Windows/race
  tests. Compiler components match the cached toolchain; that does not prove
  the deleted artifact is clean. No antivirus bypass or quarantine restoration.
- Report: `docs/audits/orca-beta-server-20261115.md`. No deployment or release.

## 2026-11-15: independent module consolidation

- Added a provider-level human-wait hook and web active-time clock. Controller
  approval/Ask suspends only the task timer; outer cancellation/deadlines remain
  effective. Nested waits are idempotent, credits persist before resuming,
  persistence failure cancels execution, and restart cannot invent wait credit.
- Stored actual tool success/error/cancel outcomes in display metadata without
  changing model request messages. Reload no longer treats a missing tool result
  as success or a successful file containing `Error:` as a failed tool.
- Read-only checks against the current public desktop session route returned
  404. Added an 8-second unauthenticated route check before interactive login,
  a public localized unavailable-service message and no-browser-open regression.
  Existing account credentials, prices, funds and services were not changed.
- Updated only synthetic transports to acknowledge the new route probe. Kept
  the initial failed fixture/native runs rather than disguising them as passes.
- Reused boot's config snapshot for desktop runtime-provider preparation and
  trusted-access decisions, with failure/cancellation and single-read regressions.
  Removed one measured duplicate config load; whole-model-switch improvements
  remain inconclusive under 50-skill measurements.
- Final root suite: 59 packages, 3480 pass events, zero failures. Final desktop
  suite: 12 packages, 1171 pass events, zero failures. Counts include subtests;
  skipped opt-in/live tests are not acceptance. Frontend test:all now includes
  the beta connection contract. CSS/typecheck/Vite build passed.
- Windows race checks passed for five core packages and desktop/browserhost.
  Used hash-verified portable w64devkit 2.11.0, not a global PATH modification.
- Native Wails mock matrix: 16 combinations, 112 screenshots; Playwright mock
  matrix: 16 combinations, 546 assertions, 116 screenshots. Native production
  assets also passed first-run Skip and embedded-browser overlay/navigation.
  Installer harness passed 68 offline checks, not an actual install/uninstall.
- No paid requests, website sends, private-chat reads, public release, updater
  switch or server deployment. Full audit and consolidated pending actions are
  in `docs/audits/orca-beta-integration-20261115.md`.

## 2026-11-13: DeepSeek Web V3 real synthetic tasks

- Kept the authorized isolated beta profile; no repeated login/authorization
  prompts, personal chats, paid API fallback or server deployment.
- Diagnosed intermittent website format drift using only new synthetic replies.
  Added a final-only verbose-lead-in path, one-time specific correction for
  incorrect `raw_argument`, and a complete-envelope read-only alias for
  `<tool_call>`/DSML. Mutation still requires V3 and the normal permissions.
- Propagated the registered tool's internal `ReadOnly()` capability without
  serializing it to the model. Read-only aliases require both a known name and
  that actual capability, preventing a writable replacement using the name.
- Corrected response-shape labels and failed-run tool counts. Added protocol,
  replay, spoofing, writable-alias and Agent continuation regressions.
- Real code/CSV/cross-file tasks passed in the native WebView2 test shell;
  several format-drift failures were preserved. See the audit for counts and
  exact limitations. This is not stock-UI or account-billing acceptance.
- Root Go full serial rerun, desktop Go full, frontend full/build and 16-case
  Playwright mock QA passed. Windows race, final native installed UI and
  package provenance remain pending; nothing was published.
- R19 reviewed source selection passed: 285 approved new source files, two
  excluded temporary live fixtures, no unknown paths. R19 live code, CSV and
  cross-file tasks each passed independently; this does not erase prior
  failures or certify billing.

## 2026-11-12: restart, retained consent and local verification

- Resumed in the beta worktree only; the old R9 process no longer existed.
  Consolidated the Web module handoff once, without repeated cross-chat polling.
- Added a failing regression for recovery discarding an existing sending grant.
  Separated the durable grant from temporary fail-closed availability; retained
  replay barriers, manual recovery, paused reservations and active-task guards.
- Added explicit disable, identity-reset/loss, conservative schema migration,
  blocked restart and no-new-consent regressions. Schema 4 prevents older clients
  from silently ignoring the new authorization boundary.
- Updated the settings page to fetch authoritative recovery status and discard
  late pre-recovery polling. Modern/Classic, zh/en recovery clicks passed
  76 assertions and eight screenshots; no new layout changes.
- One real R9 cancellation attempt could not reach the website-ready stage;
  provider requests and task scopes stayed zero. It remains a failed acceptance
  run. Its Engine and Browser both released. No hidden setting was changed.
- An elevated native localhost-only smoke passed with a fresh fixture profile.
  Full core regression exposed harness assumptions: temporary files under a Git
  ancestor and missing shell/search paths. Preserve that failed run and correct
  only the harness environment before rerunning; do not weaken product tests.
- Final frozen candidate, full regression and authenticated acceptance remain
  required. No release, server deploy, real billing or paid model call occurred.

## 2026-11-08: post-R9 bounded cleanup

- Kept R9 immutable. Its native embedded-browser host passed a hidden localhost
  Chinese-form smoke with native input, submit, resize, hide and PNG capture;
  owned test processes exited. This was tester-driven, not an AI task.
- Reproduced fixed-name registry registration surviving test repeats. Added
  test cleanup only; original shuffle seed passed ten repetitions through a
  test-only overlay. Production duplicate rejection remains unchanged.
- Reproduced auto-plan leaving its completion context live and not disabling
  internal thinking. Request-local cancellation and DisableThinking now match
  the existing isolated-classifier policy, without changing parent context or
  session effort. Ten focused repetitions passed after RED.
- Reproduced real MCP stdio handshake followed by call-time stderr+exit leaking
  local diagnostics into model-visible error. Typed local diagnostics preserve
  local Error/cause; remoteTool strips only this transport diagnostic. Normal
  MCP content/IsError/RPC errors remain intact. Focused, full and three-repeat
  plugin tests passed; two existing POSIX-only tests skip on Windows.
- Historical server protocol acceptance: 75 top-level/223 including subtests
  pass, zero skips/failures; fresh PG16.15 plus miniredis emulation, not Redis or
  production. Six actual client protocol sources were mirrored unchanged for
  in-process synthetic HTTP/callback tests. No actual upstream inference or fee.
- These later edits are NOT in R9. New source selection/build and final tests
  are required. Human Web recovery, real model/browser/attachment/MCP tasks,
  actual account login/deployment and installation remain separate gates.

## 2026-11-08: cancellation and auxiliary lifetime review

- R7 real serial memory-tool acceptance passed once. R8 live cancel/recovery
  failed; no tool dispatched and no answer committed after cancellation. The
  original website failure is unknown; no uncertain request was replayed.
- Actual-Agent tests reproduced cancellation inside final Message/Usage/Notice
  callbacks. Received text and real usage remain, without success commit or tool
  execution. Unexecuted complete calls keep cancellation receipts; a synthetic
  Agent/Adapter/Engine next turn reconciles them without replay.
- Controller must join its own Web producers before reporting idle. Manual
  compaction exposed auxiliary-owner/task-owner mismatch too. Scope tracking
  preserves separate auxiliary webpage bindings, nested tasks and API parallelism.
- Active XHR abort was withdrawn after a cached-original-open counterexample
  proved possible upload interference. Site Stop and actual HTTP termination
  remain mandatory; OPENED lifecycle invalidates stale identity only.
- Earlier full Go regression passed 59 root packages and desktop with 38/8
  explicit skips. Later source and candidate gates remain required. Separate
  server fresh-PostgreSQL acceptance passed 38 cases plus two helpers, zero skips;
  Redis/OAuth/current deployed provenance/live billing are not implied.
- Isolated Web settings remain protected. No automated consent recovery,
  private-state inspection, paid request, deployment or publication followed.
- Website-maintainer handoff identifies Sub/Whalestack as current identity and
  CNY-ledger authority. Historical account30 tests do not establish current
  production compatibility. Public-client/refresh and deployment provenance
  are being checked with the migration owner; no old server was deployed.

## 2026-11-08: native response diagnostic follow-up

- Read-only UI inspection confirmed enabled web access and paused reserve
  replenishment. The r5 window was closed normally before profile reuse;
  no consent setting, password, cookie or personal conversation was inspected
  or changed by automation.
- One frozen response-shape diagnostic ran without Agent/tools. It failed
  without usable metadata; Engine, Browser, process and output release were
  confirmed. No retry, protocol guess, paid fallback or publication followed.
- Investigating the actual JS-to-Go diagnostic path offline before another
  live request. See `docs/audits/orca-beta-web-diagnostic-20261108.md`.
- The diagnostic-only Go path now retains fixed local failure categories and
  payload-free counts without accepting rejected metadata. Five independent
  event-waiter/Engine/Adapter cases and the compiled asset digest check passed.
  The follow-up bounded-drain fix passed eight cases over ten independent
  repetitions. Overlong diagnostic answers no longer cancel before a real
  terminal event; cancellation, errors and missing terminals remain failures.
  Native settings remain protected; recovery and sending consent are left to
  the human user. These are diagnostic fixes, not production web acceptance.
- A later read-only UI check confirmed restored consent. The next single live
  diagnostic completed with OK and accepted metadata, but matched no exact
  prompt in two JSON XHR POST candidates (chat match count one). This isolates
  the observation failure to request text matching, not cached fetch or login.
  Edge whitespace normalization is being tested as a hypothesis, not asserted
  as the website's proven behavior. No replay or production parser relaxation.
- A subsequent fresh request matched only the exact command.trim() candidate,
  confirming that boundary difference for this request. Its XHR observer read
  371 JSON frame shapes, without exporting response text. The overall case
  still failed its exact-OK assertion. Ordered patch diagnostics are pending;
  aggregate shapes alone are not a proven production decoder contract.
- The ordered diagnostic subsequently passed a fresh real exact-OK task and
  release checks. It observed p/o inheritance, a complete fragments SET snapshot
  containing prior THINK content, a metadata BATCH and status/close completion
  without a finish event. The scoped raw-response production fix is now being
  implemented; full Agent tool acceptance is still outstanding.
- Implemented the scoped raw task channel and independently passed host tests,
  31 transport/page/wire scenarios and 19 opening/lifecycle scenarios. Review
  closed a missing host opening flag, error-code leakage, bridge-size/Unicode,
  batching and latest-status issues. One symlink test remains skipped for OS
  privileges. A new internal native candidate is pending; no production web
  acceptance, package authorization or publication is implied.

## 2026-11-07: second-round freeze and candidate preparation

- Web task identity revocation now cancels task children synchronously before
  approval can release an old action. The reproduced post-logout execution is
  blocked; independent API parent contexts remain active.
- Main Agent, compaction, auxiliary and browser requests retain their source
  and task scope through result commit. Partial/uncertain webpage responses
  cannot trigger a hidden resend or compaction fallback. Compaction rechecks
  cancellation and history under the session lock before committing.
- Provider-kind admission covers custom aliases and controllers whose original
  configuration was removed. Browser tool receipts stay correlated and occur
  only once; stale observations require fresh evidence before another decision.
- Native synthetic Wails acceptance passed 16 cases, 168 directed clicks and
  112 screenshot checkpoints. Headless acceptance passed 16 cases, 538
  assertions and 116 screenshots. Neither substitutes for real website login
  or actual model-driven task completion.
- The account30 server candidate passed isolated acceptance but is NOT deployed.
  Unknown-usage cancellation may retain a financial reservation and a slot;
  the same behavior exists in the original account30 baseline. Paid acceptance
  remains blocked, without changing live prices, balances or model services.
- The first post-integration desktop run exposed an obsolete receipt assertion
  and the suite-wide 180-second limit. The assertion now checks exact correlated
  results and fresh evidence; the full suite is rerunning with a 600-second
  package bound. Final evidence belongs in the second-round audit.
- The candidate build is for isolated login acceptance only. Eighteen prepared
  real-Agent scenarios remain NOT_RUN; no real model request, human login,
  delivery approval or publication is implied by this freeze.

## 2026-11-06: second-round integration (in progress)

- Target beta.2 while retaining beta.1 deliverables. No client publication or
  stable updater change. Human login acceptance is now authorized to prepare,
  but passwords and codes remain user-entered.
- Added stable host `ParentTaskID` to main, nested, compaction, planning, risk
  and browser requests. Six regression groups first failed, then passed.
- Reproduced the background-startup race with a blocking synthetic browser
  factory. Explicit startup signaling now waits without enabling or sending;
  startup/logout and browser-receipt focused tests pass.
- Web protocol/lifecycle and multi-source account acceptance are being combined.
  Real website tasks and payments are not yet accepted.
- Server preflight found production account30 ahead of the groups28-based
  candidate. Reject the old deployment candidate; preserve newer output-budget
  and billing policies in a separate isolated merge.
- Eighteen distinct synthetic Agent tasks and a read-only artifact verifier are
  prepared; 43 fixture self-tests passed, actual Agent runs remain NOT_RUN.
- Current evidence and remaining gates:
  `docs/audits/orca-beta-second-round-20261106.md`.

## 2026-11-05 (Asia/Shanghai)

- Created independent `codex/orca-beta` worktree at core `d2389092`.
- Imported 66 browser-preview source/test/documentation files by explicit
  allowlist, with byte hashes checked. Excluded community.json and unrelated
  readiness/live-model reports. Original worktree unchanged.
- Copied 3,501 server backend source files into an isolated candidate directory;
  did not deploy or alter live services.
- Baseline root provider/boot/config tests passed (workspace-local Go cache).
- Baseline frontend CSS/type/production build passed using Node 24.19.0 and
  regenerated-source-compatible baseline bindings. Full new bindings pending.
- Added host-owned HTTP client injection for existing OpenAI/Anthropic adapters,
  to reuse their wire protocols with scoped runtime credentials and receipts.
- Added browser request usage attribution for probes, actions and verification;
  focused regression pending full backend integration compilation.
- Removed duplicate Skill catalog filesystem scan. Synthetic 50-file benchmark,
  three short samples: previous 25.5-26.8ms, single snapshot 12.4-14.4ms. This is
  not a whole-application startup speed claim. Catalog isolation regression passes.
- Dependency installation reported four low and two high advisories. Review
  reachability and patched versions before delivery; no automatic major update.

## Outstanding gates

Historical checkpoint. The current gate status is maintained in
`docs/audits/orca-beta-automatic-20261106.md`; the dated results below retain
failures as well as successful follow-ups.

## 2026-11-06 (Asia/Shanghai)

- Account requests use server-granted, group-scoped credentials only in the Go
  transport. Unknown Responses publications are rejected rather than silently
  rewritten as Chat Completions. Server receipt values remain exact CNY strings.
- Added a no-request-replay context for account inference. Even a disconnect
  before the first token cannot start a second inference attempt. Ordinary API
  retry behavior is unchanged. Provider suites and repeated adapter tests pass.
- Added a bounded DPAPI receipt journal. Initial persistence precedes sending;
  unresolved entries cannot be evicted to admit more paid work. Recovery queries
  receipts, never replays model requests. Lifecycle integration remains under QA.
- Native browser host-only Wails check passed with synthetic localhost content.
  The separate full-UI test found the preview-only window-class lookup. Host
  construction now explicitly receives the beta class, retaining the current-PID
  boundary. Unknown/empty classes do not fall back to another window. Focused
  class regression passed ten repetitions; full native matrix is being rerun.
- Frontend full tests/build passed. Synthetic beta geometry/finance checks:
  394 assertions, 80 screenshots; browser panel checks: 119 assertions, twice.
  These do not certify real OAuth, real model operation or native occlusion.
- Root tests with a synthetic clean C-drive profile passed 57 packages and
  failed two Windows shell cases. Repeated D-drive atomic rename failures are
  recorded separately. These failures are not waived as sandbox noise.
- The production server baseline was rebuilt byte-for-byte. Three disposable
  server runs have not passed acceptance: the latest diagnostic found Redis
  configuration read permission failure before business tests. Owned resources
  were cleaned successfully; production containers and data were untouched.
- The patched Vite/source-map dependency update removed the two high audit
  findings; four low findings remain. No forced major dependency upgrade.
- No real model requests, paid tests, client publication, stable-source switch
  or production server deployment has occurred in this integration.

### Automatic follow-up

- Full root follow-up passed 59 packages, and full desktop follow-up passed 12
  packages before the final review changes. Existing skips were recorded rather
  than counted as exercised features. Final snapshots are being retested.
- Removed inherited WindowsApps WSL bash aliases from shell selection. Added a
  bounded Windows sharing-error retry around atomic rename, retaining the old
  destination on failure. Session/vision and browser metadata tests pass.
- Native Modern/Classic UI matrix passed 16 cases, 112 screenshots and 168
  trusted clicks scoped to owned test HWNDs. Headless geometry/finance checks
  subsequently reached 554 assertions and 116 screenshots. Neither is human
  account login or paid inference acceptance.
- Browser callbacks now follow COM reference lifetime and clear completed
  one-shot captures. Repeated cancellation/teardown regressions pass. An approved
  256 KiB download spent about 60 seconds in native state 0 while the STA stayed
  responsive. Validation/publication/cleanup each took milliseconds; do not
  attribute the delay to a specific security product without further evidence.
- Independent review found and repaired browser profile junction escapes,
  protected-profile transfer admission, owner-close races, and account-catalog
  browser model resolution. WebBeta now latches uncertainty if the completion
  checkpoint cannot be saved, before any subsequent queued request can send.
- Wired account admission into actual submit/display/guidance/runtime-drain
  paths. Public-entrypoint tests verify detached non-cooperative controllers
  keep logout pending and block a new identity. Expired sessions now expose a
  sign-out recovery path rather than an unusable sign-in-only button.
- Account request errors carry a no-replay marker beyond the HTTP adapter.
  Agent stream continuation and compaction fallback respect it; received text
  survives, partial tool calls are not executed, ordinary API recovery remains.
- Account receipts preserve exact integer CNY, generation ownership, DPAPI
  persistence and per-receipt storage-error recovery. Turn aggregates explicitly
  cover retained requests, not a complete historical invoice.
- Disposable PostgreSQL/Redis acceptance passed 67 top-level tests / 192 pass
  events. Separate full candidate startup passed migrations, synthetic consent
  denial/allowance, public PKCE, session and revoke. The native protocol/client
  stage is still running through fixture validation; no production deployment.
- Skill build snapshot reuse reduced synthetic 50-skill warm controller-build
  medians from 1157/1223 ms to 331/312 ms in two runs. This is not a native cold
  start or model response latency claim.
- Packaging now uses an exact source inventory and reviewed new-file hashes,
  not a recursive dirty-worktree copy. Source, frontend build inputs and beta
  identity overrides have separate provenance. Actual artifacts remain gated.

### Final automatic integration follow-up

- Serial full suites passed: root 59 packages (37 existing/opt-in test skips),
  desktop 12 packages (18 skips). The earlier parallel statusline failure remains
  recorded; ten isolated statusline repetitions also passed.
- Reproduced and fixed a Wails viewport ordering race. Shared FIFO delivery
  prevents a cleanup hide overtaking a newer show. Deterministic regression,
  20 repetitions, frontend tests/build, and a fresh native 16-case matrix passed.
- Fixed two actual server defects exposed by the real client/candidate loop:
  refresh grant int64 rounding in Redis Lua JSON, and native upstream headers
  overwriting the desktop wallet receipt ID. The candidate remains undeployed.
- Final disposable candidate run `orca-startup-68b99cec4dd6a1cf4f39f5f60671ff52`
  passed PKCE, two refresh rotations, OpenAI/Anthropic success and cancellation,
  exact receipts, query-only restore and revoke. Four synthetic inferences,
  no real model calls, no real spending, and owned cleanup passed.
- Native packaging now shares the cleared build environment boundary; GOENV,
  GOFLAGS overlays and NSIS local configuration cannot inject build inputs.
  Positive controls plus 36 native-build safety checks passed. No claim of
  completed artifact verification or actual installation is made by those tests.
- Final account review repaired background-subagent parent-turn attribution and
  a settlement lookup account-switch race. Both deterministic regressions failed
  before the fix, then passed 50 shuffled runs. Job-owned cancellation remains
  independent; old receipt IDs cannot use a new account's authorization.

### Final artifact checks and first-start repair

- A fresh snapshot build exposed a false packaging check: Go omits linker flags
  from build metadata when `-trimpath` is enabled. Path privacy remains enabled;
  a bounded headless `--orca-build-info-json` probe now verifies the executable's
  actual linked channel/version before any profile or Wails initialization.
  Negative identity, timeout and empty-profile regressions passed.
- The first fully packaged beta passed archive/CRC/version checks and normal
  process exit, but its real screenshot showed a startup save error. Delivery
  was blocked. Nonblank screenshots and exit code zero are not visual acceptance.
- New synthetic Windows files reproduced `Access denied` while readers held
  the destination. The original GUI handle owner is unknown; this does not
  identify a filter driver or antivirus product as the cause.
- Model migration and fallback tab saves now share checked, rename-only
  publication. Windows retries remain bounded to seven attempts (630 ms scheduled
  backoff); expected bytes are checked before every attempt. Persistent failures
  preserve old bytes, validation errors fail immediately, and no delete/truncate
  fallback exists. Fileutil tests passed 50 shuffled runs; desktop migration and
  both save paths passed ten. Linux/amd64 and Darwin/arm64 root builds passed.
- The excluded smoke-capture helper also had a DPI-virtualized bounds bug. It
  now uses and restores a thread-local DPI context; no system DPI or app layout
  was changed. The original clipped screenshot remains valid failure evidence.
- A diagnostic GUI rerun and a new full source-snapshot package are required.
  Their final results belong in the delivery directory's `VALIDATION.md`, not
  in a claim that the rejected first package passed. Human login remains deferred.

## 2026-11-07: Real Web Acceptance and Fidelity Gate

- Human DeepSeek login and explicit sending authorization completed. Native
  ordinary QA passed four fresh tasks, including one after normal restart.
- Recovery acknowledgement persistence and status snapshot isolation repaired;
  old uncertain requests remain non-replayable. No automatic consent added.
- Real production-component memory-tool tasks each executed lookup/add/count
  once but failed final serialization, before and after a wording-only fix.
- Correct raw JSON can become the exact observed invalid frame through Markdown
  rendering. The current innerText pipeline is lossy; do not blame the upstream
  model from that observation or reconstruct missing escaping heuristically.
- Protocol clarification and lossless-encoding/one-correction/no-tool-replay
  regressions passed offline. Main independently repeated the subset three times.
- Root Go regression passed 59 packages and desktop passed; skips remain explicit.
  Paid account enablement remains blocked. No client publication or updater change.
- See `docs/audits/orca-beta-web-fidelity-20261107.md` for boundaries and evidence.

## 2026-11-08: Lossless Web Transport and Bounded Agent Acceptance

### Follow-up in progress, after r6

- R7 fresh snapshot/build, frontend tests/build and native identity checks passed.
  Main root regression passed 59 packages (38 skips), desktop main passed
  (8 skips), and Linux/Darwin amd64 root builds passed. R7-specific actual serial
  task acceptance separately passed one fresh random task: actual lookup/add/count
  order matched call order, each once, and independent answer/receipt checks and
  complete owned-resource release passed. It does not certify cancellation.
- R8 now distinguishes a fully confirmed host cancellation from an unknown
  send. Matched HTTP termination, UI stop, revoked output and native queue drain
  are required. The host rechecks current identity/generation/request/URL/block
  state when consuming the stop receipt, fixing a queued stale-receipt race.
- Confirmed stop archives, never completes, the old conversation; a durable
  owner/request tombstone prohibits replay. Missing evidence, deadline/network
  failure or checkpoint failure retain uncertainty. Schema 3 prevents older
  binaries from ignoring that ledger. Main independent webbeta count3 and all
  29 local raw/page scenarios passed. Native stop/recovery is not yet certified.

- A new regression reproduced out-of-order execution and extra calls after
  cancellation in a serial-required read-only batch. Web BeginTask now supplies
  a task-local serial policy. Each Agent resets inherited policy before source
  admission, preserving API child parallelism. Existing TaskProvider wrappers
  forward it without another execution layer. Three-package targeted tests
  passed ten repetitions, including ordered execution, cancellation, wrappers,
  nested source isolation and existing parallel/segmented dispatch behavior.
- Reused XHR objects retained stale upload classification. The site observer
  now clears that classification on every open before classifying the new URL.
  The synthetic unrelated-response reproduction failed before and passed after
  the fix. Main independently passed all ten offline attachment scenarios.
- Neither change is in frozen r6. R7 build/native acceptance and the full Go
  regression are pending at this checkpoint. No release or server deployment.

- Observed the current UI's own synthetic request without exporting cookies,
  headers, unrelated responses or personal history. Request binding permits only
  exact text or the exact trim of the host command, never fuzzy matching.
- Added a task-only raw response channel with inherited SSE path/operation
  handling, snapshot deduplication, reasoning/text separation, bounded Unicode
  chunks and explicit network plus DOM lifecycle confirmation. Opening warmup
  remains non-executable. The original tool parser, gate and executor remain.
- Independent review fixed an actual Go-to-JS opening flag omission, unsafe
  upstream-error forwarding, split-surrogate/budget issues and completion/drain
  races before building r6. Host Go and 31 browser cases passed, as did 19
  opening/lifecycle cases and five core packages; live paid probes were skipped.
- Built a fresh r6 beta candidate from the reviewed 1,526-file snapshot. Frontend
  tests/build and native identity gates passed. The isolated desktop main-package
  suite passed in 254 seconds with eight explicit skips. Its earlier restricted
  token AccessDenied failure is retained; path and DPAPI checks were not weakened.
- Five authenticated tasks passed through the production native host, Engine,
  Adapter and actual Agent in a test shell. Only lookup/add/count memory tools
  were registered. Fifteen actual tool executions, forwarded receipts and final
  answers independently matched; each tool executed once per task. Three short
  objective repetitions plus one steps and one example comparison passed.
- This small instructed-tool comparison does not establish spontaneous tool
  initiative or a prompt winner. Provider Stream calls are counted separately
  from website generations/corrections; missing website token/fee data is not
  invented. Existing read-only execution may fan out; strict web-only sequential
  dispatch is not certified by these tests.
- Owned processes exited and Engine/browser release plus output EOF were
  confirmed after every task. No automatic permission recovery, paid request,
  account deployment, stable-data change, publication or delivery installer.
- File/code read isolation, full browser/attachment/cancellation acceptance and
  authoritative account settlement remain outstanding. Detailed evidence and
  the exact r6 identity are in the November 8 diagnostic report and HANDOFF.

## 2026-11-13: V3 continuation

- R21 internal candidate built from the reviewed source selection; 16 real
  Wails/WebView2 synthetic UI click cases passed across Modern/Classic,
  zh/en and four widths. The stock candidate window could not be controlled
  by the Windows Computer Use interface, which explicitly denied access.
- A repeated real DeepSeek Web code task exposed invalid `raw_argument` twice.
  R22 makes that one correction path request JSON `arguments.content` only;
  all execution gates and strict parsing remain unchanged. A later code task
  passed its output but the harness lacked `ORCA_TEST_GO`; the synthetic
  project itself passed `go test ./...`. With that environment fixed, new
  code, CSV and cross-file tasks passed end to end (5/5/6 tools).
- Root and desktop Go full tests passed after the R22 change. The R22 internal
  candidate was freshly rebuilt with frontend, Wails identity and hash checks;
  it remains marked `awaiting-real-acceptance`. R21 is stale and must not be
  delivered. See `docs/audits/orca-beta-v3-live-20261113.md` for exact
  evidence and remaining acceptance gates.

## 2026-11-13: Stock UI tasks and raw-argument diagnosis (R23-R26)

- Native Computer Use could finally control the real beta main window. Used
  the existing isolated login/consent, synthetic project only, Search off,
  and per-call mutation approvals. No private chats, paid fallback or account
  settlement requests were used.
- Actual code repair, error recovery and test execution, CSV summary and
  cross-file report tasks completed across explicit user continuations.
  Independent checks confirm four Go subtests, 1477 total cents/five valid
  rows, report facts and unchanged protected inputs. This is not unattended
  three-of-three acceptance: approval timeout and format failure happened.
- R23 separated initial raw transport failures from stop-confirmation errors;
  R24 adds payload-free fragment subtype diagnostics. The earlier
  `raw_fragment_unknown` remains unresolved, not mislabeled as fixed.
- Inspection of only the current synthetic webpage answer found the whole
  report in `raw_argument`, with no raw block. Reordered V3 field validation
  before raw framing so the existing single targeted correction can apply.
  Added missing/truncated-block rejection and one-write Agent-loop regression,
  plus content-free original/correction error codes. No lax text execution.
- R26 was built from the reviewed source snapshot, with byte-verified unchanged
  frontend reuse from R23. R25 source selection correctly refused a stale audit
  document hash; no R25 binary was built. R26 remains diagnostic-only.
- R26 native UI wrote/read back the report (90s), completed an additional
  byte-preserving file transformation (244s, four tools, independently matching
  all 1187 bytes), answered 27+34 with no tools, stopped a task between tools
  without creating its output, then answered 71+18 without resuming that task.
- Root full Go tests passed under isolated, repository-external temp settings.
  Desktop full Go tests passed with a 600s package timeout (main: 197.858s).
  Earlier restricted-sandbox/path failures and the 180s package timeout remain
  documented. Raw browser tests and `git diff --check` passed; no fresh frontend
  source change or new frontend acceptance claim in this backend iteration.
- Evidence: `docs/audits/orca-beta-v3-live-20261113.md`, excluded local verifier
  `.tmp/verify-r26-ui-results.ps1`, and `.tmp/r26-ui-independent-results.json`.
  No publication, deployment, updater change, delivery installer or source ZIP.
# November 15: current account reasoning metadata

Integrated confirmed/unknown reasoning capabilities through catalog validation,
account provider persistence, effort menus and final request serialization.
Auto resolves against the current server default; obsolete explicit choices
fail before credentials or inference. User preferences and legacy catalogs are
preserved. Added exact request JSON, no-charge rejection and refresh tests.
Root (59 packages), desktop (12 packages), four protocol/config race packages
and frontend full tests passed. See the account-reasoning audit for counts,
skips and retained Windows launch failures. No paid request or deployment.

## 2026-11-16: actual account adoption and stop cleanup

- R9 Use current login succeeded after the user's click. Existing authorization
  and website login were retained; 50 old bindings were archived with zero
  active/inflight at the adoption baseline. Do not request another login/grant.
- A new main-UI read-only code-d task read README once and automatically sent
  the tool result back. The next answer used its contents and streamed reasoning,
  but failed on raw_fragment_type_unknown, then stop_unconfirmed. Recorded as
  incomplete; the result ledger remains unconfirmed. Inputs are unchanged.
- Fixed a separate post-error cleanup defect in source: preserve the exact
  request's EOF/loadend receipt for the host stop handshake while suppressing
  all further output. Parser failure does not waive identity, request ownership,
  cancellation or no-replay checks. Unknown fragments remain rejected.
- Offline browser 44 cases and XHR identity eight cases passed. Full host race
  suite: 21 top-level pass, one opt-in live skip, no failures; Go-to-page wire
  tests additionally ran 49 browser cases. Source is newer than running R9.
- No new candidate, installer, source ZIP, paid request, deployment or public
  release. Unknown fragment semantics and the broader version gates remain
  open. Details: docs/audits/orca-beta-account-switch-20261116.md.
- Added a frozen-R9 negative control: the new failed-request handshake test
  fails on old source but passes after the fix. It did not edit the old binary.
- Static inspection of public, unauthenticated official frontend assets found
  more fragment categories; added fixed diagnostic labels only, not permissive
  execution. Final core race 157 pass; host race 21 pass/one live skip, including
  61 browser wire cases. No real retry or fresh native acceptance was claimed.
- Rechecked production read-only and reran server source provenance: current
  server and protected policies unchanged; desktop extension remains undeployed.

