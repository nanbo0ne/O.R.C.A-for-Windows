# Web runtime follow-up, November 15

Status: source and automated checks in progress; not a release certificate.

## Changes

- Increase active web task lifetime from 5 to 30 minutes. Actual permission and
  Ask waits remain excluded. Allow 35 generations for 30 tool actions, up to
  three corrections across distinct replies, and completion. Each individual
  reply is still corrected at most once. One generation can wait 10 minutes.
- Remove the additional completion-to-start cooldown. Admission still enforces
  single-account serialization, rolling requests per minute, and the sampled
  start-to-start interval. Generation time now counts toward this interval.
- Separate host acknowledgement from webpage delivery. New receipt fields track
  pending, sending, response_received and unconfirmed. An HTTP response is NOT
  evidence that the model understood the tool result or completed the task.
  Legacy records remain unknown; interrupted sends are unconfirmed after restart.
- Preserve the precise active-time limit in cancellation diagnostics, without
  enabling automatic request replay.
- Reuse the existing native raw-response reasoning stream and shared reasoning
  UI. Do not scrape rendered Markdown or invent hidden reasoning/token usage.

## Automated evidence

- Root webbeta/provider/agent tests passed at root-211319-284f5711.
- Raw-response browser tests passed using the bundled modern Node runtime.
  Added tests assert reasoning before final response, ordered Unicode text,
  batching and no post-cancellation emissions. The first system-Node attempt
  failed due to its unsupported Node version; that log is retained.
- React UI browser tests: 16 cases, 68 assertions, 16 screenshots, zero failures
  at .tmp/integration-20261115-r2/web-reasoning-ui-r3. Both Modern and Classic,
  Chinese/English, and 760/1024/1366/1920px were exercised. User collapse remains
  collapsed on subsequent chunks, and reopening shows the concatenated text.
  Two earlier test-selector failures are retained; they were harness defects.
- R3 build, frontend full tests, CSS/types and bundle build passed. Its hash is
  47fb1ca434af8ed64232215f57c8e6a8ff5db8e972c5dd7c90c430744d4afc9b.
- Race run root-213020-737f7075 passed 511 top-level tests in four packages.
- Root full run root-220008-5cb919fa: 56 packages/2286 top-level tests passed;
  boot/bot/serve binaries could not launch (Access denied). Desktop full run
  desktop-214041-cd3fa24c: 11 packages/110 top-level tests passed; main desktop
  binary was missing on launch. No failed test assertion was recorded, but the
  full runs are NOT passing. No security workaround was applied.

## Real R3 task

Code-c reached 6m04s and 11 generations, exceeding both former limits. Eight
tools completed, all eight results were followed by a confirmed webpage answer.
The real native UI displayed reasoning before that answer completed; collapse
was also clicked. Independent verification passed four existing tests, 1000
random cases and 1000 input-mutation checks, with 20 inputs unchanged and no
unexpected files. Evidence: live/code-c-independent.json and runtime-native/.

This is NOT a clean full-turn pass: final prose lacked a valid V3 record, and
the earlier tool-format correction had exhausted the one-per-task repair cap.
The final repair was not dispatched; the generic protocol diagnostic obscured
that distinction. The subsequent source change permits at most three repairs
over separate replies and reports quota exhaustion explicitly. It does not
execute plain text or retry a second time for the same malformed reply. A new
Agent regression covers an earlier write correction followed by a separate
final correction, with only one actual write. R4 live verification is pending.

The authorized profile was preserved. For the internal test profile only, close
behavior was changed from background to exit and process display to detailed.
An installer IPC shutdown attempt found no listener because beta intentionally
disables that production interface; R2 was instead exited normally through its
UI. No process was force-killed and no web authorization was reset.

## R4 continuation and native browser

- R4 built from a reviewed frozen snapshot. Executable SHA-256:
  `8b4966cc5b4f73d3a68355601e61f0ecc2150dd075d7d81c777d5647b76a8eea`.
  Sandboxed startup had WebView2 child failures and no window; the exact idle
  test process was stopped after path/window checks. The same binary launched
  successfully in the normal desktop environment. No AV changes or repacking.
- CSV-b completed in 3m50s, eight generations, six actions and one correction.
  Independent verification found exact total 6090 cents, correct quoted CSV,
  deduplication and rejected rows, all 25 inputs unchanged and no extra files.
  A product-name instruction was retained only as data. Evidence is under
  `live/csv-b-independent.json` and `runtime-native/csv-b-final-r4.png`.
- Cross-b completed after ten generations, seven actions and two corrections.
  All seven tool-result deliveries are confirmed. Independent checks matched
  dependency ordering, versions, exact quoted/backslash/newline notes, all 30
  unchanged inputs and no extra writes. The embedded tool-call example was
  copied as data, not executed. Cancellation of the preceding browser task did
  not prevent this new user turn. See live/cross-b-independent.json.
- Native manual browser checks passed: local navigation, form submission,
  back with preserved form values, target-blank as an internal tab, and hiding
  the native page beneath the account modal. Fixture POST recorded one submit.
  `type_text` interacted incorrectly with the active IME; UIA set_value then
  preserved the Chinese field. This is NOT a complete IME acceptance result.
- A real main Agent delegated browser_task using the inherited Web provider.
  It reached explicit origin/model-exposure consent. The test was cancelled,
  without granting permission, and the fixture recorded no further POSTs.
  This is delegation/cancellation evidence, NOT a passed browser Agent task.
- Opening that active task's panel exposed a redundant select-command error.
  The frontend now observes an already leased tab without manual selection;
  pending consent remains covered, and another owner's lease is not adopted.
  Browser rendering regression: 23 cases, 133 assertions, 23 screenshots pass.
  Frontend full tests, types, CSS and build also pass. R5 native recheck pending.
- Installer safety harness passed 68 synthetic checks. It did not execute an
  installer or modify registry/shortcuts; real VM coexistence remains pending.
- Production read-only recheck retained the known binary hash and the same
  two forwarding / 30 reconcile records. No deployment or paid request.
- Normal-permission retry of boot/bot/serve still failed to launch with Access
  denied. This is retained as an environment gate, not a passing Go full run.

## R5/R6 continuation (2026-11-16)

- R5 hash: `e0b04da2159c073be50eb8e8a1ed21c565bde503d732b4b3e66534ab5f205f37`.
  Code-d did NOT pass: four read-only tools, six generations, one correction,
  then navigation_timeout before dispatching the next result. Three receipts
  had response_received; the last remained unconfirmed. No write was executed.
  Evidence: live/code-d-turn-r5.json. Keep this failure in the denominator.
- The transport unnecessarily navigated to the same chat for every receipt.
  R6 reuses only an exactly matching, ready, idle document with a valid host
  generation and no block. New navigation gets at most two 30-second attempts
  before any dispatch; failure is KnownUnsent, not an uncertain sent request.
  Page-side URL checks run before sending, including after asynchronous waits.
  Neither generation ownership nor the replay ledger is relaxed.
- Raw-response browser regression passed 39 synthetic cases, including two
  sequential requests in one document, repeated-ID rejection, a wrong document,
  and navigation during pre-send waits. Initial NODE_PATH error is retained in
  the failed harness log; the corrected dependency path run passed.
- Browser child time was still five wall-clock minutes. R6 changes it to
  30 minutes per parent turn, extending only for actual consent wait periods.
  Parent provider human-wait hook is paused too. Cancel, denial, caller deadline,
  cumulative action budget and exactly one pending approval remain enforced.
- Browser-related desktop race suite passed in the normal Windows environment
  (desktop-235216-307e88ce). Its sandbox attempt failed DPAPI/private-file access;
  those errors were not attributed to application assertions or AV.
- Full desktop race run desktop-000536-10f70ab2 passed the main desktop package,
  but four native packages failed checkptr at uintptr-to-pointer boundaries:
  browserhost/callbacks_windows.go:79; webviewabi/pointer_liveness_windows_test.go:33;
  computeruse/backend_windows.go:955; webbetahost/login_only_windows_test.go:72.
  These are NOT Access denied failures. No checkptr/race checks were disabled.
  Plain full-suite and native candidate verification remain separate gates.

## Current remaining gates

R6 native follow-up: the local /second fixture rendered in the embedded view
with the correct URL and Chinese text. A newly forked synthetic conversation
retained default Ask permissions rather than copying full access. Its read-only
request was rejected BEFORE sending because old send_unconfirmed state remained.
Durable sendAuthorized is still true. Neither authorization nor old ambiguous
records were edited; no new paid/web request occurred. R6 exited normally.

R7 source follow-up fixes the transient foreign-tab error: passive effects on
the first render after an owner change used the previous owner's browser state.
State snapshots now carry ownership. Real React component clicks in both styles
reproduced six failed assertions before the change; all ten pass after it.

Native race diagnosis additionally found typed callback provenance loss and a
missing COM escape boundary in sealed UIA point lookup. Typed callback arguments,
validated SID byte access and an explicit escaping call boundary replace the
unsafe round trips. Fixture pointer addresses are still compared across GC and
stack growth; no checkptr/race suppression. Final R7 root full race passes 2385
top-level tests (35 skipped), desktop passes 802 (18 skipped). The opt-in real
WebView2 callback lifecycle passes. Plain runs still fail to launch boot/bot/
serve and desktop main tests. R7 native UI matrix failed before startup because
desktop.test.exe was missing. Actual R7 UI clicks separately confirm startup,
local browser restoration, top account entry and modal occlusion/restoration;
the app exited normally. No model send, permission change or account login.

R7 executable SHA-256:
`b96be793cb1fe83ffb4ce123e6cab3b26dbee02bc1d834ee5bbf5c6e56628e1c`.
Frozen frontend full/beta checks, CSS/types, bundle and native build pass.
Browser panel regression: 23 cases/133 assertions; owner transitions: 2/10.
Three-platform core and host-package compilation passes, not full non-Windows
desktop runtime acceptance. Final source delivery differs only in audit docs.

Run new-candidate real web tasks to completion, inspect actual reasoning and
receipt checkpoints, and independently verify artifacts. Continue the browser,
account and billing matrices independently. Production account extension is
still not deployed; no live account login/settlement or installation acceptance
is claimed. Preserve existing DeepSeek consent and all stable application data.
