# O.R.C.A. Desktop 3.0.17 Verification

## Scope

Changes under verification: browser consent following the permission selector, the browser-authorization sign-in, the account capsule, the live model list, and the deferred rendering/polling work.

## Automated checks

- `go build -tags production ./...` in `desktop` — clean.
- New unit tests in `desktop/whalestack_refresh_test.go`: the model-set comparison treats reorders as no change, and a swapped model name (`chimera-1.1`) survives the chat-model filter.
- `wails generate module` regenerates the bindings contract; the frontend type-check and production bundle build clean.
- `ORCA_BROWSER_WAILS_TEST=1 go test -tags production -run TestBrowserPreviewInWailsWindow` — pass in 1.97s. The test drives a real WebView2 child window: create, observe the accessibility tree, click, type, submit, and capture a PNG. Evidence: `.tmp/browser-preview-evidence/wails-host/`.
- `go test . -count=1` in `desktop` — release-gate tests, including the documentation/version agreement.

## Manual checks

- Browser consent: with full access selected, the browser surface no longer prompts per action; switching back to a restricted mode restores the per-action consent dialog.
- Sign-in: the capsule opens the default browser on the approval page and shows the one-time code while it waits; cancelling returns the capsule to its resting state.
- Model refresh: the capsule's refresh action re-reads the relay's model list; the provider entry is rewritten only when the set actually changes.
- Rendering: settings, history, automation and tool-library panels are absent from the initial bundle load and appear on first open.

## Known limitations

- Sign-in depends on the account service minting the relay key. The unified build retired the legacy writer (`410 LEGACY_ACCOUNT_WRITER_RETIRED`); the login service now calls `POST /api/v1/unified/keys/<action>` with the `{event, options}` envelope, reuses an existing desktop key through `reveal`, and creates one bound to the local-deployment group. Verified end to end against the production login service and relay on 2026-11-26: device code issued, browser approval accepted, poll returned `approved` with a working key, and `GET /v1/models` answered with the group's models.
- The relay key is bound to one group. The desktop group is configurable (`desktopKeyGroupId` in the login config, or `WHALESTACK_KEY_GROUP_ID`); the default is the local-deployment group, not the free web-chat group.
- Windows builds are unsigned; macOS builds are not notarized.
