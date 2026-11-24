# ORCA beta handoff

Status: integration in progress. This file is not a release certificate.

## Latest: 2026-11-16 R9 account-switch source

Read `docs/audits/orca-beta-account-switch-20261116.md` and
`docs/audits/orca-beta-gap-triage-20261116.md` before older entries below.
The user has signed in personally in R8. Do not ask for login or grant again.
R9 adds explicit adoption of that login without deleting website data, while
retiring old tasks/bindings. State version 6 is not readable by R8; do not roll
back only the executable after this profile is saved. R9 is now running from
`.tmp/beta2-candidate-20261115-r9/OrcaBeta.exe` (SHA-256 starts `4ef9bbfe8a07`).
The user completed Use current login; actual connection/adoption succeeded.
Do not request adoption, logout, re-login or repeat checkbox grants. A new
read-only code-d task read README once and automatically sent its receipt;
the second response failed on raw_fragment_type_unknown, then stop_unconfirmed.
That task is incomplete and its ledger remains unconfirmed. The current
source additionally fixes premature failed-request observer cleanup; 44
browser cases, eight XHR identity cases and the host race suite pass (21
top-level, one live skip, 49 Go-wire browser cases). This source fix is NOT
in running R9 or its frozen source ZIP. The actual unknown fragment type
remains unidentified. See the updated account-switch audit for evidence.
Subsequent public-source diagnosis added 12 fixed, non-payload fragment
categories without accepting unsupported content. Final targeted race reruns:
157 core tests, 21 host tests (one live skip), 61 Go-wire browser cases; all
passed. The current-login baseline and failed-task metadata are retained.
Server source/protocol were read-only rechecked at 10:36 CST: desktop route
still 404, production unchanged, source policy hashes unchanged. No deployment.
R9 full root/desktop
Go, relevant race, frontend build and native synthetic UI checks passed. Read
the account-switch audit for the retained first-run transient-cover failure.
R8 native UI and ordinary full Go suites passed; the earlier R7 notes
about those test-launch gaps are historical. Production desktop extensions,
real account settlement and isolated Windows installation remain unverified.

## 当前有效结论（2026-11-16 R7）

请先读 `docs/audits/orca-beta-acceptance-status-20261116.md`，后面的旧记录按日期理解。
当前源码在 `D:/AI-Reasonix/.tmp/orca-beta-worktree`，分支 `codex/orca-beta`，尚未提交。
R7 冻结构建位于 `.tmp/orca-beta-build/fb5cd9d6e9994b1386bdf51d608816fe/`；
候选位于 `.tmp/beta2-candidate-20261115-r7/`，版本 `3.0.14-beta.2`。
EXE SHA-256 为 `b96be793cb1fe83ffb4ce123e6cab3b26dbee02bc1d834ee5bbf5c6e56628e1c`。
根模块/桌面全量竞态已通过；普通全量和最终原生 UI 夹具仍有启动被拒绝/文件消失。
R7 实际窗口可启动、恢复内置网页，账号面板正确遮挡及恢复网页，已正常退出。
真实账号和扣费未通过，线上新增接口未部署；旧网页不确定发送仍需用户核对一次。
没有新付费请求。不得把候选或此源码包当作已完成验收的安装器，不发布或切换更新源。
交付源码仅比冻结构建多最终报告文字，生产代码按 manifest 逐项核对；不包含资料、凭据或聊天。

## Current web runtime follow-up

R7 source follow-up: native callback self/out pointers retain typed provenance;
SID reads avoid an x/sys integer-to-Go-pointer round trip. The sealed whole-PC
backend remains disabled. BrowserPanel state now carries its owner: synthetic
A-B-A clicks reproduced cross-owner activation in both styles before the fix
and pass afterwards (2 cases, 10 assertions). Do not substitute these for native
or model acceptance. R6 loaded the local native fixture, then exited normally.
Its new read-only web turn was blocked before send by OLD send_unconfirmed
state. Existing send authorization remains true; do not reset credentials or
rewrite historical uncertainty as success. New full-suite/native checks follow.

Latest continuation: 2026-11-16. Start with
`docs/audits/orca-beta-acceptance-status-20261116.md` and the runtime audit.
R4 completed CSV-b and cross-b with independently verified outputs and all
receipts acknowledged by a subsequent webpage response. R5 code-d failed on
unnecessary same-chat navigation, before any write. R6 is an internal candidate
for exact-document reuse, bounded pre-dispatch navigation recovery, and browser
child approval-aware time accounting. Do not treat R5 as passing or reset its
uncertain conversation. Subsequent R7 root and desktop full race suites pass;
the earlier native checkptr failures are fixed. Ordinary Windows test launch
failures remain separately recorded.

Read `docs/audits/orca-beta-web-runtime-20261115.md` before the chronological
entries below. Source now permits 30 active minutes and 35 generations, removes
double rate-limit waiting, and tracks result delivery separately from host
acknowledgement. Shared reasoning rendering passes 16 browser UI cases. R3 code-c
verified artifacts and all eight result deliveries, but final-format correction
hit the task-wide repair cap. Source now permits three separate repairs, each
reply at most once; R4 verified two separate corrections in cross-b. Do not reset
the existing login/send consent or treat historical checkpoints as confirmed
web delivery. Historical sections below describe their original snapshots.

## November 15 later: automatic receipts and two more artifacts

CSV-a completed with a final answer and independently correct output. Cross-a
and code-b outputs pass independent verification, but both original turns stop
at five minutes before a final answer. Code-b additionally passes 500 randomized
cases and input-mutation checks. Preserve incomplete-turn outcomes.

IMPORTANT: ledger `returned` means the host adapter acknowledged a tool result
BEFORE webpage send, not that the website received or processed it. Last-result
delivery remains unconfirmed for code-b; do not infer it from this field. Current
conversation is archived with no inflight. See the follow-up audit for evidence.

The user personally enabled full access in the synthetic test conversation.
Existing web login/send consent remains unchanged. ContextPanel now displays
unknown context as unavailable instead of zero; frontend full tests/build pass,
but the running R2 executable is unchanged and new native rendering is pending.

## November 15 code-a approval follow-up

Read `docs/audits/orca-beta-code-a-followup-20261115.md` first. The real Agent
finished code, tests and RESULT.md after approval; independent tests and 500
randomized cases passed. Its original turn hit the ten-generation limit before
the final answer. A new read-only turn reused the same V3 chat successfully,
without replaying writes. Do not turn this into a clean first-attempt pass.

Budget diagnostics now identify the exact quota and reject invalid/overflowing
counters. This source change is NOT in the running R2 executable or the earlier
reviewed-20261115-r2 ZIP. Web race tests pass; latest full-root rerun has three
Windows test-launch failures, so it is not all green. CSV follow-up awaits a
Node tool approval. Existing website consent remains intact.

## November 15 final continuation: candidate and native verification

Start with `docs/audits/orca-beta-integration-r2-20261115.md`. A fresh internal
candidate now passes root/desktop full tests, relevant races, frontend build,
and all 16 native UI fixture combinations (164 clicks, 112 screenshots).
Non-Windows core/host-package builds pass; full native desktop builds do not
yet have evidence. Historical security/path failures below are retained, not
the latest test outcome. The latest candidate remains internal-only.

The real code-a Web Agent has read five synthetic project inputs and awaits
file-write approval. Do not replay it, reset website consent, or count it as a
successful task. Current production read-only counts are two forwarding and
30 reconcile reservations; neither state establishes termination or refund.
No production rollout, live account login/settlement or installer acceptance.
The new source/evidence handoff preserves the earlier dist package.

## November 15 later: account capabilities and Windows retry

Read `docs/audits/orca-beta-account-reasoning-20261115.md` for the newest client
changes and test evidence. Current server reasoning metadata now controls the
menu and actual account request; explicit choices and Auto are distinct, and
unsupported stale choices fail before credentials/inference. Root and desktop
full tests passed after the user reported temporarily disabling antivirus.
The earlier failures remain recorded; no independent false-positive verdict.
No production deployment, paid call, release or installer acceptance occurred.

## November 15 evening: current server and Windows security hold

Read `docs/audits/orca-beta-server-20261115.md` before the earlier report.
The current request-prices production source was recovered and reproduced
byte-for-byte. The desktop account increment is now in the isolated directory
`D:/AI-Reasonix/.tmp/orca-beta-server-requestprices-20261115/backend`.
Fresh PG/Redis acceptance and actual server/client synthetic login, inference,
cancel receipts, upgrade and rollback passed. Production was not changed.

The later Windows desktop rerun is NOT green: antivirus deleted its temporary
test executable at 18:07, and browserhost path-access checks also failed under
the restricted token. Windows race testing was stopped. Do not disable antivirus,
restore the sample or claim a false positive without further evidence. Earlier
passing suites below describe their original snapshot, not this later run.
Client DTOs now retain server request-tier prices and reasoning metadata; this
does not certify finished UI or all reasoning/protocol support. The prior dist
source package was preserved and has not been regenerated for these additions.

## November 15: module consolidation and current gates

Read `docs/audits/orca-beta-integration-20261115.md` first. It separates source
implementation, offline tests, native clicks, historical live model results,
and remaining external acceptance. This checkout is `codex/orca-beta` at shared
base `d23890928acf8bf6aede558959670027445829ac` plus reviewed uncommitted changes.

New changes exclude actual approval/Ask waits from the web task's active time,
persist tool outcomes for accurate transcript restoration, fail clearly when
the current server lacks the desktop login route, and build controllers from
one config snapshot. The account login probe is unauthenticated; a 401 proves
only route availability, not successful login or settlement. Never replace
current server code with the historical account30 candidate.

Final root/desktop Go suites and frontend tests pass. Relevant Windows races,
16 native Wails UI combinations, 16 headless UI combinations and the native
embedded-browser/onboarding smoke also pass. Native UI used synthetic account
and web transports, not real ORCA login. No new live model or paid call was made
on November 15. Older November 13 model results must keep their original failures.

Delivery is source/evidence only until current server login/settlement, the
remaining live task matrix, unknown web fragment/handoff recovery and actual
installer coexistence gates pass. `-PrepareCandidate` remains internal-only;
do not pass `-IntegrationGatesPassed` on the strength of automatic tests.
The authorized DeepSeek profile has not been reset; do not ask the user to
re-authorize simply to test a newly built executable. Current state schema is 5.

## November 13: V3 real-task follow-up

See `docs/audits/orca-beta-v3-live-20261113.md` for the current live synthetic
task matrix, explicit failures and release blockers. Current source has a
strict, read-only native-envelope compatibility path for DeepSeek Web plus
specific one-time correction for incorrect V3 raw-argument fields. It is not
a generic DSML writer. The two `z_live_*_v3_windows_test.go` files in
`desktop/webbetahost/` are temporary host fixtures with isolated local paths;
exclude them from source packages. The latest native UI test executable is the
internal R26 diagnostic build under `.tmp/beta2-v3-diagnostic-20261113-r26/run/`.
It is not an installable/deliverable beta. R23 is the last fresh full frontend
candidate; R24/R26 reuse its byte-verified frontend after proving the frontend
sources unchanged. Do not use older R9/R10 candidates as a proxy for this source
or re-request website consent on every run.

Native main-UI execution is now verified for one code-repair scenario (including
an explicit continuation after approval timeout), CSV calculation, cross-file
report writing, exact-byte text replacement, pure Q&A, and cancellation between
tools followed by a new question in the same conversation. Independent files,
not the model's completion claims, establish results. This is NOT three clean
repetitions per category or cancellation during every transport phase.

R26 fixes error precedence: a body in `raw_argument` is classified before raw
marker validation, so the existing one-time targeted correction can run even
when no raw block was emitted. The original and corrected protocol error codes
are now separate, content-free diagnostics. Exact reproducer and Agent-loop
tests pass; the subsequent live report continuation emitted valid JSON directly,
so it does not alone prove that the new correction branch was exercised live.

Root and desktop full Go suites passed with isolated HOME/APPDATA and a temp
directory outside the repository. Desktop needs more than three minutes for
the whole package (197.858s in the successful run); the successful bound was
600s. Raw browser regression and diff checks passed. Earlier sandbox/path and
three-minute timeout failures remain recorded in the audit. R23's unknown raw
fragment subtype, old uncertain-conversation handoff, restored error badges,
approval-time accounting, live account settlement and complete release gates
remain open. No installer/source delivery ZIP or public release is approved.

## November 12: persistent consent and temporary protection

The current source separates a saved sending grant from effective availability.
Temporary restrictions and uncertain sends still disable sending. An explicit
successful recheck may restore only the prior grant; it never resends an old
request, refills reservations, or restores an uncertain conversation. Explicit
disable, identity reset and detected signed-out/changed identity revoke consent.
State schema 4 is intentionally rejected by older candidates. Legacy enabled
state proves prior opt-in; disabled legacy state is not guessed or auto-enabled.
Do not reopen a schema-4 profile with R9 or older binaries.

The settings UI reads the authoritative post-recovery status and rejects stale
polls instead of fabricating an enabled/disabled state. Its layout is unchanged.
Modern/Classic and Chinese/English headless recovery clicks passed 76 assertions
with eight screenshots. This is synthetic bridge testing, not website acceptance.
Backend recovery/persistence regression passed before the final identity cases;
final-source full regression remains required. Evidence is kept in the worktree's
`.tmp/beta2-integration/` directory, outside distributable source selection.

The one R9 real cancel attempt on November 12 stopped at `site_ready`: native
WebView2 processes failed before any provider request or task scope. Both native
resources were released. No model message, security recovery or consent toggle
was automated. A separate elevated localhost-only native Wails smoke passed;
this supports an execution-environment issue but is not a live-site fix.
The failed run remains failed and its lease is retained pending ownership review.
No deployment, publication, paid request or stable-data change occurred.

Coordination now stays in the main chat; the Web module collaborator handed over
once and froze its files. Only a module handoff or final review needs resync.

## Working source after R9

R9 is an internal beta.2 candidate, not delivery acceptance. Later source adds
one narrow MCP boundary: local stdio stderr diagnostics remain available for
local startup/transport diagnosis but are not automatically appended to a
model-facing tool transport failure. Normal tool results, IsError/RPC-error
content and cancellation cause identity are preserved. This is not a general
secret scrubber. Real stdio RED/GREEN and three full plugin repetitions passed.

Auto-plan's internal request now disables thinking and cancels its own request
context on completion, without cancelling the parent or changing the user's
model effort. Ten focused repetitions passed. Provider registry TEST cleanup
now supports repeated runs; production duplicate-registration rejection is
unchanged. These post-R9 edits require a new source freeze and candidate.

R9 native browser smoke passed with its own hidden Wails window and localhost
Chinese form: native input, submit, resize and hide. It is tester-driven host
acceptance, not AI decision-making or production-UI acceptance. R9 PNG/TXT
acceptance helpers passed Build/offline permission checks but no upload ran.
The R9 real-site gate remains pending HUMAN recovery in the settings window;
no automated security toggle or new live send followed the failed R8 run.

The aligned historical server candidate additionally passed 75 top-level
isolated protocol tests (223 pass events, zero skips/fails), using fresh real
PostgreSQL and miniredis EMULATION. Client authorization/refresh/revoke/switch,
group isolation, own-ledger queries and settlement were exercised with synthetic
data. Actual Redis, running-service provenance, real browser login and real
upstream accounting remain unverified; no deployment or real charge occurred.

## Latest checkpoint: November 8 cancellation follow-up

This checkpoint supersedes the older candidate-status paragraphs below. R8 was
built, but its live cancellation/recovery case FAILED. The cancelled task
dispatched no tools and committed no answer; the follow-up failed with no-replay
protection. The exact initial website failure is unknown. The isolated Web
settings are now protected/disabled. No automated security recovery, profile
inspection or further live send followed. Do not reopen that schema-3 profile
in R7 or older binaries.

The working source, newer than R8, fixes late Message/Usage cancellation and
retains explicit unexecuted tool receipts. Web task release joins associated
producers, including separately bound auxiliary summaries. Controller idle is
held until its own task drains, without blocking unrelated API tasks. Final
source regression and real acceptance of a new immutable candidate are required.

An experimental active-XHR-abort was REJECTED: independent review proved a
cached-original-open reuse could abort the object's later upload. Production
retains ordinary site Stop and real HTTP termination proof; lifecycle events
invalidate stale identity only, never authorize abort or establish completion.

The earlier 17:26 full Go run passed 59 root packages and desktop, with 38/8
explicit skips; it does not certify later edits or native/race acceptance.
The separate server candidate passed 38 fresh PostgreSQL cases plus two helpers
with zero skips. Redis/OAuth, current production provenance and live settlement
remain separate gates. Paid requests in this follow-up: zero. No deployment,
publication, final installer delivery or stable-update-source change occurred.

Account authority update: the website maintainer reports Sub/Whalestack is now
the sole identity/CNY-ledger authority; the old Mac/Lens candidate is not current
production provenance. Public identity discovery lists confidential clients,
not a verified desktop public-client/refresh contract. Keep the historical
account30 PG results separate, do not deploy it over current services, and do
not embed web-client secrets. Current deployment/source and desktop interfaces
must be confirmed with the migration maintainer before live account acceptance.

Current follow-up: r7 was freshly built and its reviewed source snapshot is
`85dcdd112b6b41cda38c2a11ad3b1742` (1,529 files). Its executable SHA-256 is
`78f45c0984bdbda5567bcef9dee42d78c154095c211251113acaa944e6f406e9`.
R7-specific live serial-tool acceptance passed: lookup/add/count executed once
in model-call order, independently checked against receipts and final answer;
owned PID 69080 exited with Engine/browser release and output EOF confirmed.
This is not a cancellation pass.
The working source additionally contains the reviewed r8 confirmed-stop change.
Main independently passed its webbeta suite three times and 29 local raw/page
scenarios. The host receipt is revalidated at consumption, including generation,
active request, URL, block state and identity. R8 native/live acceptance is pending.

R8 writes Web state schema 3, accepting legacy schemas 1/2 on input. R7 and older
schema-2 binaries intentionally reject that state instead of ignoring the new
cancellation replay ledger. After R8 use, do not reopen the same profile in R7,
edit its version field, or delete cancellation records to force a downgrade.
Formal ORCA data is unaffected. Neither r7 nor r8 is a delivery installer.

November 8 follow-up: the working source now contains task-local sequential web
tool dispatch and an upload-observer XHR reuse fix, neither present in r6.
Targeted serial/cancellation/API-child isolation checks passed ten repetitions;
ten offline attachment scenarios passed independent review. A new r7 snapshot
and native cancellation acceptance are being prepared; do not reuse r6 evidence
as acceptance of those new changes. Account execution/settlement remediation is
being developed separately in the isolated server candidate, not deployed.

## 接手须知

本目录是 ORCA beta 的独立整合源码，不是正式版安装目录。正式版与
beta 不共用配置、会话、草稿、登录凭据和浏览器资料，也不自动导入旧数据。
共享核心默认仍可构建正式渠道；构建 beta 必须使用下方专用脚本及渠道标识，
不能直接运行普通 `wails build` 后把输出当作 beta。

当前正在进行第二轮 beta.2 整合。beta.1 的自动验收记录保留，不能代替
beta.2 的真实登录、模型和计费验收。用户已完成 DeepSeek 独立登录和发送授权；
原生普通问答及一次重启通过，旧工具题暴露了 Markdown 提取保真问题。
11 月 8 日 r6 原文通道已通过五道受限的真实 Agent 工具题，但完整网页
Agent 验收矩阵仍未完成。ORCA 账号真实登录与扣费仍未验收。密码和验证码
仍由用户本人输入，真实流程未通过前不宣称完成、不发布客户端。
本轮新增结论见 `docs/audits/orca-beta-web-fidelity-20261107.md`，尤其不能将
旧 r5 候选当作包含后续源码修复的新包。
11 月 8 日已定位观察器的发送文本绑定差异，并取得当前请求的 SSE 结构；
生产原文提取代码、离线回归和受限原生工具循环已通过。实际范围是生产
WebView2 宿主、Engine、Adapter 和 Agent，使用测试壳及三个纯内存工具，
不是 stock 主界面的文件、编程、浏览器或付费账号验收。
各次失败与进程释放证据均保留，
没有自动重放工具或改授权。当前续测见
`docs/audits/orca-beta-web-diagnostic-20261108.md`。
第二轮整合记录见 `docs/audits/orca-beta-second-round-20261106.md`；第一轮历史记录见
`docs/audits/orca-beta-automatic-20261106.md`。

The second round targets beta.2. Human credentials remain user-entered. Synthetic
tests do not certify authenticated website tasks, real settlement, or deployment.

Latest bounded acceptance (2026-11-08): five authenticated DeepSeek Web tasks
passed through the production host/Engine/Adapter/Agent, with only three memory
tools registered. Each tool executed exactly once per task; independent final
answer and forwarded-receipt checks passed. Three short-objective repetitions,
one step-oriented case and one format-example case passed. This is not full
stock-UI acceptance, a general tool-initiative benchmark, or a billing result.

The internal r6 candidate is
`.tmp/beta2-candidate-20261108-r6/OrcaBeta.exe`, SHA-256
`ab471a5a1d1348e7a5b80a366a76419a68d20c3c31653a932891fddee6fc2f5e`.
Its immutable build snapshot remains frozen; later audit-document edits are not
inside that executable's source snapshot. Status remains
`awaiting-real-acceptance`; do not relabel it as a finished installer or ship it.

## Source of truth

- Integration source: `D:\AI-Reasonix\.tmp\orca-beta-worktree`
- Branch: `codex/orca-beta`
- Core baseline: `d23890928acf8bf6aede558959670027445829ac` (desktop 3.0.13).
- Intended package version: `3.0.14-beta.2`, display name `ORCA beta`.
- Browser source: selected uncommitted browser-preview files from the existing
  V3 worktree, copied and hash checked without modifying that worktree.
- DeepSeek web source: reviewed `3.0.13-web-beta.4` sidecar. Its historical trial
  results do not establish acceptance of this integrated version.
- Historical server candidate: `D:\AI-Reasonix\.tmp\orca-beta-server\backend`.
  It is based on groups28 and must NOT replace the observed production account30
  binary. The second-round preflight found missing newer output-budget and
  account-billing semantics. The separate account30-based integration at
  `D:\AI-Reasonix\.tmp\orca-beta-server-account30` passed isolated PostgreSQL,
  Redis and real-client/synthetic-upstream checks. It is NOT deployed: an
  inherited unresolved-usage cancellation path can retain both a financial
  reservation and an execution slot indefinitely. No refund/charge policy was
  invented. See that tree's `followup-20261106/CANCEL-SCOPE-HANDOFF.md`; real
  paid-account acceptance remains blocked. Recheck live provenance before any
  rollout; the old groups28 candidate is not a substitute.

## Boundaries

- Independent beta configuration, sessions, drafts, credentials and browser data.
  Never import, inspect, migrate, or share the user's official chat data.
- No client publication, stable update switch, model-service restart, or legacy
  desktop-input enablement. Server changes require isolated acceptance first.
- Test only synthetic conversations/files. Second-round paid tests are capped
  at 80 requests and CNY 5, including auxiliary and subagent calls. Unsettled
  paid requests stop further paid testing. DeepSeek Web has no cumulative
  request or monetary budget; its per-parent-task safety limits remain ten
  generations, thirty tool actions, five minutes and one format correction.
  Captchas, restrictions and unknown sends stop the scenario, not trigger
  account rotation or paid fallback. Other sources never depend on web login.
- Secrets, cookies and real conversations are never delivery artifacts.
- Server accounting is authoritative. Missing usage or receipts are unknown,
  never a zero-cost claim. Restricted gifts are not unrestricted cash.

## Frontend contract

Preserve both existing Modern and Classic layouts. New account and connection
controls belong in existing chrome/menu slots. DeepSeek login is a secondary
native dialog; the first panel is status and explicit actions only. Native
webviews must hide behind approvals and modal overlays. No frontend access to
OAuth secrets, API keys, cookies or arbitrary CDP is allowed.

## Building the beta, not the shared-core default

Do not run an unqualified `wails build` and call its output the beta. The shared
core's default channel and packaging metadata intentionally remain unchanged.
The beta requires `main.channel=beta`, version `3.0.14-beta.2`, `OrcaBeta.exe`,
the beta Windows resources, and the beta installer identity together.

`scripts/build-orca-beta.ps1` stages a separate source snapshot and applies those
identities only there. Without `-Package` it performs a read-only preflight.
Actual packaging additionally requires `-IntegrationGatesPassed`, which is an
assertion of completed acceptance, not a switch to bypass a failing test.

For real-login acceptance before that gate, `-PrepareCandidate` builds a fresh
snapshot into an explicitly named directory under this worktree's `.tmp` only.
It writes a runnable directory and `candidate.json` marked
`awaiting-real-acceptance`, not an installer or delivery ZIP. It rejects both
`-Package` and `-IntegrationGatesPassed`. Preserve its isolated test profile;
never treat the candidate as accepted or copy it over an installed version.

Prerequisites: Go compatible with go.mod, Node 24 (CI-supported Node 22 is also
used by the core), Wails 2.12.0, PowerShell 7, NSIS and 7-Zip. Build the frontend
and regenerate Wails bindings after backend API changes. Do not reuse an old
frontend/dist or an executable from the original V3 checkout.

The source ZIP deliberately contains no `.git`, dependencies or compiled
frontend. Verify its `manifest.json` first. The packaging script requires a Git
worktree rooted at the source directory, so an extracted ZIP must first become
a NEW local repository with a baseline commit of only the manifest-listed
source files. Do not stage an enclosing workspace or add local credentials,
generated files or profiles. That new commit is your local import baseline,
not the original release commit. The shared checkout's own manifest records
the original core commit and exact uncommitted integration bytes separately.
Direct frontend builds and Go tests do not require Git history; beta packaging
still requires the explicit beta identities and reviewed source selection.

Pinned installer payloads are supplied explicitly with `-PayloadDirectory` and
`-CodeGraphArchive`. The script verifies the Node binary and the CodeGraph
archive/member hashes. They are build dependencies, not personal data, and
should not be copied from an arbitrary installed application's directory.

The existing official updater is disabled for this channel. The installer must
not terminate official ORCA processes, claim its protocols, replace its files,
or remove either application's user data. Never point beta's data override at
an official profile to make old conversations appear.

## Synchronizing shared core

Keep common fixes in the existing internal/desktop modules; experimental
account/browser/web modules are enabled by channel and platform. Do not copy a
whole worktree over another one. Review upstream commits, apply them to the
beta integration branch, and rerun both default-channel and beta isolation
tests. Record the resulting core commit and beta commit in the final artifact
manifest. The current integration is uncommitted, so no final beta commit or
source-to-binary certificate exists yet.

## Development and acceptance

See `DEVLOG.md` for this integration's changes and actual commands/results.
Only a final verification report with explicit pass/fail/not-run entries may
authorize packaging as a usable local beta. A built executable is not proof of
native WebView2, authentication, accounting or installation acceptance.

Current evidence locations (not personal profiles):

- `docs/audits/beta-ui-native-qa.md`: synthetic frontend matrix and separate
  real Wails modal/click acceptance, including failed attempts.
- `D:\AI-Reasonix\.tmp\orca-beta-root-test-consolidation-20261106.md`:
  earlier root suite and fixture diagnostics; superseded follow-up results are
  in `D:\AI-Reasonix\.tmp\orca-beta-support-followup-20261106.md`.
- Server candidate `dev-docs/desktop-acceptance.md`: fresh PostgreSQL/Redis
  harness with no production mounts, accounts, money or upstream models.
- `docs/audits/orca-beta-automatic-20261106.md`: current integration test gates,
  automatic versus manual evidence, and remaining delivery restrictions.

Do not enable the account UI against an unpatched server, or label an archive
as accepted merely because it compiles. Human DeepSeek login was performed for
the isolated integration, but live ORCA account login and settlement are not
accepted. Existing sidecar acceptance is not evidence for this integration.
