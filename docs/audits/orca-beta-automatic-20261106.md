# ORCA beta automatic acceptance

Status: automatic integration gates passed; first packaged startup failed,
scoped repair passed focused regression, replacement artifact verification pending.
This is not a complete delivery/release certificate.
User decision: human ORCA and DeepSeek login is deferred to the next round.

## Scope and non-actions

Source: `D:/AI-Reasonix/.tmp/orca-beta-worktree`, `codex/orca-beta`, shared
baseline `d23890928acf8bf6aede558959670027445829ac`. Version/channel are supplied
by `scripts/build-orca-beta.ps1`; an unqualified Wails build is NOT the beta.

All tests use synthetic inputs and isolated profiles. No official chats were
read, no real user account was logged in, no model service restarted, no client
published, and no stable feed changed. Real model requests/spend remain 0/0 CNY.
Synthetic wallet credits exist only in disposable test databases, never live
accounts. Temporary server resources are label-checked before cleanup.

## Completed evidence

| Area | Result and limitation |
| --- | --- |
| Final serial Go suites | Root 59 packages and desktop 12 packages passed after admission/no-replay fixes; 37/18 existing or opt-in test skips. These are not skipped packages. |
| Frontend | Full tests, CSS/type checks and production build passed. Last synthetic matrix: 554 assertions, 116 screenshots. |
| Native UI | 16 Modern/Classic, Chinese/English cases; 112 screenshots and 168 scoped HWND clicks. Native viewport clamps differ from exact headless dimensions. |
| Native browser | Post-confinement host/adversarial/upload/download/callback fixtures passed with fresh synthetic beta profiles; cleanup passed. Download completion took about 62 s; platform cause remains unconfirmed. |
| Website login structure | Fresh-cookie hidden DeepSeek official login page passed in 9.17 s, no adapters/sends/credentials/chat extraction. Not authenticated acceptance. |
| Account client | Actual Go client + synthetic transport + DPAPI + registered provider/journal integration repeated; no production server or real account in those tests. |
| Account admission | Public submit/display/guidance/deferred-send detached-controller regressions and expiry recovery passed 10 repetitions. |
| No replay | Main Agent and compaction fail closed after marked uncertain account requests; ordinary API stream-recovery regressions still pass, 10 repetitions. |
| Final ownership review | Background tasks preserve the original parent turn without inheriting foreground cancellation. Settlement lookup binds the account generation before dispatch. Both regressions failed before repair, then passed 50 shuffled repetitions; affected suites also passed. |
| Server service contracts | Fresh PostgreSQL/Redis run: 67 top-level / 192 pass events, zero skips/failures. Four suites plus wallet invariants; cleanup passed. |
| Actual candidate startup | Full candidate + fresh PostgreSQL/Redis: migrations through 271, synthetic Allow/Deny, PKCE/session/revoke passed, no inference; cleanup passed. |
| Actual candidate client and inference | Four synthetic inferences through the real account client and native OpenAI/Anthropic adapters passed: two successful and two canceled. Exact receipts, query-only recovery and revoke passed; no external model calls. |
| Cross-compile | Shared root `go build ./...` passed Linux amd64 and Darwin arm64. This is not a full native Wails GUI build. |

Native UI evidence:
`.tmp/viewport-order-matrix-20261106-7cf41c/matrix-report.json`
(final rerun after the viewport ordering fix).
UI commands and screenshots: `docs/audits/beta-ui-native-qa.md`.
Browser/WebBeta review: `internal/webbeta/REVIEW-20261106.md`.
Performance measurements: `internal/webbeta/PERF-EXPERIMENT.md`.

Server reports are in the separate candidate directory:
`D:/AI-Reasonix/.tmp/orca-beta-server/backend/.desktop-acceptance/`.
Service-contract run: `orca-desktop-778ae67afd06e321b76b4269b147ad33`.
Actual-startup run: `orca-startup-c752ebb0f4d31c8bce70cd01f35c8250`.
Final native/client run: `orca-startup-68b99cec4dd6a1cf4f39f5f60671ff52`.
Candidate binary SHA256:
`2c7c4252f1794655aac4a0aa6ca1362bae1c87962a8611655f6984b2734108f8`.
Client test binary SHA256:
`8334612485f3271d97b5acb1477f169ec3190acdea021edbe38a9830a47d2992`.
Both success receipts settled at 18 CNY micros each. Canceled requests remained
in reconciliation with held amounts, not falsely displayed as final zero cost.
The synthetic database contained four reservations/usage rows and no payment
orders. The labeled environment was cleaned with `cleanup_failed=0`.

## Failures retained and investigated

- Docker inherited compression conflicted with max-file=1: bounded explicit
  test-container logging fixed it. No Docker daemon setting was changed.
- Host port publication was unavailable on the internal network: removed it;
  candidate probes remain entirely inside the isolated network.
- Fresh AUTO_SETUP needed its writable DATA_DIR: fixture now creates and checks
  only its owned directory and aborts promptly on backend exit. Real migrations
  and initial setup are still exercised, not bypassed.
- Native-client stage first failed while seeding an allowlisted synthetic route;
  no inference occurred. The final fresh run passed after fixture correction.
- Final parallel root run failed `TestRunStatuslineCmd` (empty output under its
  subprocess deadline). Ten isolated repetitions and a complete serial root
  rerun passed; the original failed run is retained.
- Actual candidate/client testing found a server refresh defect: Redis Lua
  `cjson.encode` rounded a 63-bit account version. Atomic rotation now stores
  exact Go-serialized JSON, with large-integer and replay regressions. A fresh
  candidate run passed two refresh rotations plus immediate session checks.
  Its next failure was the synthetic upstream's HTTP URL, correctly rejected
  by the production HTTPS policy. The fixture subsequently used HTTPS and its
  own process-local CA. Production validation was not weakened; final native
  inference passed separately.
- Initial new guidance admission fixture omitted an Executor, for which steering
  is intentionally a no-op. Corrected to a real synthetic Agent plus controlled
  runner; the public-entrypoint assertion remains unchanged and now passes.
- A native browser capture was blank although navigation completed. Repeats
  passed, but source review reproduced a real ordering race: asynchronous Wails
  viewport calls could apply an old cleanup hide after a newer show. The shared
  frontend bridge now serializes immutable viewport updates, without retries.
  The regression failed before the fix and passed 20 repetitions afterward;
  a fresh native 16-case matrix passed. This establishes the race fix, not the
  exact cause of that historical frame. A stale targeted modal screenshot is
  also retained; the corresponding final matrix capture is correct.
- Native packaging could inherit GOFLAGS overlays or user GOENV outside the
  approved source snapshot. All native stages now use cleared process-local
  environments and pinned tools/inputs; NSIS ignores local config. Positive
  overlay/GOENV controls and 36 native-build safety checks passed.
- HTTPS native inference passed OpenAI adapter/receipt/no-replay checks, but
  Anthropic returned the upstream request ID instead of the server ledger ID.
  The owned test database settled both requests (36 CNY micros total); querying
  the upstream ID correctly returned 404. This is a real correlation defect,
  fixed by pinning the authenticated desktop request's receipt ID at response
  commit. Ordinary API-key behavior is unchanged. Both protocols now pass the
  unchanged receipt checks, including canceled requests and session recovery.

## Remaining gates and limits

The first packaged artifact must not be delivered as accepted. Its actual
fresh-profile screenshot showed a Windows model-state rename failure despite a
successful process lifecycle. Synthetic reader locks reproduce that failure
mode; the original holder remains unknown. The repair revalidates the expected
snapshot before each bounded rename attempt, also covers fallback tab saves,
and never deletes or truncates the old file. Focused tests passed (fileutil 50
shuffled runs; desktop save/migration ten), but final acceptance requires a new
snapshot build and a fresh actual-program screenshot. The capture helper's
separate DPI bounds defect was fixed only in excluded test tooling.

Go `-trimpath` also hides `-ldflags` from build metadata. The corrected packaging
gate queries the linked channel/version through an early, headless, hash-pinned
and time-bounded CLI probe. It does not weaken path privacy or fall back to a
build log. See `DEVLOG.md` and the delivery's final `VALIDATION.md` for superseding
artifact results; historical intermediate reports intentionally remain intact.

1. Real paid inference and authenticated website operation remain untested.
   The actual candidate, real Go client and native OpenAI/Anthropic adapters
   passed against an isolated synthetic upstream, not production model services.
2. Final source snapshot build and source diff checks. Existing COM
   unsafe-pointer vet diagnostics are not a clean vet
   pass. No race-detector result is claimed without a working C toolchain.
3. Installer/portable production build, archive/CRC/version hashes, isolated
   install/uninstall and side-by-side data retention. None is implied by unit
   tests of the packaging scripts.
   The installer harness requires a disposable Windows VM/account; no such
   environment has been established. Host-account shell/registry effects cannot
   be isolated merely by changing APPDATA. Its 68 synthetic checks passed,
   but actual install/uninstall and wizard acceptance remain untested.
4. Clean source ZIP from reviewed file hashes, matching build provenance,
   no credentials/profiles/caches/old installers. Not yet a delivered artifact.
5. Human login, authenticated DeepSeek website send/logout/switch and real
   balance/account changes remain explicitly deferred to the next round.
6. No service deployment until automated integration gates and live topology
   compatibility review pass. The local beta cannot claim live ORCA account
   availability while the server patch is undeployed.

The native download delay is reproducible but its platform cause remains
unconfirmed. No SmartScreen, browser safety or operating-system protection was
disabled. This report is bounded acceptance evidence, not a complete security
audit or assurance of every provider/site/platform.
