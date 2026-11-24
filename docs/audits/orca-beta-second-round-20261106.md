# ORCA beta.2 integration and acceptance

Status: IN PROGRESS. This is not a delivery or deployment certificate.

## Scope

- Worktree: `D:\AI-Reasonix\.tmp\orca-beta-worktree`, `codex/orca-beta`.
- Target: `3.0.14-beta.2`; beta.1 artifacts remain unchanged.
- No client publication, stable update change, official-profile access or model
  service restart. Only synthetic task data may be used for acceptance.
- DeepSeek Web is optional and independent of ORCA account login. No silent
  provider substitution or paid fallback. Website usage is not an ORCA invoice.
- Paid limit: CNY 5 and 80 requests, including auxiliary requests. Website calls
  have no cumulative budget; per-task limits are 10 generations / 30 tool calls /
  5 minutes, one correction, serial account transport and no uncertain replay.

## Reproduced And Repaired

1. Provider requests lacked host parent-task attribution although usage events
   carried it. New assertions failed in main/subagent, compaction, coordinator,
   planning and risk paths. `Request.ParentTaskID` now uses the existing stable
   parent turn; six focused groups pass. It is not inferred from user text.
2. A request concurrent with declared background web initialization returned
   "web access is not open" immediately. The desktop startup entrypoint with
   a blocked synthetic browser factory reproduced this. Explicit startup wait
   now resolves on readiness, failure or identity closure without enabling or
   sending. Focused startup/logout/browser-receipt tests pass.
3. Composer preparation could reject outside its error handler. The cooperating
   implementation restored the catch and tested Error/string rejection, draft
   preservation and send-lock release: 37 synthetic cases passed.
4. Account receipt attribution missed the new parent ID; negative settled amounts
   were accepted. Account-side regressions first failed, then passed after
   attribution propagation and invalid-money rejection. Zero and unknown remain
   distinct. Full integration reruns are still required.
5. A dispatched web failure could enter Agent recovery or summary fallback and
   create a second request. Actual Agent/CompactNow regressions reproduced this;
   dispatched/uncertain/empty-web completions now carry a no-replay error.
6. Identity closure could look like successful EOF after a partial summary. The
   adapter now guarantees a terminal failure, and all three summary entrypoints
   hold provider task ownership through commit. Summary collection requires a
   normal completion marker, rejects tools, and observes cancellation. Commit
   checks cancellation and unchanged source history under the session lock;
   messages, display epoch and rewrite version update together.
7. Asynchronous identity cancellation left a permission-gate race. A deterministic
   actual-Agent regression executed one tool after identity stop before repair.
   Registered web child contexts are now cancelled synchronously before stop
   returns; the ordinary API parent is not cancelled.
8. Native web DOM fixtures reproduced missed fast replies, aria-checked toggles,
   and ambiguous new responses. Positive transient-busy evidence plus one stable
   new assistant response is required. Missing evidence returns an uncertain-send
   error rather than retrying. Eleven headless DOM scenarios passed three times;
   these are synthetic DOMs, not proof of current official-site compatibility.

## Protocol And Lifecycle

- Strict JSON V2 progress plus one complete tools/final record; older bound
  conversations keep their old grammar. No arbitrary prose scanning for calls.
- Validated calls are recorded before delivery. Results must match the original
  invocation, ID, name and arguments. Missing/duplicate results do not replay.
- Progress can appear before completion; terminal final JSON currently waits
  for complete validation. This is not character-level final streaming.
- Engine task scope is integrated through Agent and browser lifecycles,
  not only a single Stream call. Logout must cancel in-progress tools and wait
  for actual exit. Old parent tasks cannot adopt a new webpage identity.
- These are offline results, NOT authenticated website acceptance. JSON remains
  the default; no DSML superiority has been established.

## Server Gate

Read-only preflight observed `0.2.19-whalestack.30`, executable SHA-256
`01913f5591902e9eb4aed84f2c335fdd94418c0d65629801da0477c883247493`.
The old Native5 candidate is based on groups28; its own base guard rejects the
current source. Deploying it unchanged would remove newer billing behavior.
The desktop session route returned 404. The new account30 candidate passed
isolated PG/Redis integration (72 top-level tests, 202 pass events), exact old
binary upgrade/rollback, and four actual-client synthetic inference flows.
Production still has not changed. Full serving topology and human acceptance
remain unverified.

The follow-up found an unresolved financial lifecycle: cancelled requests with
unknown final usage retain their hold and a concurrency reservation indefinitely.
An explicit final receipt settles idempotently, but no automatic time bound was
established. Do not enable the paid desktop API or invent refunds, maximum-price
charges or receivables as a test workaround. The server remains NO-GO pending
review of this behavior. Existing balances, keys, prices and model services were
not changed. Evidence: `D:\AI-Reasonix\.tmp\orca-beta-server-account30`.

Evidence: `.tmp/beta2-server-preflight/README.md` and accompanying hash/route
inventories. No production changes or money mutations were made by that audit.

## Real Acceptance

Fixtures: `D:\AI-Reasonix\.tmp\beta2-acceptance`. Six classes with three distinct
nonce variants each: search, code repair, CSV, image/TXT, browser and MCP.
43 fixture self-tests passed; all 18 actual Agent tasks currently NOT_RUN.
Only ORCA may execute the task; the harness sets up inputs and verifies outputs.

- Human ORCA login: NOT_RUN.
- Human DeepSeek secondary-window login: NOT_RUN.
- Actual website Agent tasks / paid settlement: NOT_RUN.
- Modern/Classic native clicks on this candidate: NOT_RUN.
- Headless production-frontend bridge: 16 cases, 538 assertions, 116 screenshots,
  no failures. Chinese/English, Modern/Classic, 760/1024/1366/1920px.
- Native Wails synthetic connections: 16 cases / 168 directed clicks / 112
  screenshot checkpoints passed on 2026-11-06; only owned
  test windows and loopback fixtures. No live website/model/login. Evidence:
  `.tmp/beta2-native-20261106-233011-d16fe620`.
- Root Go suite: 59 packages passed, 37 skipped live/platform tests. Two initial
  failures were caused by placing a no-Git fixture under the repository; the
  final frozen-source rerun under `C:\orca-beta-test-tmp` passed on 2026-11-07.
  Evidence: `D:\AI-Reasonix\.tmp\orca-beta-validation\20261107-104413-701c71bc-Full.summary.json`.
- Final desktop Go suite: 12 packages passed, 18 skipped platform/opt-in tests,
  no timeout. Evidence:
  `D:\AI-Reasonix\.tmp\orca-beta-validation\20261107-104024-62c98df1-Desktop.summary.json`.
  The earlier stale-receipt assertion failure and suite-wide 180-second timeout
  remain failure evidence; the repaired test checks exact ID/name/result and
  fresh observations, and the complete suite now uses a 600-second bound.
- Frontend `test:all`, CSS/type checks and production build: PASS. Existing Vite
  large-chunk warning remains. Fresh candidate build and installer suites: PENDING.
- Race tests: not established; the existing Windows environment has CGO disabled
  and no discovered compiler. Do not convert this into a passing result.

Do not package an accepted beta.2 or deploy based only on this interim report.
Replace pending statuses only with observed evidence from the actual candidate.

## Test Isolation Review

An auxiliary-test helper initially did not override APPDATA itself. Review found
the existing package TestMain already redirected AppData to its own temporary
directory; the demonstrated problem was shared state between synthetic cases.
The historical exact child environment was not retained, so this is not a full
file-access audit. An authorized metadata-only check of the real sidebar file
found its last-write timestamp predates this run; no contents were opened,
hashed, copied, deleted or restored. No personal-file write was established.
The helper now uses per-case desktop test homes and asserts the sidebar path is
inside that home. All further full suites use the cleared-environment runner.
Evidence: `D:\AI-Reasonix\.tmp\orca-beta-privacy-metadata-evidence-20261106.md`.
