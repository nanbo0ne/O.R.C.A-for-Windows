# Bounded Synthetic Controller Experiment

Date: 2026-11-06. Target: `D:/AI-Reasonix/.tmp/orca-beta-worktree`.

## Scope and Files

The initial measurement-only follow-up added:

- `desktop/web_beta_controller_perf_test.go`
- `internal/webbeta/PERF-EXPERIMENT.md`

The subsequently approved optimization changes only skill tool registration in
`internal/skill/tools.go` and its boot call in `internal/boot/boot.go`, with new
`internal/skill/subagent_catalog_test.go` regressions. The existing
`internal/skill/skill.go` ListAll/Enabled change and catalog_snapshot_test.go are
retained unchanged. Kuhn confirmed no concurrent boot edits; his boot fixtures
were not touched. Config callback work remains deferred.

The approved login-only harness adds `desktop/web_beta_login_native_windows_test.go`,
`desktop/webbetahost/document_scripts.go`, `login_boundary_windows.go`, and
`login_only_windows_test.go`, and narrowly updates webbetahost/browser_windows.go.

No account/UI edit, native launch, browser/profile reuse, external tool execution,
model request, commit or deployment was performed by this implementation worker.
The parent subsequently ran the authorized fresh-cookie login-only native check;
its retained evidence and limited acceptance scope are recorded below.
The fake provider returns two in-memory chunks and never invokes HTTP.

## Method

The opt-in test uses `withBetaProfile`, then `initializeBeta`, a fresh synthetic
workspace with an empty `.git` marker, and a synthetic config. Two fixture sizes
are tested: no added skills, and 50 nested `.agents/skills/*/SKILL.md` files.
Built-in skills remain present in both cases. MCP, CodeGraph, LSP, auto-planning,
vision and optional host-tool groups are disabled. The only enabled ordinary
tool is `read_file`; the fake provider emits no tool calls, which is asserted.
PATH is empty and ProgramFiles candidate roots point inside the fixture, so
controller construction cannot launch the shell-discovery Bash probe.

Each completed invocation uses, per fixture size:

- Three fresh-profile samples each of standalone config load, direct boot.Build,
  and App.buildController. Fixture creation/config serialization are outside the
  timed interval. No controller has yet been built in those fresh profiles.
- Eight warm paired App.buildController/boot.Build samples, alternating their
  order after one unmeasured warmup. Each controller is closed before continuing.
- Eight samples each of two consecutive standalone LoadForRoot calls and one
  beta-enabled skill ListAll. Runtime-provider preparation is separately timed.
- Four backend workflows: build, real Controller.RunTurn with one fake response,
  App.NewSession on a project tab, real App.SetModelForTab from perf/a to perf/b,
  and controller Close. Exactly four fake Stream calls and no tools are asserted.

"Fresh/cold" is synthetic fresh-profile construction in a running Go test
process. Files were just written and OS caches were not flushed. It is NOT
process launch, cold OS cache, Wails/React startup, WebView2 cold start or login
readiness. Project NewSession is the project's existing session-rotation path;
it does not measure the global-tab asynchronous rebuild/UI callback. The fresh
controller stage separately measures construction needed for a new controller.
Shutdown is Controller.Close with no native browser, plugins or background jobs,
not App/Wails shutdown. No legacy first-boot migration was exercised because
isolated beta deliberately disables legacy import.

These are sequential wall-clock measurements on a shared development machine,
not CPU profiles. p50 uses the lower middle sample; small-sample p95 is effectively
the maximum. Values printed as 0.000 ms are below displayed timing resolution,
not proof of zero cost. Individual stage measurements are not additive tracing
inside one build; paired subtraction is only an estimate of wrapper overhead.

## Baseline Results

Two separate bounded test invocations passed in 50.847 s and 54.540 s package
time (test bodies 50.01 s and 54.26 s). The following are the two runs' p50s,
in milliseconds, shown as `run 1 / run 2`, not pooled percentiles:

| Stage | No added skills | 50 nested skills |
| --- | ---: | ---: |
| Fresh profile: config load | 13.675 / 15.084 | 13.430 / 16.374 |
| Fresh profile: direct boot.Build | 154.330 / 153.874 | 1110.023 / 1323.784 |
| Fresh profile: App.buildController | 149.012 / 142.148 | 1064.164 / 1280.237 |
| Warm App.buildController | 163.527 / 140.961 | 1157.360 / 1223.349 |
| Warm direct boot.Build | 144.269 / 124.756 | 1222.307 / 1330.669 |
| Paired desktop minus boot | 16.948 / 14.541 | -50.855 / -20.735 |
| Warm config load 1 | 12.515 / 11.370 | 13.091 / 14.029 |
| Warm config load 2 | 12.257 / 10.981 | 13.627 / 13.465 |
| Warm beta skill ListAll | 14.622 / 12.432 | 216.076 / 246.563 |
| New controller on warm profile | 145.536 / 150.354 | 1146.736 / 1187.869 |
| Build to first synthetic text | 146.053 / 150.869 | 1147.249 / 1188.384 |
| RunTurn to first synthetic text | 0.513 / 0.000 | 0.513 / 0.515 |
| Project new conversation | 19.801 / 22.081 | 20.707 / 21.153 |
| Project model change | 178.841 / 182.482 | 1176.384 / 1410.890 |
| Controller shutdown | <0.001 / <0.001 | <0.001 / <0.001 |

The second run's warm App.buildController p95 was 147.331 ms with no added
skills and 1719.751 ms with 50 skills. The second config-load p95 was 12.100 ms
and 24.376 ms respectively. Synthetic text after RunTurn had maximum 1.004 ms
and 0.575 ms. A single first-run workflow shutdown sample was 0.330 ms; the
rest rounded below 0.001 ms. Local-runtime-provider preparation also rounded
below 0.001 ms for these non-local models.

The initial, larger pilot reached its 90-second cap during the 50-skill warm
loop. It is excluded from the result table. Its stack was in
`BuiltinSubagentTools -> Store.Read -> List -> ListAll -> ProfileDiscoveryAllowed`.
The final fixed sample budget was reduced before both successful runs; no path
guard or safety check was disabled to improve timing.

## Interpretation and Proposal

The duplicate load is confirmed in code: desktop/local_ai_app.go buildController
loads first, then boot.Build performs migration and loads again. Model change
also loads configuration in App.SetModelForTab before those two calls.

A single 11-14 ms load is measurable and roughly 7-9% of the small warm build.
The 14.5-16.9 ms paired wrapper overhead supports a narrow improvement there.
With 50 beta-isolated nested skills, a config load is only about 1% of total
construction. Negative paired medians and wide ranges show that build noise
swamps this small difference: these data do NOT demonstrate an end-to-end
50-skill speedup from removing the load. Do not subtract the unrelated fresh
profile medians or advertise them as an A/B improvement.

The parent's reported 25-27 ms to 12-14 ms skill optimization measures the
standalone flat `.orca/skills/*.md` catalog fixture. This experiment instead
enables beta path guards and uses permitted nested `.agents` fixtures; the
numbers are not directly comparable. Baseline boot used one ListAll snapshot
for the main catalog, but BuiltinSubagentTools still called Store.Read four
times; each Read performs List/ListAll. Thus five scans remained in the baseline
coding profile's construction. The observed 216-247 ms single scan and 1.16-1.22 s
warm build are consistent with this being a larger cost, but this is an
inference from timings/code/one timeout stack, not an instrumented CPU profile.

Recommended minimal config solution, PROPOSAL ONLY:

1. Keep boot.Build as the owner of root resolution and
   MigrateLegacyIfNeeded -> LoadForRoot ordering. Do not pass today's earlier
   desktop-loaded config straight into boot; that can bypass first-boot import.
2. Add a specific optional per-build `boot.Options.PrepareRuntimeProviders`
   callback of type
   `func(context.Context, *config.Config, string) ([]config.ProviderEntry, error)`.
   Invoke it on that just-loaded config and effective requested/default model,
   before merging runtime providers and resolving the executor. Preserve the
   existing RuntimeProviders overlay order and migration notice/error behavior.
3. The desktop wrapper supplies a closure calling prepareLocalRuntimeProviders
   and captures the trusted-computer-use consent bit from the same config for
   SetTrustedComputerUseAccess after Build. Remove only its preliminary load.
4. Config remains private to that single Build call; no global cache, no retained
   cross-project config, and no generic caching abstraction. Model switching's
   separate validation load is left alone initially.
5. Before merging, test migration-before-callback on non-beta synthetic legacy
   data, nested/explicit root semantics, runtime-provider precedence, independent
   concurrent workspaces, changed config on the next build, and consent from the
   same loaded config. Repeat this experiment with identical fixtures afterward.

### Approved Skill Registration Change

Boot now passes its existing enabled catalog to
`BuiltinSubagentToolsFromCatalog(store, catalog, runner, profileResolver...)`.
The old `BuiltinSubagentTools` API remains compatible and obtains one List,
rather than four Read scans. The new helper checks names and the store's disabled
set without filesystem discovery; only presence is consumed from the snapshot.
Execution and profile resolution still call Store.Read and all original parsing,
override, inline/subagent and profile/path guards. No global or cross-build cache
was added; no path guard or configuration-load ordering changed.

Regressions cover project-over-global/builtin override, disabled names even when
given an unfiltered catalog, inline overrides, missing/deleted skills, changed
body/model, disabled-after-registration, invalid replacement content, cross-root
separation and enforcement of a current isolated-profile discovery boundary.
Deleting the fixture between discovery and registration also verifies that
registration consumes the supplied snapshot without a hidden rescan.

### After Results: Identical Experiment

The test/fixtures/sample counts were not edited. Two new Go process invocations
passed in 30.214 s / 22.709 s package time (test bodies 29.89 s / 21.82 s).
After p50 milliseconds, shown as run 1 / run 2:

| Stage | No added skills | 50 nested skills |
| --- | ---: | ---: |
| Fresh profile: config load | 18.699 / 16.299 | 17.200 / 13.017 |
| Fresh profile: direct boot.Build | 121.313 / 105.882 | 506.332 / 326.124 |
| Fresh profile: App.buildController | 108.665 / 112.257 | 449.861 / 288.949 |
| Warm App.buildController | 136.334 / 108.413 | 330.880 / 312.250 |
| Warm direct boot.Build | 118.023 / 94.430 | 309.373 / 301.500 |
| Paired desktop minus boot | 10.801 / 14.454 | 10.650 / 7.154 |
| Warm config load 1 | 16.155 / 14.701 | 13.420 / 12.476 |
| Warm config load 2 | 17.274 / 14.205 | 14.578 / 13.530 |
| Warm beta skill ListAll | 20.271 / 16.338 | 230.490 / 244.890 |
| New controller on warm profile | 153.398 / 100.052 | 678.392 / 272.122 |
| Build to first synthetic text | 154.471 / 100.052 | 679.480 / 272.636 |
| RunTurn to first synthetic text | 0.521 / 0.513 | 0.527 / 0.514 |
| Project new conversation | 27.370 / 22.014 | 42.066 / 19.845 |
| Project model change | 187.756 / 129.838 | 824.142 / 342.028 |
| Controller shutdown | <0.001 / <0.001 | <0.001 / <0.001 |

Warm 50-skill App.buildController fell from 1157/1223 ms to 331/312 ms, about
71-74% lower comparing corresponding runs. Its after p95s were 435/385 ms;
single skill scan cost remains 230/245 ms, consistent with eliminating four
additional scans rather than weakening discovery. This is sequential before/
after evidence on a shared machine, not a randomized isolated A/B trial.
The first after workflow timings were noisy; both runs are retained above.
Synthetic chunk delivery itself remains about 0.5 ms, and new-conversation/
shutdown do not show a demonstrated optimization. No real Wails startup,
DeepSeek latency or authenticated-readiness speedup is claimed.

## Reproduce the Offline Experiment

From `D:/AI-Reasonix/.tmp/orca-beta-worktree/desktop`:

```powershell
$env:GOCACHE = 'D:/AI-Reasonix/.tmp/orca-beta-gocache'
$env:GOPROXY = 'off'
$env:TEMP = 'D:/AI-Reasonix/.tmp/orca-beta-test-temp'
$env:TMP = $env:TEMP
$env:ORCA_BETA_SYNTHETIC_PERF = '1'
go test -mod=readonly . -run '^TestWebBetaSyntheticControllerPerf$' -count=1 -timeout=120s -v
```

The test skips when ORCA_BETA_SYNTHETIC_PERF is not 1; that skip was separately
verified. Default test runs therefore do not silently acquire this workload.

## Native Harness Review

Reviewed only the permitted prototype source, especially its current
WEB-BETA-TRIAL.md and desktop/web_beta_acceptance_windows.go. The actual tag is
`webbeta_acceptance` (with underscore), not `webbetaacceptance`.

- Prototype `background_readiness` branches immediately after WebBetaOpen. It
  checks hidden presentation, ready state, unchanged Enabled consent and unchanged
  Sent; it does not Configure, Refill or Send. This is the only existing prototype
  branch suitable for a strict no-send/no-consent-change readiness check.
- `readiness` sends no message but explicitly recovers and enables consent, then
  disables it. It is not a no-configuration-change test.
- `upload_probe` and `upload_probe_image` upload to the live service, even without
  sending a chat message. They are outside this authorization.
- `smoke`, `arithmetic`, `visible`, `agent`, `background`, `background2`, and
  `attachments` configure the engine and perform real DeepSeek sends. Do not run.
- A fresh signed-out profile cannot meet background_readiness's Ready assertion.
  Loading a new-cookie login page must be recorded as login-page reachability,
  not authenticated adapter readiness, tool acceptance or successful logout.
- The prototype runner/start hook is intentionally absent from this integrated
  worktree. Its tag currently enables host diagnostics only; setting its mode
  environment variable here does not create a native acceptance run.
- NewLoginOnlyWithWindowClass now provides the separate beta-class login-only
  path. NewWithWindowClass remains the regular adapter path. Login-only refuses
  all Run and Recheck before posting work, excludes all adapter/probe scripts even
  with acceptance diagnostics flags, and accepts only structural login health.
- Login-only installs native NavigationStarting and FrameNavigationStarting
  guards before Navigate: only HTTPS chat.deepseek.com, with no userinfo or port
  override. All new windows are handled/blocked. External SSO/challenge frame
  navigation is deliberately not allowed. Normal page subresources are not a
  network firewall; the website can fetch its ordinary CDN assets.

### Exact Existing No-Send Native Command

The existing integrated `TestBetaConnectionsInProductionUI` is a full native
Wails/production-React test with two real WebView2 surfaces, BUT its DeepSeek
browser factory is replaced by a loopback fixture. It forbids model sends and
external account HTTP. This is useful native UI evidence, not real-site adapter
acceptance. Compilation with the following tags and `-run '^$'` passed. No
native test was launched in this task.

AFTER parent authorization only, from the desktop directory:

```powershell
$env:GOCACHE = 'D:/AI-Reasonix/.tmp/orca-beta-gocache'
$env:GOPROXY = 'off'
$env:TEMP = 'D:/AI-Reasonix/.tmp/orca-beta-test-temp'
$env:TMP = $env:TEMP
$env:ORCA_BETA_SYNTHETIC_PERF = '0'
$env:ORCA_BETA_NATIVE_UI_TEST = '1'
$env:ORCA_BETA_NATIVE_CHILD = ''
$env:ORCA_BETA_NATIVE_CASE = 'modern-en-1024'
$env:ORCA_WEB_BETA_ACCEPTANCE = ''
$env:ORCA_WEB_BETA_PROBE = ''
$env:ORCA_BETA_NATIVE_ROOT = Join-Path (Join-Path (Split-Path (Get-Location).Path -Parent) '.tmp') ('webbeta-native-nosend-' + [guid]::NewGuid().ToString('N'))
if (Test-Path -LiteralPath $env:ORCA_BETA_NATIVE_ROOT) { throw 'Refusing to reuse native profile/evidence root' }
New-Item -ItemType Directory -Path (Split-Path $env:ORCA_BETA_NATIVE_ROOT -Parent) -Force | Out-Null
go test -mod=readonly -tags 'production,browserui,webbeta_acceptance' . -run '^TestBetaConnectionsInProductionUI$' -count=1 -timeout=150s -v
```

The test itself creates the previously nonexistent evidence root and its child
profiles; do not precreate the leaf, copy cookies, or point it at an existing
beta profile. The extra webbeta_acceptance tag does not turn its fake DeepSeek
factory into a live-site adapter.

### New Fresh-Cookie Real-Site Login-Only Harness

`TestWebBetaNativeFreshLoginOnly` is tagged `windows && production &&
webbeta_acceptance` and additionally requires ORCA_WEB_BETA_LOGIN_ONLY_TEST=1.
It creates a previously nonexistent evidence root under this worktree's .tmp,
uses the main initializeBeta/betaDataRoot helper, rejects an existing DeepSeek
profile, and clears inherited WEBVIEW2_* overrides. Its blank, hidden beta-class
Wails shell has no App bindings, engines, agents, account clients or production
React UI. It loads only the official DeepSeek origin with the login-only host.
It never enables consent/refills; no provider configuration is loaded or written.

Only a structural login-controls boolean is observed, never input values,
cookies, chats, uploads, screenshots or probes. Document-loaded alone is NOT a
pass. The host must remain unpresented/not Ready/not Busy, refuse Run, then
CloseAndWait within 15 s. Only its freshly created owned DeepSeek profile is
cleared after confirmed release; release failure retains it and fails. The final
report is written only on complete success. No authenticated login, logout,
identity-switch, tool, vision, account or production-UI acceptance is claimed.

**Parent-run login-only check PASSED on 2026-11-06; see evidence below. This is
not authenticated acceptance. No further native calls are authorized for this
worker now. Human login is explicitly deferred to the next round.** The command
below is retained for reproducibility, not authorization to repeat it. Any repeat
requires parent approval and native-window queue coordination, from desktop:

```powershell
$env:GOCACHE = 'D:/AI-Reasonix/.tmp/orca-beta-gocache'
$env:GOPROXY = 'off'
$env:TEMP = 'D:/AI-Reasonix/.tmp/orca-beta-test-temp'
$env:TMP = $env:TEMP
$env:ORCA_BETA_SYNTHETIC_PERF = '0'
$env:ORCA_WEB_BETA_LOGIN_ONLY_TEST = '1'
$env:ORCA_WEB_BETA_ACCEPTANCE = ''
$env:ORCA_WEB_BETA_PROBE = ''
$env:ORCA_WEB_BETA_LOGIN_ROOT = Join-Path (Join-Path (Split-Path (Get-Location).Path -Parent) '.tmp') ('webbeta-fresh-login-' + [guid]::NewGuid().ToString('N'))
if (Test-Path -LiteralPath $env:ORCA_WEB_BETA_LOGIN_ROOT) { throw 'Refusing existing profile/evidence' }
New-Item -ItemType Directory -Path (Split-Path $env:ORCA_WEB_BETA_LOGIN_ROOT -Parent) -Force | Out-Null
go test -mod=readonly -tags 'production,webbeta_acceptance' . -run '^TestWebBetaNativeFreshLoginOnly$' -count=1 -timeout=150s -v
```

This worker's compile-only check used `production,browserui,webbeta_acceptance`
with `-run '^$'`; this worker did not execute the native harness. Offline tests
exercise all Run refusals, script isolation, native COM navigation cancellation
and popup handling.
A 100x check passed after fixing a reproduced intermittent COM test callback
output-pointer lifetime issue by using edge.ComProc.Call's uintptrescapes contract.

### Parent-Run Native Evidence (2026-11-06)

The parent reports `TestWebBetaNativeFreshLoginOnly` PASS in **9.17 s**. The
retained JSON was read back during this documentation update:

[login-only-result.json](D:/AI-Reasonix/.tmp/orca-beta-worktree/.tmp/webbeta-fresh-login-cc88bfedfbe94ec5bc0f556acb2c4988/login-only-result.json)

It records `origin=https://chat.deepseek.com`, `windowClass=OrcaBetaWindow`,
`loginControlsPresent=true`, `presented=false`, `runRefused=true`,
`adapterInstalled=false`, `probesOrUploads=false`, and
`profileReleasedAndCleared=true`. The harness writes this success report only
after its release/cleanup barrier. This supports fresh-cookie real-site login-page
reachability in the hidden beta-class host, the refusal path, and release/cleanup
of that fresh owned profile. Initialization also exercised native guard
registration, but did not adversarially test redirects/popups on the live site.

The **9.17 s is parent-reported total test duration**; the JSON contains no timing
measurement. It is not a cold-start benchmark or time-to-authenticated-readiness.
There was no authentication and no model send. No authenticated adapter readiness,
vision, upload, tool execution, account switching, authenticated logout, production
UI or end-to-end agent acceptance is established by this run.

Current gate: automation-only checks, with human login reserved for the next
explicitly authorized round. Parent plans a separate isolated host-adversarial
check; it is pending, not a result of this run. This documentation update makes
no further native/browser/network calls and grants no additional send permission.

## Handoff and Validation

Exact files edited or added in this approved follow-up:

- `internal/boot/boot.go`
- `internal/skill/tools.go`
- `internal/skill/subagent_catalog_test.go`
- `desktop/webbetahost/browser_windows.go`
- `desktop/webbetahost/document_scripts.go`
- `desktop/webbetahost/login_boundary_windows.go`
- `desktop/webbetahost/login_only_windows_test.go`
- `desktop/web_beta_login_native_windows_test.go`
- `internal/webbeta/PERF-EXPERIMENT.md`

No parent lifecycle wiring is required for this follow-up: boot's call is already
updated. The additive skill API preserves the original entry point. The additive
native constructor is `NewLoginOnlyWithWindowClass(profile, windowClass)`;
`ErrLoginOnly` supports refusal assertions. This constructor must not replace the
normal app browser factory. The only runnable new native entry is the explicitly
opted-in tagged test above; no production startup hook was added.

Completed offline checks with the requested process GOCACHE and GOPROXY=off:

- `go test -mod=readonly ./internal/skill ./internal/boot ./internal/webbeta/... -count=1 -timeout=120s`
- `go test -mod=readonly ./internal/skill -run 'Test(SubagentCatalog|CatalogSnapshot)' -count=20 -timeout=90s`
- Desktop: `go test -mod=readonly . ./webbetahost -run '^(TestWebBeta|TestLoginOnly|TestProfile)' -count=1 -timeout=90s` (perf opt-out)
- Desktop: `go test -mod=readonly ./webbetahost -run '^TestLoginOnly' -count=100 -timeout=90s`
- Desktop: `go test -mod=readonly -tags webbeta_acceptance ./webbetahost -run '^TestLoginOnly' -count=100 -timeout=90s`
- Desktop compile-only: `go test -mod=readonly -tags 'production,browserui,webbeta_acceptance' . ./webbetahost -run '^$' -count=1 -timeout=90s`
- The unchanged synthetic experiment twice, as detailed above; gofmt check clean.

The temporary COM fixture failure was retained in Kuhn's broader-run evidence,
not hidden by a skip. Subsequent focused runs passed after the pointer-lifetime
fix. No race-detector run was performed here. The parent's subsequent limited
native login-only acceptance is recorded above, separately from these offline
checks and synthetic performance measurements.
