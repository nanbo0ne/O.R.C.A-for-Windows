# O.R.C.A. Desktop 3.0.11 Verification

## Scope

This record covers the `/compact` compatibility, Composer interaction, image
paste de-duplication, stale-controller protection, and session-loading changes
in the 3.0.11 patch. The unrelated working-tree change
`site/src/data/community.json` is intentionally excluded.

## Local verification

- `go test -count=1 -p=1 ./...`: passed.
- `go test -count=1 .` from `desktop`: passed.
- `npm run test:all`: passed, including responsive Composer, streaming,
  cancellation, effort persistence, transcript timeline and provider contracts.
- `npm run check:css`: passed.
- `npm run build`: passed with TypeScript and Vite production output.
- `git diff --check`: passed; Git reported only existing line-ending warnings.

## Compaction diagnosis and protection

Normal turns and compaction use the same active Provider instance and model.
Compaction differs by omitting tools, using a checkpoint system prompt, and
flattening the selected history. Therefore a provider's explicit
`404 model_not_found` is classified as model availability/routing failure, not
as an effort failure. An effort incompatibility is classified only from an
explicit `400/422` reasoning-parameter response.

The tests cover:

- model-not-found with no retry, no archive and unchanged live history;
- one request-scoped effort omission retry without changing saved preference;
- one tool-free conversation fallback for request-shape/transient failures;
- context-too-large with no retry;
- automatic-compaction backoff after failure and later retry after expiry;
- exact model preservation across standard and fallback requests;
- no leakage of internal request purpose or override fields into wire JSON.

No live provider request was made in this local pass, so the historical Token
Lens 404 is not claimed to be reproduced or fixed upstream. The application now
keeps the model unchanged, preserves history, avoids cross-provider fallback,
and gives an actionable refresh/switch message.

## Interaction and loading

The processing state is rendered in the Composer's flexible middle cell.
Keyboard handling keeps `Enter` for send and `Shift+Enter` for newline, with IME
composition protection. Native clipboard reads are serialized per paste and
image content hashes are used for de-duplication. Blank Composer clicks focus
the textarea without capturing controls or menus. Concurrent first-paint loads
are coalesced per tab, and stale balance results are discarded by generation.

## Coverage boundary

Computer Use remained disabled and no native GUI automation was used. Native
Wails button interaction and installed-app screenshots are therefore not
claimed as locally verified. Release CI remains responsible for cross-platform
builds, installer acceptance, signing and platform package checks before the
public release is considered complete.
