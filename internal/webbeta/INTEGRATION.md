# Web Beta Backend Integration

Source: `D:/AI-Reasonix-sidecar/orca-web-beta/O.R.C.A-3.0.13-source`, current
`WEB-BETA-TRIAL.md` (background fix). Only individually reviewed backend files
were imported. No personal state, browser profile, frontend or live acceptance
runner is imported.

## Wiring and APIs

- Kuhn supplies `betaEnabled() bool` and `betaDataRoot() (string, error)` before config
  reads. Web Beta stores only its own files beneath `<beta root>/deepseek-web`.
  Do not call the sidecar's environment-redirection/workspace restriction code.
- Main's existing startup/shutdown hooks call `startWebBetaBackground(a)` and
  `closeWebBeta()`. These peer-owned hooks are preserved unchanged.
- SubmitToTab and SubmitDisplayToTab now call
  `a.beginWebBetaAdmission(tabID)` and defer its release through submission or
  queueing. The old `checkWebBetaAdmission` helper remains compatible. Switching to
  this provider may call `a.prepareWebBetaBackground()` to recreate a failed
  controller, but must not retry a draft automatically.
- Retained frontend APIs: WebBetaOpen() (Status,error), WebBetaStatus() Status,
  WebBetaConfigure(Settings) error, WebBetaRecover() error,
  WebBetaBounds(webbetahost.Bounds) error, WebBetaLogout() error.
- Main now calls `ensureWebBetaModelEntry()` immediately after initializeBeta.
  Registration failure aborts startup. Model name is
  `DeepSeek官方（网页 Beta）`, provider name `deepseek-web-beta`, kind
  `deepseekweb-beta`; display as DeepSeek_Web (web experimental). Show the model
  even while disabled, do not change the current selection or API allow-list.
- ProviderEntry.Configured now exempts kind `deepseekweb-beta` without any
  ORCA_WEB_BETA_READY environment condition. Stream performs engine-local,
  cancellable readiness checks. No API key or token is extracted from the page.
- Shared protocol and propagation are now integrated under the expanded owner
  scope: Request.ConversationID/ContextEpoch, Message.Files and FileContent
  metadata with Data excluded from JSON. Agent/planner requests carry stable
  identity; controller rebuild keeps it, forks change it, compaction advances
  the epoch. Input, provider and history file snapshots are deep copied.
- Controller now resolves explicit web file/image refs through the existing
  pinned-directory/TOCTOU reader and read_file + file_send permission gates,
  including relative-path deny rules. File-only input goes through RunRich;
  missing snapshots are not silently downgraded to strings. Guidance uses the
  same authorization path. Classifier/compaction bindings are isolated even
  when a caller accidentally supplies the main conversation ID.
- Root/desktop module files now add only jsonschema/v5 v5.3.1 and its sums.
- Logout invalidates old conversation owners. Starting another identity must
  use a new session ID; never silently feed an old account's transcript to it.
  It now retires and drains the corresponding controllers itself, including
  accepted detached tabs and queued runtime replacements. Old controllers stay
  retired after login; UI must start a new conversation, not replay the old draft.
- Added control.Controller.Retire(), Retired(), WaitIdle(context.Context), and
  ErrControllerRetired. Retirement is permanent. WaitIdle observes actual
  foreground execution and session background-job completion, not CancelAck.
- Added jobs.Manager.Stop() and WaitStopped(context.Context); a stopped manager
  never executes newly submitted jobs. Killed status alone is not completion.
- Added Engine.StopForIdentityChange() and CloseContext(context.Context).
  Close remains compatible and waits indefinitely; bounded logout uses the new
  methods without pretending that timed-out operations have stopped.
- Runtime config read/modify/write now uses App.configWriteMu, shared with the
  account catalog writer. Startup registration is before worker startup. Neither
  registration nor enablement changes ProviderAccess, including a web-only list.
- Vision metadata is adapter-declared support, explicitly not live-verified in
  this build, with no verification timestamp/probe evidence. Manual unsupported
  override and desktop vision-off remain effective. This writer uses the isolated
  config directory explicitly; Kuhn's shared default-path isolation is also now
  present and uses config.ProfileConfigDir/ProfileHomeDir.

## Implemented Safety Changes

- Close permanently rejects old Engine/Vault pointers, cancels and drains
  queued/running operations including adapter post-processing. Late events and
  unread streams cannot block reset or yield tool/completion results afterward.
- Logout first closes admission, retires/cancels relevant controllers and their
  background jobs, invalidates queued submits and attachment grants, and journals
  identity_drain_pending with old-owner tombstones (including unbound requests).
  Queue/controller/engine drain has a shared 10-second budget. A timed-out tool
  stays genuinely running, profile/generation stay intact, and recovery/open/
  configure cannot bypass the failure. Retry Logout after actual execution exits.
- Only after actual drain does reset durably journal a new generation, release
  the native host (separate 15-second bound), and clear its owned profile. Failure
  requires an explicit logout retry in the same process. On restart a fully
  journaled reset may finish cleanup; a drain-pending journal cannot recover or
  enable. Neither path enables sending, restores bindings or replays work.
- Native release closes the own controller, releases retained COM references,
  and waits only on its browser-process handle. Timeout forbids profile cleanup
  and recreation. No global process matching/termination, cookie-only logout,
  generic-browser cleanup or ORCA-account clearing is used.
- Owned profiles require a marker and fixed `<beta root>/deepseek-web/profile`
  child path; symlinks/junctions/reparse points and unowned directories fail
  closed. The state/journal and unrelated sibling stores survive profile reset.
- Observed logout of an established website identity latches identity_changed;
  later page login/recheck cannot revive old URLs. Use explicit WebBetaLogout
  before switching identities. Unobservable account changes made externally
  while the app is stopped are not claimed detected.
- Unknown completions pause the whole queue before releasing its slot. Tool
  parsing rejects duplicate JSON keys as well as unknown names/schema errors.
  The optional reserve pool defaults paused. Startup still does not refill.
- Windows transient state-replace failures retry only the same atomic rename
  (bounded to 630ms), preserving the old checkpoint; no model request retry.

## Validation (2026-11-06)

Run from the target worktree, with process-local GOCACHE set to
`D:/AI-Reasonix/.tmp/orca-beta-gocache`, GOPROXY=off, and TEMP/TMP set to
`D:/AI-Reasonix/.tmp/orca-beta-test-temp` (system temp ancestors are restricted
by the sandbox's OpenRoot checks).

- `go test -mod=readonly ./internal/webbeta -count=20 -timeout=90s`: passed.
- `go test -mod=readonly ./internal/agent ./internal/control ./internal/provider
  -count=1 -timeout=180s`: passed, including the new snapshot propagation tests.
- From desktop: `go test -mod=readonly ./webbetahost . -run
  'Test(WebBeta|OwnedProfile|Profile|IdleLogout|RestoredIdentity)' -count=10
  -timeout=120s`: passed (native browser mocked, no network).
- Updated full package run: `go test -mod=readonly ./internal/webbeta
  ./internal/control ./internal/config ./internal/jobs ./internal/agent
  ./internal/provider -count=1 -timeout=180s`: passed.
- Drain/retirement/config regressions across webbeta/control/config/jobs:
  `-run 'Test(Identity|Retire|Retired|StopWaits|WebBeta)' -count=20`: passed.
- CLI compilation: `go test -mod=readonly ./cmd/orca ./cmd/deepseek-orca
  -run '^$' -timeout=90s`: passed. There is no Go package at repository root.
- Desktop repeated coverage now includes real Agent non-cooperative tools,
  detached/rebuilt controllers, queue admission, unrelated API survival, metadata
  declarations/manual disable, explicit allow-lists, and shared config locking.
  The final full selected desktop/webbetahost run passed ten repetitions after
  temporary parent account-test compile errors were resolved by the parent.
- The actual OS symlink fixture skips for missing Windows symlink privilege;
  reparse-attribute rejection has a separate synthetic unit test.
- CGO_ENABLED=0; neither gcc nor clang is available, so race tests are not run.
- A later full Control run intermittently failed while its existing Windows
  junction-swap fixture renamed a temporary directory (Access denied). An
  immediate unmodified full-package rerun passed. This fixture/environment
  flake is not treated as clean repeated acceptance evidence.
- One full Agent run also hit Windows Access denied in the existing concurrent
  timeline-save rename fixture; immediate unmodified Agent rerun and the final
  full backend run both passed. No claim of a diagnosed OS cause is made.
- Source dependency overlay checks were used during integration only; final
  successful tests above use the target worktree directly. Temporary overlay
  and named-file harness were removed. No commits/deploy or live model requests.

### Fresh-Cookie Native Check

On 2026-11-06 the parent reports `TestWebBetaNativeFreshLoginOnly` PASS in
**9.17 s**. This documentation update verified the retained structural evidence:

[login-only-result.json](D:/AI-Reasonix/.tmp/orca-beta-worktree/.tmp/webbeta-fresh-login-cc88bfedfbe94ec5bc0f556acb2c4988/login-only-result.json)

The report records the official HTTPS DeepSeek origin and `OrcaBetaWindow`,
login controls present, hidden presentation, Run refused, no installed adapter,
no probes/uploads, and the fresh owned profile released and cleared. The harness
uses `NewLoginOnlyWithWindowClass` and no App/agent/account bindings. This is
native fresh-cookie login-page reachability and fresh-profile release evidence,
not authentication, sending, authenticated logout or identity-switch acceptance.
Guard registration succeeded during initialization; live adversarial navigation
and popup behavior were not exercised by this successful login-page run.

The 9.17 s is parent-reported total test duration, not a measurement in the JSON
or a cold-start/readiness benchmark. No authentication or model send occurred.
Human login is explicitly reserved for the next round; the current gate remains
automation-only. Parent's separate isolated host-adversarial check is pending.
This worker makes no further native calls now. Repeating the native command or
performing authentication/sends requires separate authorization and coordination.

### Controller Build Follow-Up

Boot now reuses its enabled per-build skill catalog for dedicated subagent tool
registration through `BuiltinSubagentToolsFromCatalog`; execution/profile lookup
still re-read the store and enforce the existing override, disabled-name, parsing
and path guards. No global cache or config callback was added.

In the unchanged synthetic 50-skill experiment, warm App.buildController p50
fell from **1157/1223 ms to 331/312 ms** across two before/two after runs, roughly
71-74% lower comparing corresponding runs. This is sequential synthetic backend
evidence on a shared machine, not real Wails startup or DeepSeek response latency.
Full stage results, exact follow-up files/APIs, regressions and commands are in
[PERF-EXPERIMENT.md](D:/AI-Reasonix/.tmp/orca-beta-worktree/internal/webbeta/PERF-EXPERIMENT.md).

## Exact File Manifest

Paths relative to `D:/AI-Reasonix/.tmp/orca-beta-worktree`. Existing unrelated
parent/peer changes, including provider.Config.HTTPClient, are preserved.

```text
go.mod
go.sum
desktop/go.mod
desktop/go.sum
desktop/app.go
desktop/main.go
internal/provider/provider.go
internal/agent/agent.go
internal/agent/coordinator.go
internal/agent/session.go
internal/agent/session_timeline.go
internal/agent/file_snapshots.go
internal/agent/file_snapshots_test.go
internal/control/controller.go
internal/control/retire.go
internal/control/retire_test.go
internal/control/guidance.go
internal/control/web_beta_attachments.go
internal/control/web_beta_attachments_test.go
internal/config/config.go
internal/config/web_beta_test.go
internal/jobs/jobs.go
internal/jobs/stop_test.go
desktop/web_beta_admission.go
desktop/web_beta_app_other.go
desktop/web_beta_app_windows.go
desktop/web_beta_lifecycle_windows_test.go
desktop/web_beta_wiring_windows_test.go
desktop/webbetahost/acceptance_disabled.go
desktop/webbetahost/acceptance_enabled.go
desktop/webbetahost/browser_windows.go
desktop/webbetahost/release_windows.go
desktop/webbetahost/identity.go
desktop/webbetahost/identity_test.go
desktop/webbetahost/profile.go
desktop/webbetahost/profile_test.go
desktop/webbetahost/profile_link_other.go
desktop/webbetahost/profile_link_windows.go
desktop/webbetahost/profile_link_windows_test.go
desktop/webbetahost/page.js
desktop/webbetahost/site.js
desktop/webbetahost/probe.js
internal/webbeta/agent_integration_test.go
internal/webbeta/attachments.go
internal/webbeta/correction.go
internal/webbeta/correction_test.go
internal/webbeta/engine.go
internal/webbeta/engine_test.go
internal/webbeta/json.go
internal/webbeta/lifecycle.go
internal/webbeta/lifecycle_test.go
internal/webbeta/provider.go
internal/webbeta/provider_test.go
internal/webbeta/scheduler.go
internal/webbeta/scheduler_test.go
internal/webbeta/state.go
internal/webbeta/state_replace_other.go
internal/webbeta/state_replace_windows.go
internal/webbeta/state_replace_windows_test.go
internal/webbeta/types.go
internal/webbeta/INTEGRATION.md
```

## Acceptance Boundary

The bounded browser/WebBeta backend review, regression fixes, exact files/APIs,
and remaining peer-owned findings are recorded in [REVIEW-20261106.md](REVIEW-20261106.md).
The subsequently authorized generic-browser native adversarial/upload/download/
callback run passed with the beta path policy enabled; see
[NATIVE-HOST-REVALIDATION-20261106.md](NATIVE-HOST-REVALIDATION-20261106.md).

Backend regression tests use synthetic in-memory browsers and temporary profiles,
with no live model requests. The separately parent-run fresh-cookie native check
above now establishes only hidden login-page reachability, refusal of automated
requests, and actual release/cleanup of that fresh owned profile. It does not
establish authenticated readiness or logout, manual account switching, uploads,
vision/tools, native focus/clipping, full production UI, or real cold-start timing.
Those still require separately authorized acceptance. Human login is deferred to
the next round; no send permission follows from the login-page pass. The generic
host pass above does not establish full beta production-UI acceptance. Build/test success
must not be substituted for these remaining acceptance results.
No account provider/receipt code, frontend, tabs.go, local_ai_app.go, or
RequestPurposeBrowser changes are part of this backend work. The only app.go
edits are the two admission guards; the only main.go edit is startup registration.
