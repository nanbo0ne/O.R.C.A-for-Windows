# Beta UI QA Handoff

## Scope And Status

Final post-callback/profile-guard and `needsSignOut` native revalidation PASSED: `.tmp/beta-native-final-revalidation-20261106-140147-564/matrix-report.json`, 16 sequential Modern/Classic en/zh cases, 112 checkpoints/screenshots, 168 trusted HWND-routed clicks, 128.33 seconds. The offline PNG verifier passed all 112 captures. The production frontend was rebuilt with Node 24 and the native test executable recompiled against the current backend; no production source/assertion changes were made for this rerun. Reviewed Classic/zh/1024 account, Classic/en/760 web login, and Modern/zh/760 settings captures: modal controls remain unobstructed and native fixture pixels occur only in the intended viewport. The prior 554-assertion/116-screenshot headless evidence is preserved unchanged.

Residual intermittent failure: the first revalidation stopped after 13 passes at Classic/zh/1024, before the account modal. `.tmp/beta-native-revalidated-20261106-135602-596/classic-zh-1024/` retains its failure PNG/JSON/log. Local navigation completed and backend error fields were empty, but no visible native child appeared within the existing eight-second checkpoint deadline. This is not established as the earlier metadata problem, nor claimed fixed. Test-only diagnostics now retain frontend viewport calls/completion order, document visibility, requested native visibility, backend saved viewport, and shown owner on failure. One fresh targeted case passed in `.tmp/beta-native-visibility-gate-20261106-140118-603`; then one bounded full rerun passed as above. No retries remain running and no assertion/deadline was relaxed. Real account authorization remains deferred.

The synthetic full-UI native layout gate PASSED on 2026-11-06 after rebuilding the parent's explicit beta HWND-class and metadata persistence fixes. Fresh evidence: `.tmp/beta-native-final-20261106-104728-763/matrix-report.json`. All 16 sequential cases passed in 100 seconds: 112 checkpoints/screenshots and 168 trusted HWND-routed clicks. The separate offline PNG paint/occlusion verifier also passed. No native test window remains open. No live account, model, or website requests were made; real ORCA authorization and the real DeepSeek site adapter remain unverified.

The final run first passed a targeted Classic/en/760 regression gate in `.tmp/beta-native-metadata-gate-20261106-104646-847`. Only then was one complete matrix run, because the metadata fix affects both styles. Prior failed captures and logs were preserved, not overwritten:

- `.tmp/beta-native-ui-20261106-100915-664`: preview-only HWND lookup rejected the beta host; resolved by the parent's explicit same-PID beta-class lookup.
- `.tmp/beta-native-matrix-20261106-101725-080`: 15 cases passed, then Classic/zh/1920 hit metadata rename failure.
- `.tmp/beta-native-matrix-20261106-102325-492`: loopback fixture selected Chrome-blocked port 5061. The fixture now binds an available port in 18080-18179; existing listeners are untouched.
- `.tmp/beta-native-matrix-20261106-102807-079`: all 16 interaction flows passed, but two initial web-view screenshots were blank. This is NOT a visual pass. Checkpoints now wait for the local navigation event and bounded PNG paint/occlusion criteria, preserving intermediate captures.
- `.tmp/beta-native-matrix-20261106-103330-441`: Modern 8/8 passed the stricter checks, then Classic/en/760 failed on metadata access denied. Native retries stopped until the parent's bounded Windows replace/fsync fix and error-ownership regression tests passed. No frontend error/visibility assertion was weakened.

`desktop/beta_wails_ui_test.go` runs the embedded production frontend with real App bindings, beta channel isolation, the actual `OrcaBetaWindow` class, and separate native browser surfaces. The DeepSeek backend factory is replaced with an isolated loopback native fixture. The actual DeepSeek site adapter and real ORCA authorization are deliberately outside this test.

The native harness requires a fresh directory below this worktree's `.tmp`, refuses reuse, and retains profiles/logs/screenshots. It never moves the global cursor or uses SendInput. Input is posted through descendants of the test PID's verified beta HWND, followed by a trusted DOM-click assertion. Address setup uses a synthetic form event; modal actions use native routed clicks. Account authorization uses a no-op opener and a deny-all HTTP transport. Both browser fixtures restrict navigation to their loopback server.

Checkpoints: ordinary browser visible; account modal hides native children; account signing-in/cancel remains unobstructed; nested web dialog has exactly one correctly bounded native child; closing web hides that child; web settings hides all children; closing primary restores the ordinary browser without changing web queue/sent/settings. Each checkpoint checks exact HWND count/bounds plus the loopback fixture's distinct paint color (present only when expected), not merely a nonblank application background. Modern and Classic use production frame settings. Screenshots use a thread-locked per-monitor DPI context to avoid 150% scaling crops. Native widths are requested window widths and may be OS/display-clamped; the headless matrix separately verifies exact CSS viewport widths.

## Native Command (Parent Only, After Scheduling)

Do not run concurrently with another native test window. The production beta window class must remain `OrcaBetaWindow`; the fixture never substitutes the preview class or searches unrelated processes. Each child has a 105-second test deadline and 110-second process deadline. The matrix stops at the first failing case, retaining its failure PNG, JSON state, and log.

Build assets and compile without launching a window. WebView2 subprocesses crashed under the execution sandbox; run the authorized native executable outside that sandbox, not with reduced browser security:

```powershell
Set-Location D:/AI-Reasonix/.tmp/orca-beta-worktree/desktop/frontend
$env:PATH="$env:USERPROFILE/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin;"+$env:PATH
npm run build
Set-Location ..
go test -c -tags 'production browserui' -o ../.tmp/beta-native-ui.test.exe
```

After parent approves the isolated run, this exact command launches one native case:

```powershell
Set-Location D:/AI-Reasonix/.tmp/orca-beta-worktree/desktop
$env:ORCA_BETA_NATIVE_UI_TEST='1'
$env:ORCA_BETA_NATIVE_ROOT='D:/AI-Reasonix/.tmp/orca-beta-worktree/.tmp/beta-native-ui-'+(Get-Date -Format 'yyyyMMdd-HHmmss-fff')
$env:ORCA_BETA_NATIVE_CASE='modern-en-1366'
& ../.tmp/beta-native-ui.test.exe '-test.run=^TestBetaConnectionsInProductionUI$' '-test.timeout=35m' '-test.v'
```

For all 16 sequential cases, use a new root and unset `ORCA_BETA_NATIVE_CASE` before the same executable command. Matrix: Modern/Classic, en/zh, requested widths 760/1024/1366/1920. Do not reuse the single-case root. Clear `ORCA_BETA_NATIVE_UI_TEST` after the run. The ordinary browser host acceptance is separate and is not a replacement for these modal checks.

Offline evidence verification opens no windows:

```powershell
$env:ORCA_BETA_NATIVE_EVIDENCE='D:/AI-Reasonix/.tmp/orca-beta-worktree/.tmp/beta-native-final-20261106-104728-763'
& ../.tmp/beta-native-ui.test.exe '-test.run=^TestBetaNativeEvidence$' '-test.timeout=2m' '-test.v'
```

## Frontend Commands

From `desktop/frontend` with Node 24 prepended as above:

```powershell
npm run test:all
node node_modules/tsx/dist/cli.mjs src/__tests__/beta-connections.test.ts
node scripts/check-css-syntax.mjs src/components/beta-connections.css
node scripts/check-z-index-tokens.mjs src/components/beta-connections.css
npm run build
```

Start a local Vite server on an unused port, then run in a second terminal:

```powershell
$env:NODE_PATH="$env:USERPROFILE/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules"
$env:ORCA_BETA_BASELINE='D:/AI-Reasonix/.tmp/orca-beta-worktree/.tmp/beta-ui-baseline/baseline.json'
node src/__tests__/beta-connections.browser.cjs http://127.0.0.1:5287 ../../.tmp/beta-expiry-recovery-verified
node src/__tests__/browser-panel.browser.cjs http://127.0.0.1:5287 ../../.tmp/beta-browser-trueclick
```

Receipt-safety matrix PASSED: `.tmp/beta-receipt-safety-verified/report.json`, 530 assertions, 16 layout cases plus four focused receipt-safety scenarios, 108 screenshots, zero failures. The earlier retained-turn-only result remains preserved in `.tmp/beta-turns-verified/report.json` (490 assertions, 96 screenshots). Original shell/Composer geometry equals the pre-edit baseline at exact CSS widths 760/1024/1366/1920 in both styles and languages. Evidence uses `SyntheticProject` / `TestConversation` only. Wallet values remain exact strings; pending or unknown receipt costs are unknown even when the server supplies nonempty numeric or malformed values, never zero. Scoped gifts are separate from cash. Errors use public localized categories, never raw credentials/URLs/configuration. Wallet refresh is bounded to ten seconds except manual refresh or a new settlement.

Account details now include the collapsible `Retained requests by turn` / `可见请求按回合汇总` section. It consumes `OrcaAccountTurnSettlements()` without frontend grouping, deduplication, arithmetic, or Number conversion. Only `retained_requests` scope is displayed; this is not an all-history bill. Cost is shown only for settled/estimated rows; pending/unknown cost stays blank. The server's settled subtotal is independent and can remain visible while a turn is pending. Fixtures cover exact large decimals, estimated labels, pending subtotal, unknown blanks, and disclosure keyboard focus. Generated bindings include the latest turn-settlement and model-refresh methods.

Post-review receipt safety: the public status DTO now accepts optional `receiptError`. A nonempty value maps only to a fixed localized receipt-storage warning in the new ORCA account section and account details; raw paths, credentials, and backend text are never rendered. Existing status polling clears both warnings when the backend clears the field, without changing authentication or closing details. Individual receipt cost now shares the settled/estimated-only amount guard with turn summaries, so even numeric or malformed nonempty pending/unknown costs display Unknown, never an amount. `ModelsStale` does not add UI behavior. This text/data-only follow-up changes no CSS, shell geometry, or native presentation code; the 16-case native suite was not rerun. Real-person login remains deferred.

Receipt follow-up verification: beta unit tests, `npm run test:all`, typecheck/production build, the full headless layout matrix, and four focused Modern/Classic en/zh cases at 760/1024 all passed. The focused cases check fixed public warning copy despite a raw synthetic error containing a session-expiry string and private path/token markers; signed-in state and logout controls stay intact; clearing the field removes both warnings; valid settled/estimated costs retain exact strings; nonempty pending/unknown costs remain hidden. Reviewed the Modern/en/760 primary warning and details and Classic/zh/760 details screenshots: no overlap or unreachable controls. Follow-up touched only `BetaConnectionsPanel.tsx`, `betaBridge.ts`, en/zh locales, the two beta test files, and this report; generated bindings already included `receiptError`.

Expiry recovery follow-up PASSED: `.tmp/beta-expiry-recovery-verified/report.json`, 554 assertions, 16 layout cases plus the four focused receipt/expiry scenarios, 116 screenshots, zero failures. The optional public `needsSignOut` field shares one `authenticated || needsSignOut` decision between the existing account button label and its click handler. An expired unauthenticated account with retained controllers therefore offers logout, not a rejected new login; account details stay unavailable. Synthetic login refuses while the flag is true. Tests verify the logout call clears the flag, the same button offers sign-in, and the next sign-in reaches authorization. Modern/Classic en/zh at 760/1024 pass; old shell/Composer geometry is unchanged. Reviewed narrow English and Chinese expiry captures and the English re-login capture. Beta unit tests, the full frontend suite, and production build passed again. No CSS, native presentation, or package-preparation files changed; no native rerun or personal-data/real-account access occurred. This follow-up touched `BetaConnectionsPanel.tsx`, `betaBridge.ts`, the beta browser fixture, and this report. Source approval hashes must be refreshed after all ongoing backend/frontend work is frozen.

Older browser fixture: 119 assertions, including a deliberate opaque obstruction negative control. It now waits for Playwright actionability, retains the independent center hit-test, and performs real clicks instead of DOM `.click()`. The earlier intermittent hit-test failures were not reproduced in two diagnostic runs; no specific production overlay culprit is claimed.

Final `npm run test:all` (including pre/post suites), beta unit tests, dedicated CSS syntax/z-index checks, TypeScript, and production build all passed. The build retains the existing large-chunk warning; no dependency changes were made. No native test window or test-only Vite server remains running.

Manual screenshot review included native Classic/en/760 web login and restored browser; Classic/zh/760 account panel; Modern/zh/1366 account panel; Modern/en/1024 settings; and headless Classic/en+zh/760 and Modern/en/1024 expanded account details. No overlapping controls or native-content leaks were found. All 112 native captures additionally passed the automated paint/occlusion check. Synthetic native finance authentication is not implemented: exact balances, gifts, receipts, and retained-turn summaries are covered by the headless bridge fixtures, not a real account.

## Files Owned By This Frontend Work

- `desktop/frontend/src/App.tsx` and `components/AppChrome.tsx`: beta entry, gating, native obstruction, account model metadata refresh. Other pre-existing browser changes are preserved.
- `desktop/frontend/src/components/BetaConnectionsPanel.tsx` and `beta-connections.css`: new dialogs and finance view only.
- `desktop/frontend/src/lib/betaBridge.ts`, `locales/en.ts`, `locales/zh.ts`: public DTOs and localized copy.
- `desktop/frontend/src/lib/bridge.ts`: generated-name drift guard only, recognizing the separate BetaBridge contract; existing AppBindings types unchanged.
- `desktop/frontend/src/__tests__/beta-connections.browser.cjs`, `beta-connections.test.ts`, `browser-panel.browser.cjs`, `composer-send-failure.test.ts`: beta QA, true-click regression, test-only CRLF normalization. Existing shared `transcript-layout.browser.cjs` fixture support is reused.
- `desktop/beta_wails_ui_test.go`: opt-in isolated native fixture. `beta_wails_evidence_test.go`: offline full-matrix PNG verification. `browser_wails_ui_test.go` and `browser_wails_acceptance_test.go`: test-only beta-class capture/finder support, preview default preserved.
- This handoff document. No Composer, global stylesheet, dependency, or production backend edits belong to this frontend change.
