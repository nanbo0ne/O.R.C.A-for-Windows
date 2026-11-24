# DeepSeek account switch compatibility: R9 source

Internal candidate work, not a release certificate. Date: 2026-11-16.

## Reproduced user failure

R8's Open webpage button exposed a signed-out DeepSeek page. The user signed
in personally, then the read-only recovery check returned the identity error.
No password, Cookie or historical chat was read. The account identity latch
intentionally rejected old conversation reuse after a login boundary, but the
application offered only logout/recheck, not adoption of the current login.

Two additional code gaps were found: identity loss revoked the source grant,
and the native identity latch was not durable across application restarts.

## New transition

- Use current login stops and retires old web controllers, waits for actual
  tools and native handles to finish, and durably archives old bindings.
- It preserves the website profile, ORCA transcript files and an existing
  source grant. Explicitly revoked authorization remains revoked. It never
  sends old context or replays old tool results into the new account.
- A fresh native page is checked read-only before new conversations can run.
  Initialization, drain and check failures retain a retryable pending state.
- An empty owned-profile marker persists observed identity loss. It contains
  no account identifiers or website content. Only the explicit drained
  adoption path clears it; ordinary restart and generic recovery cannot.
- Old owners remain invalid. API controllers and ORCA account sessions are
  not retired. Background openings remain paused after adoption.
- Opening the webpage from settings now preserves the unsaved settings draft.
  Modern/Classic retain their existing shell and Composer arrangement.
- Stored state is version 6. Do not reopen a profile saved by this candidate
  with R8 or older binaries. Previous candidate binaries are kept unchanged.

## Evidence and limitations

Evidence root: `.tmp/integration-20261115-r2/`.

- Initial React account-switch clicks: 8 cases, 68 assertions, no failures,
  Modern/Classic, Chinese/English, 760 and 1366 widths (synthetic bridge).
- Real Agent/tool cancellation fixture passes for current, detached and rebuilt
  web controllers while an API controller continues. This is synthetic model
  execution, not a real DeepSeek task.
- An initial failed-release test exposed closed-engine status discarding the
  pending switch and grant. Fixed and rerun successfully; failure is retained
  in `desktop-090339-548d353b`.
- Provider race: 156 top-level pass, no skips/failures, exit 0, evidence
  `root-092047-2ec5ed9a`. Desktop main plus host race: 691 pass, 8 opt-in skips,
  no failures, exit 0, `desktop-092105-d8d29cf7`.
- Full ordinary Go: root 2387 pass/35 skip, desktop 809 pass/18 skip, no failures,
  exits 0. Evidence `root-092749-8411fb00`, `desktop-092805-08dbdb57`.
- Final React: account switching 8 cases/68 assertions/8 screenshots;
  recovery 4 cases/108 assertions/12 screenshots. Both zero failures.
  Evidence `beta-account-switch-r9`, `beta-recovery-r9`.
- Frozen frontend: offline dependency install, fresh bindings, test:all,
  beta unit tests, typecheck, CSS checks and production bundle passed.
- Native Wails: `native-runtime-r9-ready` passed all 16 style/language/width
  cases, nine checkpoints each, 212 trusted native clicks, plus browser preview.
  Widths: 760/1024/1366/1920. This exercises real backend adoption against a
  local synthetic page with no send authorization, not a real DeepSeek account.
- The initial native run `native-runtime-r9` passed eight Modern cases, then
  failed Classic English 760 when a menu-item click preceded native hide
  completion. Its evidence is retained. The fixture now checks actual native
  actionability for at most two seconds before input, never replaying input.
  The subsequent full run needed zero extra native wait in all cases. This
  does not establish that extremely rapid menu opening has no transient cover.
- Native tests use an explicit test-only Go overlay of `beta_wails_ui_test.go`;
  the frozen production source/bundle is unchanged. Overlay path and logs are
  retained. Screenshots for Modern Chinese and Classic English were inspected.
- R9 build/provenance passed. Candidate: `.tmp/beta2-candidate-20261115-r9/`;
  frozen build: `.tmp/orca-beta-build/89c301e7482e43f58849c3c23eefe139/`.
  EXE SHA-256: `4ef9bbfe8a07efb2990f766227417a7f7b052f240f0b6e204f873aa430738d2c`.
  Source manifest: `e7ffac421a49b69d78ddb048c1d768811103a9206176aa69a8a626ff20ee1670`.
  Internal source ZIP: `020d1ffa8970c8784e0af0812198865455310ccb2e960f3ce07e9950114afdf3`.
- R8 exited normally; verified R9 launched with the same isolated profile.
  The actual settings panel shows the preserved grant and Use current login
  button. The user has been asked to perform that explicit account adoption;
  no re-login, logout or checkbox changes were requested.
- The pending-adoption entry above describes the handoff before the user
  clicked the button. The subsequent real result is recorded below. The EXE
  remains an internal candidate, not an accepted installer or public release.

## Actual R9 account adoption and new task

The user completed Use current login. The actual settings UI confirmed that
the login was retained and old tasks isolated; connections showed connected.
Safe metadata confirmed version 6, enabled=true, sendAuthorized=true,
reservePaused=true, no block, 50 archived conversations, zero active bindings
and zero inflight. No credentials, cookies or old webpage chats were read.
Do not request this adoption or grant again.

Evidence: `live/account-switch-r9-baseline.json` under the evidence root.

A new main-UI conversation in synthetic code-a, using DeepSeek Web, was asked
to read code-d's README, implementation and tests and report defects without
writing, running commands or accessing the network. This was not an old task
resume and did not substitute a test transport or harness for the Agent.

- The Agent read `code-d/README.md` once. Its result was automatically sent
  onward, without a manually pasted receipt. The next answer accurately cited
  the largest-remainder rules and planned the next read. Reasoning from both
  responses appeared in the shared reasoning UI.
- The second response failed after 2060 ms at response_seen with original
  trigger raw_fragment_type_unknown. The overall UI turn lasted 68 seconds.
  Stop then reported stop_unconfirmed. The fragment's actual type is NOT
  identified; do not claim this unknown-format failure is fixed.
- Counters increased from 399 to 402 sends and 255 to 256 tool records. The new
  call is read_file, host status returned, resultDelivery unconfirmed. The
  visible answer proves receipt processing, not a completed/durable response.
  Preserve the unconfirmed ledger rather than upgrading it to success.
- Authorization remains true; enabled=false is the separate temporary stop
  state. One uncertain binding remains inflight. No setting was reset, receipt
  resent, or permission automatically acknowledged.
- All three code-d input hashes match the pre-existing baseline; RESULT.md is
  absent. No mutation, paid request or private conversation access occurred.

Evidence: `live/account-switch-r9-readonly-result.json` and
`live/account-switch-r9-readonly-files.json`. Account adoption succeeded but
the real task is INCOMPLETE, not a full acceptance pass.

## Post-R9 source fix: failed-request stop proof

An offline reproduction exposed a separate cleanup defect: parsing errors
discarded the response observer before the host's stop command could consume
its terminal receipt. The parser error also prevented observing EOF/loadend.

- Parsing failure now suppresses output while draining only the exact bound
  response to its real terminal event. No new request is issued. Unknown
  fragments are still rejected, never interpreted as tools.
- The page retains one failed operation for the host stop handshake and blocks
  new sends meanwhile. A bounded timer releases resources if the host never
  responds. Document changes and manual takeover cannot grant terminal proof.
- Actual transport/identity failure remains fatal: only parsing failure may
  coexist with valid EOF. Reused XHR and unrelated loadend cannot prove the
  original request ended. Hung transport remains unconfirmed.
- This source change is NOT in running R9 or its frozen ZIP. No new candidate
  was built or live retry attempted; do not relabel R9 as R10.

Offline verification:

- `raw-response-r10.log`: 44 browser cases pass, including five new post-error
  fetch/XHR/hung/wrong-request/navigation scenarios.
- `xhr-stop-identity-r10.log`: all eight original XHR identity cases pass.
- `desktop-102856-ca9c1dc4`: entire webbetahost race suite, 21 top-level pass,
  one opt-in live skip, zero failures, exit 0. Go-produced command payloads
  also exercised 49 browser cases (44 plus five wire fixtures).
- The new tests were also run against copied, frozen R9 page/transport files.
  They fail at parse-error-fetch: a second send was admitted while the first
  failed request still lacked its stop handshake (2 sends, expected 1).
  Evidence: `stop-regression-r9-baseline/expected-failure.log`. Frozen R9
  itself was not edited. This is a page-adapter reproduction, not evidence of
  a second real user request bypassing the host's separate admission lock.

### Public protocol investigation

Read the public [DeepSeek page](https://chat.deepseek.com/) without login,
cookies or model requests, then statically inspected its two referenced
scripts. They were downloaded as evidence, not executed or shipped in ORCA.
The [official main bundle](https://fe-static.deepseek.com/chat/static/main.9eec11d937.js)
defines additional fragment categories for templates, tips, files, requests,
link reads and website tools. That does not identify this failed response's
type or authorize treating those fragments as Agent commands.

Added fixed diagnostic labels for those eight public categories and for
missing/null/non-string/empty type fields. Arbitrary type values and content
remain absent from logs. All these unsupported fragments still fail closed;
this improves the next diagnosis, not acceptance of the unknown format.

Public asset evidence: `public-deepseek-assets/`; main SHA-256
`cc2bb642ca03bde864a0362f3144c77a85fd04b68fe8f6d9ef6d93a703eab165`.
No full application/profile data was uploaded to any service.

Final targeted reruns after diagnostic additions: `root-105031-8b8cfd73`
passed all 157 top-level webbeta race tests (no skips/failures), and
`desktop-105304-eaf8e310` passed 21 host race tests with one opt-in live skip.
The latter ran 61 browser cases using actual Go-serialized commands, including
12 new unsupported-type classification cases. Both exited 0. The initial
44/49-case logs above remain retained as earlier checks, not the final counts.
Frontend was unchanged and not rerun in this narrow post-R9 iteration.

Remaining: identify the unknown fragment using payload-free owned-request
diagnostics, add a narrowly justified adapter and regression fixture, then
build a reviewed candidate and rerun the actual multi-tool task. Do not use
lossy DOM text, guesses about unknown fragments, or replayed writes.

## Broader version gates

See `orca-beta-gap-triage-20261116.md`. R8 native synthetic UI passed all 16
style/language/width cases plus the browser panel. Ordinary root/desktop suites
also passed in normal host execution. Server desktop extensions remain
undeployed; live ORCA login/settlement and isolated Windows install/coexistence
are still open. No public release, paid request or update-source change.
