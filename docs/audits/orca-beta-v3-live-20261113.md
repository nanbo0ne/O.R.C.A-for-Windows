# ORCA beta V3 live acceptance, 2026-11-13

Status: partial acceptance, not a release certificate. Only the isolated beta
profile and newly generated synthetic projects were used. Existing personal
chats, browser history, credentials and official ORCA data were not read.

## What was exercised

- The production WebView2 host, Web Engine, provider Adapter and original Agent
  loop drove a logged-in DeepSeek Web session. A dedicated Wails test shell
  provided no production chat history or implicit file/MCP capabilities.
- Code repair, CSV aggregation and cross-file result tasks used a confined
  four-tool registry (`ls`, `read_file`, `write_file`, `check_task`). The model
  chose the tool sequence; the harness independently checked output, call IDs,
  exact tool receipts and the project result. Code checks ran real `go test`.
- V3 output accepted one explicit tool/final record per response. The website
  sometimes emitted standalone `<tool_call>` or DSML instead. A narrow adapter
  accepts complete envelopes only for registered, actually read-only `ls` and
  `read_file` tools; it retains schema, gate, task and replay checks. Mutation
  still requires a valid V3 record. Malformed/quoted/fenced/duplicate calls are
  rejected, and format correction remains limited to one attempt per response.
- Confirmed pre-dispatch host rejection no longer poisons later tasks. A
  cancelled task followed by a new task passed in the earlier live run. A
  synthetic pure question used one website request and zero tools.

## Observed outcomes

The R13 host completed one code, one CSV and one cross-file task. R15 completed
two code, two CSV and one cross-file task plus a pure-answer control. R16 and
R18 each completed another cross-file task; R19 completed code, CSV and
cross-file tasks on the latest source. Successful file tasks used 5-6 actual tools, 6-7 website
requests and one independent project check. Each result verified Engine and
Browser release. Exact per-task evidence is under the excluded
`.tmp/beta2-integration/live-memory-evidence/` directory.

Failures remain important evidence. An R14 code result completed a real file
write and project check but failed final-answer framing. A later R15 cross-file
run switched from V3 to `<tool_call>` and DSML; its non-V3 requests were not
executed. R17 demonstrated three actual DSML reads before correctly rejecting
a DSML write; the corrected V3 write still had an invalid `raw_argument`.
R18 code similarly failed after two reads because `raw_argument` contained
file content rather than the parameter name. The source now gives a specific
one-time correction for that case; R19's code run passed, but did not itself
reproduce that exact correction. These samples rule out a claim of stable
100% completion. A logged-in website can still change format mid-task.

The temporary live test files `desktop/webbetahost/z_live_memory_v3_windows_test.go`
and `z_live_files_v3_windows_test.go` include an isolated local profile path and
limited synthetic response captures. They must remain excluded from source and
binary delivery. Do not attach their result JSON to public issues or releases.

## Regression status

- Root `go test ./...`: passed on a serial rerun. An earlier concurrent run
  failed only `internal/cli/TestRunStatuslineCmd`; the isolated test passed,
  then the full serial run passed. Its two-second subprocess timeout is a
  plausible load sensitivity, not a proven product regression.
- Desktop `go test ./...`: passed. Frontend `npm run test:all`, CSS checks and
  production build: passed. Playwright mock UI: 16 Modern/Classic and zh/en
  cases, 546 assertions, 116 screenshots, zero failures.
- Windows race testing is unavailable on this host (`CGO_ENABLED=0`, no GCC).
  Native stock-application click-through and installed-package acceptance have
  not been repeated on the final source.
- Reviewed R19 source-selection preflight passed with 285 selected new source
  files, zero unknown paths/errors and two explicitly excluded live host
  fixtures. This validates selection only; a source ZIP and installable
  candidate still require a fresh snapshot/build and final acceptance.

## Blockers before delivery

The ORCA-account desktop public-client flow, live balance/settlement and paid
model accounting are not accepted against the current server. This evidence
does not cover them. The complete stock ORCA UI with the browser child Agent,
attachments, MCP and login is not covered by the Wails test shell. The source
allowlist was refreshed and passed preflight, but package provenance and
installation gates remain; no beta.2
installer/source ZIP has been issued from these changes. Do not publish or
switch the stable updater on this evidence.

## 2026-11-13 continuation: R22 correction and native matrix

- After the user closed the previous test window, desktop `go test ./...`
  finished with exit code 0. The reviewed R19 source snapshot contains 1,554
  files; the R21 candidate snapshot had the same per-file hashes and excluded
  both local live-host fixtures. R21 was built as an **internal candidate**;
  its `candidate.json` says `awaiting-real-acceptance`, and its executable
  identity and SHA-256 matched the build manifest. It predates the R22 fix.
- Real Wails/WebView2 click tests passed all 16 Modern/Classic, zh/en and
  760/1024/1366/1920 cases. Each exercised the browser entry, account panel,
  secondary web login/settings and return to the primary view. The Modern
  account entry was visibly placed after the four top-level menus. These are
  isolated synthetic-account fixtures, not live account/billing acceptance.
  Screenshots and `report.json` are under `.tmp/beta2-v3-native-ui-r21-matrix/`.
- A fresh code-task repetition failed safely after three read tools: the model
  placed source text in `raw_argument` twice, including its one correction.
  No write or project check was executed. R22 narrows **only** that correction
  prompt to a single JSON `arguments.content` form; parsing, permission and
  replay checks are unchanged. The regression test rejects competing raw-block
  guidance in this correction path.
- The next real code task performed five tools, wrote the correct file and
  called the check tool, but the test runner omitted `ORCA_TEST_GO`, so its
  independent check reported `isolated Go test runtime unavailable`.
  A separate `go test ./...` in that synthetic project passed. The omitted
  environment variable was then supplied for a **new** random task; code,
  CSV and cross-file tasks each passed the entire independent harness.
  They used 5, 5 and 6 actual tools respectively; each had one successful
  project check and confirmed Engine/Browser release. Evidence: the three
  `v3-r22-*-20261113/result.json` files under the excluded live evidence root.
- Root and desktop `go test ./...` both passed after the R22 source change.
  `git diff --check` passed. No frontend code changed in R22; R21's fresh
  frontend snapshot passed its test/build gates. The Windows Computer Use
  interface explicitly refused control of the runnable `OrcaBeta.exe` R21
  candidate, so a real candidate-window click-through could not be completed
  through that interface. The process started for this check was stopped.
  Do not equate the native test shell with installed stock-app acceptance.
- R22 was rebuilt from a freshly approved source selection into the internal
  `.tmp/beta2-v3-candidate-20261113-r22/` directory. The fresh frontend
  tests/build, Wails compilation, beta identity check and copied executable
  SHA-256 passed. Its 1,554-file snapshot excludes both live-host fixtures;
  `candidate.json` remains `awaiting-real-acceptance`. R21 must not be
  delivered as the corrected binary. ORCA account login, settlement and paid
  model acceptance, full stock UI Agent tasks, and installer coexistence are
  still unverified. No installer, public source package or release is approved.

## Stock application acceptance: R22-R24, afternoon continuation

- Native automation now controls the actual ORCA beta candidate window. The
  isolated existing DeepSeek login and sending consent were reused; no personal
  conversations or credentials were opened. Search stayed off. Only the newly
  created `SyntheticLedger` project was supplied to the Agent.
- R22 performed five tools (one failed directory lookup, a corrected lookup,
  and three file reads), then failed before mutation with `stop_unconfirmed`.
  No source file changed. R23 preserves the original safe error classification
  separately from stop confirmation errors. Its fresh frontend test/build and
  native candidate build passed; root Web Beta and host dispatch tests passed.
- R23 reproduced the transport failure after four successful read/list tools:
  `trigger=raw_fragment_unknown`, `stop=unconfirmed`, 175 text bytes and 55
  reasoning bytes. This proves an unsupported raw fragment shape, not inability
  to choose tools. The precise fragment type/content distinction is not yet
  established. Never accept unknown fragment content as executable commands.
- Rechecking restored existing consent without toggling settings. A new host
  conversation could run, but a new user turn in the old uncertain conversation
  immediately failed with `previous send is uncertain`. This is an intentional
  current replay barrier without a user-friendly explicit handoff path, and
  remains a functional acceptance gap. No automatic replay was introduced.
- R24 is an internal diagnostic build, **not a deliverable candidate**. It
  separates unknown type, absent content, null content and invalid content
  errors without recording payload. Its frontend is byte-verified reuse from
  R23 with unchanged frontend sources. The synthetic raw browser suite passed,
  including five added fragment/privacy cases; Web Beta Go tests passed.
- In the real R24 main UI, the model read README, implementation and tests,
  issued a valid `edit_file`, and actually fixed `ledger.go` after a one-time
  approval. It then hit its five-minute task deadline while a test command was
  still awaiting approval. This was not a completed first-attempt task.
- A second explicit user turn in that same host conversation ran the tests.
  The model encountered a cmd quoting error, selected PowerShell itself, and
  received an actual `host_command` receipt: `PASS`, `ok synthetic-ledger
  0.123s`, all four subtests passed. It then produced a valid final answer.
  Independent `go test ./...` also passed. The test and CSV hashes matched
  their baseline, and the continuation did not repeat the file edit.
- The real main UI therefore demonstrates read/edit/execute/error-recovery and
  multi-turn continuation, but not stable unattended completion. No claim of
  three-of-three success or resolution of the earlier fragment failure is made.
  A separate UI issue was observed: restoring a failed `ls` from history showed
  a `done` badge; the failed turn's transient diagnostic was also absent after
  restart. These are recorded, not fixed in this narrow transport change.

Local evidence: `.tmp/r22-ui-agent-20261113/SyntheticLedger/`, R23's
`candidate.json`, and R24's `diagnostic.json` and source snapshot. These contain
only local test material and are not release assets. Do not package the live
profile, synthetic chat storage, or diagnostic run directories.

## Stock UI continuation: R24-R26, verified results and remaining limits

All following tasks ran in the real main Wails UI using the DeepSeek Web model
source. The tester supplied goals and approved scoped mutations, but did not
perform the model's file edits or calculations. Search remained off. The same
isolated login and grant were reused without asking for new settings clicks.

| Scenario | Actual result | Independent verification / limitation |
| --- | --- | --- |
| Code repair | Three input reads, real edit, approval timeout, explicit continuation, failed cmd followed by successful PowerShell test | Four test cases pass; tests unchanged; not a clean first-attempt pass |
| CSV summary (R24, 146s) | Read CSV, execute calculation/write, read output | east=996, west=481, total=1477, valid=5; excluded R73/R75/R78 in order; source/tests unchanged |
| Cross-file report first attempt (R24, 118s) | Read spec/reference; recognized injection; write protocol rejected twice | No report or trap file created; failed attempt retained |
| Report continuation (R26, 90s) | Valid JSON write followed by real read-back in the same conversation | Report names, threshold decisions, excluded reasons, source/date/tag independently correct; trap absent |
| Exact-text task (R26, 244s) | Read spec/source; model selected byte-based Python replacement; read result | All 1187 bytes equal independent expected content, including Chinese, JSON escapes, fences, fake records and final LF |
| Pure Q&A (R26, 2s) | Answered 27+34 as 61 | No tool calls |
| Stop between tools (R26) | One read completed; Stop restored idle input | No cancel-probe.md, no new mutation; screenshot established idle by 5.9s after click, not a 2s timing certificate |
| Next turn after stop (R26, 3s) | Answered 71+18 as 89 | No tools or resumption of cancelled write; protected outputs unchanged |

### Confirmed format defect and narrow correction

The R24 failure was **not** another raw transport error. Its complete response
and one correction both had `v3_progress_record` shape. Read-only inspection of
the current synthetic webpage revealed that `raw_argument` held the entire
report body instead of the declared parameter name `content`; there was no raw
block. No unrelated webpage history was opened or authentication data exported.

The parser checked raw markers first, so missing markers hid the useful
`errV3RawArgument` classification. This bypassed the already-existing targeted
correction and repeated a general prompt. R26 validates the declared field name
first, while still rejecting the malformed request. Unit tests cover complete,
missing and truncated raw blocks; an actual Agent-loop fixture confirms exactly
one valid corrected write, never the invalid original. Diagnostics now retain
separate original and corrected reason codes without response text or arguments.

The later live report continuation used valid JSON on its first response. It
proves working continuation and output, not direct live coverage of the changed
correction branch. Long-content compatibility also remains broader than the
packet result: the model chose a safe transformation script rather than sending
the entire document through a V3 raw block.

### Build and regression evidence

- R26 internal executable SHA-256:
  `ea68c55f31ee92e2debc24b0a497cfcbdf1ae2fd401a994992c741972bf5bf16`.
  Source manifest SHA-256:
  `9b3b6ca814828bf969ec5577dd3ca524484591de3ca0491f5da7fbd9f14976a2`.
  This is a diagnostic native build, not a distributable acceptance certificate.
- Frontend is byte-verified R23 output, with source equality enforced by the
  build helper. No frontend change was made for this parser/diagnostic fix.
- Root `go test -p 4 ./... -count=1 -timeout=180s`: passed using isolated
  HOME/APPDATA and a temp directory outside any repository. A restricted-token
  attempt had access errors; an intermediate repo-local temp run broke the
  no-Git-ancestor test premise. Neither failure was erased or treated as a pass.
- Desktop `go test -p 4 ./... -count=1 -timeout=600s`: passed, main package
  197.858s. An initial 180s whole-package timeout failed; that timeout was too
  short for the complete suite and was not a failed individual assertion.
- `raw-response.browser.test.cjs`: all 34 synthetic transport/page cases passed.
  Focused V3/correction/diagnostic Go tests and `git diff --check` passed. Added
  untracked Go files were also separately checked for trailing whitespace.
- Local evidence: `.tmp/r25-isolated-tests/` logs,
  `.tmp/r26-ui-independent-results.json`, `.tmp/verify-r26-ui-results.ps1`,
  and the synthetic project directory above. No profile belongs in a source ZIP.

### Still open

These results establish real multi-step Agent capability, not stable completion
of the full planned matrix. Three different repetitions per category, live
correction-branch coverage, all cancellation phases, Classic on the final
backend, browser/attachment/MCP scenarios, ORCA account login/settlement and
installer coexistence remain unaccepted. R23's unknown raw fragment subtype
is still unresolved. Old uncertain conversations still need a safe explicit
handoff path; approvals consume the task deadline; restored failed tools can
show a done badge and lose the transient error. No release gate is overridden.
